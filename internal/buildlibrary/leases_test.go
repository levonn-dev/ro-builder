package buildlibrary

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

// expireLease force-expires the lease on a running generation by setting
// lease_expires_at in the past.
func expireLease(t *testing.T, lib *Library, id string) {
	t.Helper()
	mustExec(t, lib, "UPDATE generations SET lease_expires_at=$1 WHERE id=$2",
		time.Now().Add(-time.Minute).UTC(), id)
}

func TestClaimNext_SetsLeaseAndOwner(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	mustEnqueue(t, lib, ctx)
	const owner = "worker-1"
	const ttl = time.Minute

	claimed, err := lib.ClaimNext(ctx, owner, ttl)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected a claimed generation, got nil")
	}

	// Verify lease fields in DB.
	var leaseOwner string
	var leaseExpires time.Time
	err = lib.db.QueryRowContext(ctx,
		`SELECT lease_owner, lease_expires_at FROM generations WHERE id=$1`, claimed.ID,
	).Scan(&leaseOwner, &leaseExpires)
	if err != nil {
		t.Fatalf("query lease: %v", err)
	}
	if leaseOwner != owner {
		t.Errorf("lease_owner: got %q, want %q", leaseOwner, owner)
	}
	if leaseExpires.IsZero() {
		t.Error("lease_expires_at should not be zero")
	}
	if claimed.Attempts != 1 {
		t.Errorf("Attempts: got %d, want 1", claimed.Attempts)
	}
}

func TestClaimNext_EmptyWhenNoQueued(t *testing.T) {
	lib := newTestLibrary(t)
	claimed, err := lib.ClaimNext(context.Background(), "w", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNext: %v", err)
	}
	if claimed != nil {
		t.Fatalf("expected nil claim on empty queue, got %v", claimed)
	}
}

func TestRenewLease_TrueWhenOwnedFalseOtherwise(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()
	mustEnqueue(t, lib, ctx)

	claimed, err := lib.ClaimNext(ctx, "worker-1", time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("ClaimNext: %v (claimed=%v)", err, claimed)
	}

	// Renew with correct owner: should return true.
	ok, err := lib.RenewLease(ctx, claimed.ID, "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("RenewLease correct owner: %v", err)
	}
	if !ok {
		t.Error("RenewLease correct owner: expected true, got false")
	}

	// Renew with wrong owner: should return false.
	ok, err = lib.RenewLease(ctx, claimed.ID, "worker-2", time.Minute)
	if err != nil {
		t.Fatalf("RenewLease wrong owner: %v", err)
	}
	if ok {
		t.Error("RenewLease wrong owner: expected false, got true")
	}
}

func TestClaimNext_ConcurrentClaimsDistinctRows(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	const n = 5
	for i := 0; i < n; i++ {
		mustEnqueue(t, lib, ctx)
	}

	type result struct {
		id  string
		err error
	}
	ch := make(chan result, n)
	for i := 0; i < n; i++ {
		owner := fmt.Sprintf("worker-%d", i)
		go func(owner string) {
			g, err := lib.ClaimNext(ctx, owner, time.Minute)
			if err != nil {
				ch <- result{err: err}
				return
			}
			if g == nil {
				ch <- result{err: fmt.Errorf("got nil from ClaimNext for %s", owner)}
				return
			}
			ch <- result{id: g.ID}
		}(owner)
	}

	seen := make(map[string]bool)
	for i := 0; i < n; i++ {
		r := <-ch
		if r.err != nil {
			t.Errorf("worker error: %v", r.err)
			continue
		}
		if seen[r.id] {
			t.Errorf("duplicate claim: id %s claimed twice", r.id)
		}
		seen[r.id] = true
	}
}

func TestRecoverExpiredLeases_Cap0FailsExpired(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	// Two running generations with expired leases, maxAttempts=0 means no requeue.
	id1 := mustEnqueue(t, lib, ctx)
	id2 := mustEnqueue(t, lib, ctx)
	mustExec(t, lib, "UPDATE generations SET status='running', lease_owner='w', started_at=$1 WHERE id=$2", time.Now().UTC(), id1)
	mustExec(t, lib, "UPDATE generations SET status='running', lease_owner='w', started_at=$1 WHERE id=$2", time.Now().UTC(), id2)
	expireLease(t, lib, id1)
	expireLease(t, lib, id2)

	requeued, failed, err := lib.RecoverExpiredLeases(ctx, 0)
	if err != nil {
		t.Fatalf("RecoverExpiredLeases: %v", err)
	}
	if requeued != 0 {
		t.Errorf("requeued: got %d, want 0", requeued)
	}
	if failed != 2 {
		t.Errorf("failed: got %d, want 2", failed)
	}

	g1, _ := lib.GetGeneration(ctx, id1)
	if g1.Status != GenStatusFailed || g1.FailureReason != FailureLeaseExpired {
		t.Errorf("id1: status=%s reason=%s", g1.Status, g1.FailureReason)
	}
}

