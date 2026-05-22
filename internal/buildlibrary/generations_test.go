package buildlibrary

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestEnqueue_InsertsQueuedRow(t *testing.T) {
	lib := openTempLibrary(t)
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
	if string(g.RequestJSON) != `{"class":"novice"}` {
		t.Fatalf("request_json round-trip mismatch: got %s", string(g.RequestJSON))
	}
	if g.CreatedAt.IsZero() {
		t.Fatalf("created_at is zero")
	}
}

func TestGetGeneration_NotFound(t *testing.T) {
	lib := openTempLibrary(t)
	_, err := lib.GetGeneration(context.Background(), "does-not-exist")
	if !errors.Is(err, ErrGenerationNotFound) {
		t.Fatalf("err: got %v, want ErrGenerationNotFound", err)
	}
}

func TestInFlightCount_QueuedAndRunning(t *testing.T) {
	lib := openTempLibrary(t)
	ctx := context.Background()

	// Insert: 2 queued, 1 running, 1 completed, 1 failed
	mustEnqueue(t, lib, ctx)
	mustEnqueue(t, lib, ctx)
	runningID := mustEnqueue(t, lib, ctx)
	completedID := mustEnqueue(t, lib, ctx)
	failedID := mustEnqueue(t, lib, ctx)

	mustExec(t, lib, "UPDATE generations SET status='running', started_at=? WHERE id=?", time.Now().UTC(), runningID)
	mustExec(t, lib, "UPDATE generations SET status='completed', completed_at=? WHERE id=?", time.Now().UTC(), completedID)
	mustExec(t, lib, "UPDATE generations SET status='failed', completed_at=? WHERE id=?", time.Now().UTC(), failedID)

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
	lib := openTempLibrary(t)
	ctx := context.Background()

	// Insert three; the middle one is the oldest (manually set created_at).
	id1 := mustEnqueue(t, lib, ctx)
	id2 := mustEnqueue(t, lib, ctx)
	id3 := mustEnqueue(t, lib, ctx)
	mustExec(t, lib, "UPDATE generations SET created_at=? WHERE id=?", time.Now().Add(-time.Hour).UTC(), id2)

	claimed, err := lib.ClaimNext(ctx)
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

func TestClaimNext_EmptyWhenNoQueued(t *testing.T) {
	lib := openTempLibrary(t)
	claimed, err := lib.ClaimNext(context.Background())
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed != nil {
		t.Fatalf("expected nil claim on empty queue, got %v", claimed)
	}
}

func TestMarkCompleted_StampsCompletedAt(t *testing.T) {
	lib := openTempLibrary(t)
	ctx := context.Background()
	id := mustEnqueue(t, lib, ctx)
	mustExec(t, lib, "UPDATE generations SET status='running', started_at=? WHERE id=?", time.Now().UTC(), id)

	if err := lib.MarkCompleted(ctx, id); err != nil {
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
	lib := openTempLibrary(t)
	ctx := context.Background()
	id := mustEnqueue(t, lib, ctx)

	if err := lib.MarkFailed(ctx, id, FailureMaxItersExhausted, "loop hit cap=60", nil); err != nil {
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

func TestRecoverOrphans_FlipsRunningToFailed(t *testing.T) {
	lib := openTempLibrary(t)
	ctx := context.Background()
	id1 := mustEnqueue(t, lib, ctx)
	id2 := mustEnqueue(t, lib, ctx)
	mustExec(t, lib, "UPDATE generations SET status='running', started_at=? WHERE id=?", time.Now().UTC(), id1)

	n, err := lib.RecoverOrphans(ctx)
	if err != nil {
		t.Fatalf("RecoverOrphans: %v", err)
	}
	if n != 1 {
		t.Fatalf("recovered count: got %d, want 1", n)
	}
	g1, _ := lib.GetGeneration(ctx, id1)
	g2, _ := lib.GetGeneration(ctx, id2)
	if g1.Status != GenStatusFailed || g1.FailureReason != FailureInterruptedRestart {
		t.Fatalf("orphan row not recovered: status=%s reason=%s", g1.Status, g1.FailureReason)
	}
	if g2.Status != GenStatusQueued {
		t.Fatalf("queued row was disturbed: status=%s", g2.Status)
	}
}

func TestEnqueueIfUnderCap_RejectsAtCap(t *testing.T) {
	lib := openTempLibrary(t)
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
	lib := openTempLibrary(t)
	ctx := context.Background()

	// One queued, then claim it so it becomes running.
	id, _ := lib.EnqueueIfUnderCap(ctx, 5, json.RawMessage(`{}`))
	if _, err := lib.ClaimNext(ctx); err != nil {
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
	lib := openTempLibrary(t)
	ctx := context.Background()
	id := mustEnqueue(t, lib, ctx)
	if err := lib.MarkCompleted(ctx, id); err != nil {
		t.Fatalf("first MarkCompleted: %v", err)
	}
	err := lib.MarkCompleted(ctx, id)
	if !errors.Is(err, ErrAlreadyTerminal) {
		t.Fatalf("second MarkCompleted: want errors.Is(ErrAlreadyTerminal), got %v", err)
	}
}

func TestEnqueueIfUnderCap_ConcurrentDoesNotExceedCap(t *testing.T) {
	lib := openTempLibrary(t)
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
