package buildlibrary

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// insertParentGeneration writes a minimal `generations` row with the
// given id so Save tests can satisfy the FK without depending on the
// Generations repository abstraction (which is exercised in its own
// test file).
func insertParentGeneration(t *testing.T, lib *Library, id string) {
	t.Helper()
	_, err := lib.db.ExecContext(context.Background(),
		`INSERT INTO generations (id, status, created_at, request_json, lease_owner)
		 VALUES ($1, 'running', $2, '{}', 'test-owner')`,
		id, time.Now().UTC())
	if err != nil {
		t.Fatalf("insertParentGeneration: %v", err)
	}
}

// scoredSnapshot is the test default; has a non-nil Score so the save
// guard's ">=1 scored snapshot in Primary" check passes, and a default
// passing gate so the gates-required guard passes too. Tests that
// explicitly want to test those guards pass their own gates / use
// unscoredSnapshot.
func scoredSnapshot(class string, gates []domain.GateResult) domain.Snapshot {
	s := unscoredSnapshot(class, gates)
	s.Score = &scoring.ScoreResponse{
		Derived: scoring.DerivedStats{MaxHP: 5000},
	}
	if len(s.Gates) == 0 {
		s.Gates = []domain.GateResult{
			{Name: "hit_floor_normal", Severity: domain.GateSeverityPass},
		}
	}
	return s
}

func unscoredSnapshot(class string, gates []domain.GateResult) domain.Snapshot {
	return domain.Snapshot{
		Class: class,
		Level: domain.Level{Base: 99, Job: 50},
		Stats: domain.Stats{Str: 1, Agi: 1, Vit: 1, Int: 1, Dex: 1, Luk: 1},
		Gates: gates,
	}
}

// okSnapshot kept as alias for tests that pre-date the scored/unscored
// distinction. Defaults to scored so the scored-snapshot guard passes.
func okSnapshot(class string, gates []domain.GateResult) domain.Snapshot {
	return scoredSnapshot(class, gates)
}

func TestSaveAndGet_Roundtrip(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	const genID = "test-gen-id-1"
	insertParentGeneration(t, lib, genID)

	in := SaveInput{
		ID:          genID,
		Class:       "taekwon_kid",
		Server:      "uaro",
		Playstyle:   "pvm",
		Mode:        "pre-renewal",
		Description: "test build",
		Primary: domain.Trajectory{
			Class: "taekwon_kid",
			Snapshots: []domain.Snapshot{
				okSnapshot("taekwon_kid", []domain.GateResult{
					{Name: "hit_floor_normal", Severity: domain.GateSeverityPass},
				}),
			},
		},
		FinalText:      "looks good",
		CalcVersion:    "test-1",
		CatalogVersion: 1,
	}
	id, err := lib.Save(ctx, in)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id == "" {
		t.Fatal("Save returned empty id")
	}

	got, err := lib.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Class != "taekwon_kid" || got.Server != "uaro" {
		t.Errorf("class/server mismatch: %+v", got)
	}
	if got.GateSummary.Pass != 1 {
		t.Errorf("expected GateSummary.Pass=1, got %+v", got.GateSummary)
	}
	if len(got.Primary.Snapshots) != 1 {
		t.Errorf("expected 1 snapshot, got %d", len(got.Primary.Snapshots))
	}
}

func TestSave_RejectsFailingGates(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	const genID = "test-gen-id-1"
	insertParentGeneration(t, lib, genID)

	in := SaveInput{
		ID:        genID,
		Class:     "novice",
		Server:    "uaro",
		Playstyle: "pvm",
		Mode:      "pre-renewal",
		Primary: domain.Trajectory{
			Class: "novice",
			Snapshots: []domain.Snapshot{
				okSnapshot("novice", []domain.GateResult{
					{Name: "hit_floor_mvp", Severity: domain.GateSeverityFail, Threshold: 0.95, Actual: 0.50},
				}),
			},
		},
	}
	_, err := lib.Save(ctx, in)
	if !errors.Is(err, ErrFailingGates) {
		t.Fatalf("expected ErrFailingGates, got: %v", err)
	}
	// And nothing got persisted.
	n, err := lib.Count(ctx)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 rows after rejected save, got %d", n)
	}
}

