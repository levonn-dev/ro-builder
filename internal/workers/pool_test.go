package workers

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/levonn-dev/ro-builder/internal/buildlibrary"
	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/orchestrator"
)

// fakeRunner records the requests it sees and returns a canned result
// after an optional delay. errResult lets a test inject a failure.
type fakeRunner struct {
	mu        sync.Mutex
	calls     []string
	result    *orchestrator.GenerateResult
	delay     time.Duration
	errResult error
}

func (r *fakeRunner) Run(ctx context.Context, _ orchestrator.GenerateRequest) (*orchestrator.GenerateResult, error) {
	r.mu.Lock()
	r.calls = append(r.calls, "called")
	r.mu.Unlock()
	if r.delay > 0 {
		select {
		case <-time.After(r.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if r.errResult != nil {
		return nil, r.errResult
	}
	return r.result, nil
}

func (r *fakeRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func newTestLib(t *testing.T) *buildlibrary.Library {
	t.Helper()
	dir := t.TempDir()
	lib, err := buildlibrary.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = lib.Close() })
	return lib
}

func TestPool_ProcessesQueuedJob(t *testing.T) {
	ctx := context.Background()
	lib := newTestLib(t)
	id, _ := lib.Enqueue(ctx, json.RawMessage(`{"class":"novice"}`))

	runner := &fakeRunner{result: &orchestrator.GenerateResult{
		Primary: &domain.Trajectory{Class: "novice"},
	}}
	pool := New(Config{Library: lib, Runner: runner, Workers: 1, PollInterval: 50 * time.Millisecond})
	pool.Start()
	t.Cleanup(func() { _ = pool.Shutdown(2 * time.Second) })

	pool.Notify()

	waitFor(t, time.Second, func() bool {
		g, err := lib.GetGeneration(ctx, id)
		return err == nil && g.Status == buildlibrary.GenStatusCompleted
	})

	if runner.callCount() != 1 {
		t.Fatalf("runner calls: got %d, want 1", runner.callCount())
	}
}

func TestPool_FailedRunMarksFailedWithReason(t *testing.T) {
	ctx := context.Background()
	lib := newTestLib(t)
	id, _ := lib.Enqueue(ctx, json.RawMessage(`{"class":"novice"}`))

	runner := &fakeRunner{errResult: &orchestrator.FailureError{
		Reason: "max_iters_exhausted",
		Detail: "60 iters",
	}}
	pool := New(Config{Library: lib, Runner: runner, Workers: 1, PollInterval: 50 * time.Millisecond})
	pool.Start()
	t.Cleanup(func() { _ = pool.Shutdown(2 * time.Second) })

	pool.Notify()
	waitFor(t, time.Second, func() bool {
		g, err := lib.GetGeneration(ctx, id)
		return err == nil && g.Status == buildlibrary.GenStatusFailed
	})
	g, _ := lib.GetGeneration(ctx, id)
	if g.FailureReason != buildlibrary.FailureMaxItersExhausted {
		t.Fatalf("FailureReason: got %s, want max_iters_exhausted", g.FailureReason)
	}
	if g.ErrorDetail != "60 iters" {
		t.Fatalf("ErrorDetail: got %q", g.ErrorDetail)
	}
}

func TestPool_ShutdownDrainsInflight(t *testing.T) {
	ctx := context.Background()
	lib := newTestLib(t)
	id, _ := lib.Enqueue(ctx, json.RawMessage(`{}`))

	runner := &fakeRunner{
		result: &orchestrator.GenerateResult{Primary: &domain.Trajectory{Class: "x"}},
		delay:  200 * time.Millisecond,
	}
	pool := New(Config{Library: lib, Runner: runner, Workers: 1, PollInterval: 50 * time.Millisecond})
	pool.Start()
	pool.Notify()

	// Give the worker a moment to claim.
	time.Sleep(80 * time.Millisecond)

	// Drain timeout long enough to finish naturally.
	if err := pool.Shutdown(2 * time.Second); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	g, _ := lib.GetGeneration(ctx, id)
	if g.Status != buildlibrary.GenStatusCompleted {
		t.Fatalf("after drain: status=%s, want completed", g.Status)
	}
}

func TestPool_ShutdownTimeoutCancelsJob(t *testing.T) {
	ctx := context.Background()
	lib := newTestLib(t)
	id, _ := lib.Enqueue(ctx, json.RawMessage(`{}`))

	runner := &fakeRunner{
		// Long delay > shutdown timeout; context cancellation should
		// interrupt and the worker should mark the row failed with
		// shutdown_interrupted.
		delay:  2 * time.Second,
		result: &orchestrator.GenerateResult{},
	}
	pool := New(Config{Library: lib, Runner: runner, Workers: 1, PollInterval: 50 * time.Millisecond})
	pool.Start()
	pool.Notify()
	time.Sleep(80 * time.Millisecond)

	err := pool.Shutdown(150 * time.Millisecond)
	if err == nil {
		t.Fatalf("Shutdown should have returned a timeout error")
	}

	waitFor(t, time.Second, func() bool {
		g, err := lib.GetGeneration(ctx, id)
		return err == nil && g.Status == buildlibrary.GenStatusFailed && g.FailureReason == buildlibrary.FailureShutdownInterrupt
	})
}

func waitFor(t *testing.T, max time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("waitFor: condition not met within %s", max)
}