func TestRecoverExpiredLeases_CapRequeuesThenFails(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	// id1 has attempts=1 (under cap=3), id2 has attempts=3 (at cap).
	id1 := mustEnqueue(t, lib, ctx)
	id2 := mustEnqueue(t, lib, ctx)
	mustExec(t, lib, "UPDATE generations SET status='running', attempts=1, lease_owner='w', started_at=$1 WHERE id=$2", time.Now().UTC(), id1)
	mustExec(t, lib, "UPDATE generations SET status='running', attempts=3, lease_owner='w', started_at=$1 WHERE id=$2", time.Now().UTC(), id2)
	expireLease(t, lib, id1)
	expireLease(t, lib, id2)

	requeued, failed, err := lib.RecoverExpiredLeases(ctx, 3)
	if err != nil {
		t.Fatalf("RecoverExpiredLeases: %v", err)
	}
	if requeued != 1 {
		t.Errorf("requeued: got %d, want 1", requeued)
	}
	if failed != 1 {
		t.Errorf("failed: got %d, want 1", failed)
	}

	g1, _ := lib.GetGeneration(ctx, id1)
	if g1.Status != GenStatusQueued {
		t.Errorf("id1 should be queued, got %s", g1.Status)
	}
	g2, _ := lib.GetGeneration(ctx, id2)
	if g2.Status != GenStatusFailed {
		t.Errorf("id2 should be failed, got %s", g2.Status)
	}
}

func TestRecoverExpiredLeases_LeavesLiveLeasesAlone(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	// One expired, one live.
	expiredID := mustEnqueue(t, lib, ctx)
	liveID := mustEnqueue(t, lib, ctx)
	mustExec(t, lib, "UPDATE generations SET status='running', lease_owner='w', started_at=$1 WHERE id=$2", time.Now().UTC(), expiredID)
	mustExec(t, lib, "UPDATE generations SET status='running', lease_owner='w', started_at=$1 WHERE id=$2", time.Now().UTC(), liveID)
	expireLease(t, lib, expiredID)
	// liveID keeps its lease active.
	mustExec(t, lib, "UPDATE generations SET lease_expires_at=$1 WHERE id=$2", time.Now().Add(time.Hour).UTC(), liveID)

	_, failed, err := lib.RecoverExpiredLeases(ctx, 0)
	if err != nil {
		t.Fatalf("RecoverExpiredLeases: %v", err)
	}
	if failed != 1 {
		t.Errorf("failed: got %d, want 1", failed)
	}

	live, _ := lib.GetGeneration(ctx, liveID)
	if live.Status != GenStatusRunning {
		t.Errorf("liveID should still be running, got %s", live.Status)
	}
}

func TestSaveAndComplete_OwnerScoping(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	passingInput := func(id, owner string) SaveInput {
		return SaveInput{
			ID:        id,
			Owner:     owner,
			Class:     "taekwon_kid",
			Server:    "uaro",
			Playstyle: "pvm",
			Mode:      "pre-renewal",
			Primary: domain.Trajectory{
				Class: "taekwon_kid",
				Snapshots: []domain.Snapshot{
					okSnapshot("taekwon_kid", nil),
				},
			},
		}
	}

	// Case 1: matching owner -> success, generation marked completed.
	id1 := mustEnqueue(t, lib, ctx)
	claimed1, err := lib.ClaimNext(ctx, "worker-A", time.Minute)
	if err != nil || claimed1 == nil {
		t.Fatalf("ClaimNext id1: %v (claimed=%v)", err, claimed1)
	}
	if claimed1.ID != id1 {
		t.Fatalf("claimed wrong id: got %s want %s", claimed1.ID, id1)
	}
	if _, err := lib.SaveAndComplete(ctx, passingInput(id1, "worker-A")); err != nil {
		t.Fatalf("SaveAndComplete correct owner: %v", err)
	}
	g1, err := lib.GetGeneration(ctx, id1)
	if err != nil {
		t.Fatalf("GetGeneration id1: %v", err)
	}
	if g1.Status != GenStatusCompleted {
		t.Errorf("id1 status: got %q, want %q", g1.Status, GenStatusCompleted)
	}

	// Case 2: wrong owner -> ErrAlreadyTerminal, tx rolled back, no saved_trajectories row.
	id2 := mustEnqueue(t, lib, ctx)
	claimed2, err := lib.ClaimNext(ctx, "worker-A", time.Minute)
	if err != nil || claimed2 == nil {
		t.Fatalf("ClaimNext id2: %v (claimed=%v)", err, claimed2)
	}
	if claimed2.ID != id2 {
		t.Fatalf("claimed wrong id: got %s want %s", claimed2.ID, id2)
	}
	_, err = lib.SaveAndComplete(ctx, passingInput(id2, "worker-B"))
	if !errors.Is(err, ErrAlreadyTerminal) {
		t.Fatalf("SaveAndComplete wrong owner: expected ErrAlreadyTerminal, got %v", err)
	}
	// Generation row stays running (the tx rolled back).
	g2, err := lib.GetGeneration(ctx, id2)
	if err != nil {
		t.Fatalf("GetGeneration id2: %v", err)
	}
	if g2.Status != GenStatusRunning {
		t.Errorf("id2 status: got %q, want running", g2.Status)
	}
	// No saved_trajectories row written (the INSERT was inside the rolled-back tx).
	if _, err := lib.Get(ctx, id2); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get id2: expected ErrNotFound, got %v", err)
	}
}