func TestSave_RejectsFailingGatesInAlternatives(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	const genID = "test-gen-id-1"
	insertParentGeneration(t, lib, genID)

	in := SaveInput{
		ID: genID, Class: "novice", Server: "uaro", Playstyle: "pvm", Mode: "pre-renewal",
		Primary: domain.Trajectory{
			Class:     "novice",
			Snapshots: []domain.Snapshot{okSnapshot("novice", nil)},
		},
		Alternatives: []domain.Trajectory{
			{
				Class: "novice",
				Snapshots: []domain.Snapshot{okSnapshot("novice", []domain.GateResult{
					{Name: "ehp_one_shot_mvp_melee", Severity: domain.GateSeverityFailHard},
				})},
			},
		},
	}
	if _, err := lib.Save(ctx, in); !errors.Is(err, ErrFailingGates) {
		t.Errorf("expected ErrFailingGates from alternatives' fail_hard, got: %v", err)
	}
}

// Warns flow through. The library accepts a trajectory carrying warn
// results; those are breakdown context, not rejection signals.
func TestSave_AcceptsWarnsOnly(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	const genID = "test-gen-id-1"
	insertParentGeneration(t, lib, genID)

	in := SaveInput{
		ID: genID, Class: "wizard", Server: "uaro", Playstyle: "pvm", Mode: "pre-renewal",
		Primary: domain.Trajectory{
			Class: "wizard",
			Snapshots: []domain.Snapshot{okSnapshot("wizard", []domain.GateResult{
				{Name: "status_immunity_curse", Severity: domain.GateSeverityWarn},
				{Name: "hit_floor_normal", Severity: domain.GateSeverityPass},
			})},
		},
	}
	id, err := lib.Save(ctx, in)
	if err != nil {
		t.Fatalf("Save with warns: %v", err)
	}
	got, err := lib.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.GateSummary.Warn != 1 {
		t.Errorf("expected GateSummary.Warn=1, got %+v", got.GateSummary)
	}
}

func TestFind_FiltersByClassAndServer(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	saves := []SaveInput{
		{ID: "test-gen-id-1", Class: "taekwon_kid", Server: "uaro", Playstyle: "pvm", Mode: "pre-renewal",
			Primary: domain.Trajectory{Class: "taekwon_kid", Snapshots: []domain.Snapshot{okSnapshot("taekwon_kid", nil)}}},
		{ID: "test-gen-id-2", Class: "wizard", Server: "uaro", Playstyle: "pvm", Mode: "pre-renewal",
			Primary: domain.Trajectory{Class: "wizard", Snapshots: []domain.Snapshot{okSnapshot("wizard", nil)}}},
		{ID: "test-gen-id-3", Class: "taekwon_kid", Server: "talonro", Playstyle: "pvm", Mode: "pre-renewal",
			Primary: domain.Trajectory{Class: "taekwon_kid", Snapshots: []domain.Snapshot{okSnapshot("taekwon_kid", nil)}}},
	}
	for _, in := range saves {
		insertParentGeneration(t, lib, in.ID)
		if _, err := lib.Save(ctx, in); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	// Filter by class + server: should return only the uaro taekwon_kid row.
	got, err := lib.Find(ctx, FindParams{Class: "taekwon_kid", Server: "uaro"})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 result, got %d (%+v)", len(got), got)
	}
	if got[0].Server != "uaro" || got[0].Class != "taekwon_kid" {
		t.Errorf("filter leaked: %+v", got[0])
	}
}

