package buildlibrary

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestEnqueue_InsertsQueuedRow(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	id, err := lib.Enqueue(ctx, json.RawMessage(`{"class":"novice"}`))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if id == "" {
		t.Fatalf("Enqueue returned empty id")
	}

	g, err := lib.GetGeneration(ctx, id)
	if err != nil {
		t.Fatalf("GetGeneration: %v", err)
	}
	if g.Status != GenStatusQueued {
		t.Fatalf("status: got %q, want %q", g.Status, GenStatusQueued)
	}
	// Postgres JSONB may reformat whitespace; compare parsed content instead.
	var gotJSON, wantJSON map[string]any
	if err := json.Unmarshal(g.RequestJSON, &gotJSON); err != nil {
		t.Fatalf("unmarshal request_json: %v", err)
	}
	if err := json.Unmarshal([]byte(`{"class":"novice"}`), &wantJSON); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	if !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatalf("request_json round-trip mismatch: got %s", string(g.RequestJSON))
	}
	if g.CreatedAt.IsZero() {
		t.Fatalf("created_at is zero")
	}
}

func TestGetGeneration_NotFound(t *testing.T) {
	lib := newTestLibrary(t)
	_, err := lib.GetGeneration(context.Background(), "does-not-exist")
	if !errors.Is(err, ErrGenerationNotFound) {
		t.Fatalf("err: got %v, want ErrGenerationNotFound", err)
	}
}

func TestInFlightCount_QueuedAndRunning(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	// Insert: 2 queued, 1 running, 1 completed, 1 failed
	mustEnqueue(t, lib, ctx)
	mustEnqueue(t, lib, ctx)
	runningID := mustEnqueue(t, lib, ctx)
	completedID := mustEnqueue(t, lib, ctx)
	failedID := mustEnqueue(t, lib, ctx)

	mustExec(t, lib, "UPDATE generations SET status='running', started_at=$1 WHERE id=$2", time.Now().UTC(), runningID)
	mustExec(t, lib, "UPDATE generations SET status='completed', completed_at=$1 WHERE id=$2", time.Now().UTC(), completedID)
	mustExec(t, lib, "UPDATE generations SET status='failed', completed_at=$1 WHERE id=$2", time.Now().UTC(), failedID)

	n, err := lib.InFlightCount(ctx)
	if err != nil {
		t.Fatalf("InFlightCount: %v", err)
	}
	if n != 3 { // 2 queued + 1 running
		t.Fatalf("got %d, want 3", n)
	}
}