func TestFind_RecencyOrder(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	// Two records of the same class; second insert should appear first.
	for i, desc := range []string{"first", "second"} {
		id := fmt.Sprintf("test-gen-id-%d", i+1)
		insertParentGeneration(t, lib, id)
		_, err := lib.Save(ctx, SaveInput{
			ID: id, Class: "taekwon_kid", Server: "uaro", Playstyle: "pvm", Mode: "pre-renewal",
			Description: desc,
			Primary:     domain.Trajectory{Class: "taekwon_kid", Snapshots: []domain.Snapshot{okSnapshot("taekwon_kid", nil)}},
		})
		if err != nil {
			t.Fatalf("Save %s: %v", desc, err)
		}
	}
	got, err := lib.Find(ctx, FindParams{Class: "taekwon_kid", Server: "uaro"})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}
	if got[0].Description != "second" {
		t.Errorf("recency: expected second first, got %q", got[0].Description)
	}
}

// Save rejects trajectories whose Primary has zero scored snapshots;
// canonical scoring couldn't run for any of them, so there's nothing
// useful to persist.
func TestSave_RejectsZeroScoredSnapshots(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	const genID = "test-gen-id-1"
	insertParentGeneration(t, lib, genID)

	in := SaveInput{
		ID: genID, Class: "taekwon_kid", Server: "uaro", Playstyle: "pvm", Mode: "pre-renewal",
		Primary: domain.Trajectory{
			Class: "taekwon_kid",
			Snapshots: []domain.Snapshot{
				unscoredSnapshot("taekwon_kid", nil),
				unscoredSnapshot("taekwon_kid", nil),
			},
		},
	}
	if _, err := lib.Save(ctx, in); !errors.Is(err, ErrNoScoredSnapshots) {
		t.Errorf("expected ErrNoScoredSnapshots, got: %v", err)
	}
	n, _ := lib.Count(ctx)
	if n != 0 {
		t.Errorf("expected 0 rows, got %d", n)
	}
}

// Save rejects trajectories whose Primary has scored snapshots but no
// gates evaluated.
func TestSave_RejectsScoredSnapshotsWithoutGates(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	const genID = "test-gen-id-1"
	insertParentGeneration(t, lib, genID)

	scoredNoGates := unscoredSnapshot("taekwon_kid", nil)
	scoredNoGates.Score = &scoring.ScoreResponse{Derived: scoring.DerivedStats{MaxHP: 5000}}

	in := SaveInput{
		ID: genID, Class: "taekwon_kid", Server: "uaro", Playstyle: "pvm", Mode: "pre-renewal",
		Primary: domain.Trajectory{
			Class:     "taekwon_kid",
			Snapshots: []domain.Snapshot{scoredNoGates},
		},
	}
	if _, err := lib.Save(ctx, in); !errors.Is(err, ErrGatesNotEvaluated) {
		t.Errorf("expected ErrGatesNotEvaluated, got: %v", err)
	}
	n, _ := lib.Count(ctx)
	if n != 0 {
		t.Errorf("expected 0 rows, got %d", n)
	}
}

// Mixed scored + unscored snapshots in Primary should pass.
func TestSave_AcceptsPartiallyScoredPrimary(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()

	const genID = "test-gen-id-1"
	insertParentGeneration(t, lib, genID)

	in := SaveInput{
		ID: genID, Class: "knight", Server: "uaro", Playstyle: "pvm", Mode: "pre-renewal",
		Primary: domain.Trajectory{
			Class: "knight",
			Snapshots: []domain.Snapshot{
				unscoredSnapshot("swordsman", nil),
				scoredSnapshot("knight", []domain.GateResult{
					{Name: "hit_floor_mvp", Severity: domain.GateSeverityPass},
				}),
				unscoredSnapshot("knight", nil),
			},
		},
	}
	id, err := lib.Save(ctx, in)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}
}

func TestGet_NotFound(t *testing.T) {
	lib := newTestLibrary(t)
	ctx := context.Background()
	_, err := lib.Get(ctx, "00000000000000000000000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}