func mustEnqueue(t *testing.T, lib *Library, ctx context.Context) string {
	t.Helper()
	id, err := lib.Enqueue(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	return id
}

func mustExec(t *testing.T, lib *Library, query string, args ...any) {
	t.Helper()
	if _, err := lib.db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func TestClaimNext_ReturnsOldestQueued(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	// Insert three; the middle one is the oldest (manually set created_at).
	id1 := mustEnqueue(t, lib, ctx)
	id2 := mustEnqueue(t, lib, ctx)
	id3 := mustEnqueue(t, lib, ctx)
	mustExec(t, lib, "UPDATE generations SET created_at=$1 WHERE id=$2", time.Now().Add(-time.Hour).UTC(), id2)

	claimed, err := lib.ClaimNext(ctx, "worker", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed == nil {
		t.Fatalf("ClaimNext returned nil; want claimed row")
	}
	if claimed.ID != id2 {
		t.Fatalf("wrong claim: got %s, want %s (older); ids=%s,%s,%s", claimed.ID, id2, id1, id2, id3)
	}
	if claimed.Status != GenStatusRunning {
		t.Fatalf("status after claim: got %q, want running", claimed.Status)
	}
	if claimed.StartedAt == nil {
		t.Fatalf("StartedAt should be set after claim")
	}
}

func TestMarkCompleted_StampsCompletedAt(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()
	id := mustEnqueue(t, lib, ctx)
	mustExec(t, lib, "UPDATE generations SET status='running', started_at=$1, lease_owner='owner-A' WHERE id=$2", time.Now().UTC(), id)

	if err := lib.MarkCompleted(ctx, id, "owner-A"); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	g, err := lib.GetGeneration(ctx, id)
	if err != nil {
		t.Fatalf("GetGeneration: %v", err)
	}
	if g.Status != GenStatusCompleted {
		t.Fatalf("status: got %q, want completed", g.Status)
	}
	if g.CompletedAt == nil {
		t.Fatalf("CompletedAt should be set")
	}
}

func TestMarkFailed_StoresReasonAndDetail(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()
	id := mustEnqueue(t, lib, ctx)
	mustExec(t, lib, "UPDATE generations SET status='running', lease_owner='owner-A', started_at=$1 WHERE id=$2", time.Now().UTC(), id)

	if err := lib.MarkFailed(ctx, id, "owner-A", FailureMaxItersExhausted, "loop hit cap=60", nil); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	g, err := lib.GetGeneration(ctx, id)
	if err != nil {
		t.Fatalf("GetGeneration: %v", err)
	}
	if g.Status != GenStatusFailed {
		t.Fatalf("status: got %q, want failed", g.Status)
	}
	if g.FailureReason != FailureMaxItersExhausted {
		t.Fatalf("failure_reason: got %q, want %q", g.FailureReason, FailureMaxItersExhausted)
	}
	if g.ErrorDetail != "loop hit cap=60" {
		t.Fatalf("error_detail: got %q", g.ErrorDetail)
	}
	if g.CompletedAt == nil {
		t.Fatalf("CompletedAt should be set on failed")
	}
}

func TestEnqueueIfUnderCap_RejectsAtCap(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	// Fill the queue to cap=2.
	if _, err := lib.EnqueueIfUnderCap(ctx, 2, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := lib.EnqueueIfUnderCap(ctx, 2, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("second: %v", err)
	}
	// Third should be rejected.
	_, err := lib.EnqueueIfUnderCap(ctx, 2, json.RawMessage(`{}`))
	if !errors.Is(err, ErrQueueAtCapacity) {
		t.Fatalf("expected ErrQueueAtCapacity, got %v", err)
	}
}

func TestEnqueueIfUnderCap_RunningCounts(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	// One queued, then claim it so it becomes running.
	id, _ := lib.EnqueueIfUnderCap(ctx, 5, json.RawMessage(`{}`))
	if _, err := lib.ClaimNext(ctx, "w", time.Minute); err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	_ = id

	// Add another queued.
	if _, err := lib.EnqueueIfUnderCap(ctx, 2, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("second: %v", err)
	}
	// Now 1 running + 1 queued = 2 in-flight; cap=2 should reject.
	_, err := lib.EnqueueIfUnderCap(ctx, 2, json.RawMessage(`{}`))
	if !errors.Is(err, ErrQueueAtCapacity) {
		t.Fatalf("expected ErrQueueAtCapacity (running counts), got %v", err)
	}
}

func TestMarkCompleted_AlreadyTerminalReturnsSentinel(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()
	id := mustEnqueue(t, lib, ctx)
	mustExec(t, lib, "UPDATE generations SET status='running', lease_owner='owner-A', started_at=$1 WHERE id=$2", time.Now().UTC(), id)
	if err := lib.MarkCompleted(ctx, id, "owner-A"); err != nil {
		t.Fatalf("first MarkCompleted: %v", err)
	}
	err := lib.MarkCompleted(ctx, id, "owner-A")
	if !errors.Is(err, ErrAlreadyTerminal) {
		t.Fatalf("second MarkCompleted: want errors.Is(ErrAlreadyTerminal), got %v", err)
	}
}

func TestEnqueueIfUnderCap_ConcurrentDoesNotExceedCap(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	const cap = 10
	const goroutines = 50
	var wg sync.WaitGroup
	successes := make(chan struct{}, goroutines)
	rejects := make(chan struct{}, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := lib.EnqueueIfUnderCap(ctx, cap, json.RawMessage(`{}`))
			switch {
			case err == nil:
				successes <- struct{}{}
			case errors.Is(err, ErrQueueAtCapacity):
				rejects <- struct{}{}
			default:
				t.Errorf("unexpected err: %v", err)
			}
		}()
	}
	wg.Wait()
	close(successes)
	close(rejects)

	successCount := len(successes)
	rejectCount := len(rejects)
	if successCount != cap {
		t.Fatalf("successes: got %d, want %d (cap)", successCount, cap)
	}
	if successCount+rejectCount != goroutines {
		t.Fatalf("total: got %d successes + %d rejects = %d, want %d",
			successCount, rejectCount, successCount+rejectCount, goroutines)
	}

	// Verify DB matches what tests counted.
	n, err := lib.InFlightCount(ctx)
	if err != nil {
		t.Fatalf("InFlightCount: %v", err)
	}
	if n != cap {
		t.Fatalf("InFlightCount after race: got %d, want %d", n, cap)
	}
}
