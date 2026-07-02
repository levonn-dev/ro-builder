package buildlibrary

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

// seedGeneration enqueues a job and updates it to status='running' with
// lease_owner='w1', returning its ID. Used by embedding tests that need a
// valid parent generation row for SaveAndComplete.
func seedGeneration(t *testing.T, lib *Library) string {
	t.Helper()
	id, err := lib.Enqueue(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("seedGeneration Enqueue: %v", err)
	}
	if _, err := lib.db.ExecContext(context.Background(),
		`UPDATE generations SET status='running', lease_owner='w1', started_at=$1 WHERE id=$2`,
		time.Now().UTC(), id); err != nil {
		t.Fatalf("seedGeneration set running: %v", err)
	}
	return id
}

// validSaveInput returns a SaveInput with one scored, gate-passing snapshot
// so summarizeAndGuard accepts it and SaveAndComplete's completion UPDATE
// matches (owner='w1').
func validSaveInput(t *testing.T, id, owner string) SaveInput {
	t.Helper()
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
				scoredSnapshot("taekwon_kid", nil),
			},
		},
	}
}

func openWithEmbedding(t *testing.T, dim int, modelID string, extra ...Option) *Library {
	t.Helper()
	if testing.Short() {
		t.Skip("requires Postgres (testcontainers); skipped under -short")
	}
	// Pre-truncate before the main Open so that bootstrapEmbedding sees an
	// empty corpus. Without this, a prior test that saved an embedding could
	// leave a non-empty saved_trajectories and cause the bootstrap to degrade
	// on a model mismatch, since Open calls bootstrapEmbedding before Truncate
	// would run.
	pre, preErr := Open(context.Background(), testDSN)
	if preErr != nil {
		t.Fatalf("openWithEmbedding pre-open: %v", preErr)
	}
	if err := pre.Truncate(context.Background()); err != nil {
		t.Fatalf("openWithEmbedding pre-truncate: %v", err)
	}
	_ = pre.Close()

	opts := append([]Option{WithEmbedding(dim, modelID)}, extra...)
	lib, err := Open(context.Background(), testDSN, opts...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = lib.Close() })
	return lib
}

func TestBootstrap_FirstRunCreatesColumnAndStamps(t *testing.T) {
	lib := openWithEmbedding(t, 8, "test-model@8")
	if !lib.SemanticEnabled() {
		t.Fatal("semantic should be enabled after first-run bootstrap")
	}
	// vector column exists
	var n int
	if err := lib.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM information_schema.columns WHERE table_name='saved_trajectories' AND column_name='embedding'`).Scan(&n); err != nil {
		t.Fatalf("query column: %v", err)
	}
	if n != 1 {
		t.Fatal("embedding column not created")
	}
	// config stamped
	var model string
	var dim int
	if err := lib.db.QueryRowContext(context.Background(),
		`SELECT model_id, dimensions FROM embedding_config WHERE id=1`).Scan(&model, &dim); err != nil {
		t.Fatalf("query config: %v", err)
	}
	if model != "test-model@8" || dim != 8 {
		t.Fatalf("stamp = %q/%d", model, dim)
	}
}

func TestBootstrap_SecondOpenSameModelIsNoop(t *testing.T) {
	_ = openWithEmbedding(t, 8, "test-model@8")
	lib2, err := Open(context.Background(), testDSN, WithEmbedding(8, "test-model@8"))
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer lib2.Close()
	if !lib2.SemanticEnabled() {
		t.Fatal("second open with matching model should stay enabled")
	}
}

func TestBootstrap_DisabledWhenNoOption(t *testing.T) {
	lib := newTestLibrary(t) // Open without WithEmbedding
	if lib.SemanticEnabled() {
		t.Fatal("semantic must be disabled with no embedding option")
	}
}

func TestBootstrap_HNSWParamsBakedIntoIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Postgres (testcontainers); skipped under -short")
	}
	// Force a clean slate: drop the column/index/stamp so the bootstrap's
	// CREATE INDEX IF NOT EXISTS actually builds a fresh index with our
	// params (a leftover default-param index from a prior test would
	// otherwise satisfy IF NOT EXISTS and mask the WITH clause).
	pre, err := Open(context.Background(), testDSN)
	if err != nil {
		t.Fatalf("pre-open: %v", err)
	}
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS saved_trajectories_embedding_idx`,
		`ALTER TABLE saved_trajectories DROP COLUMN IF EXISTS embedding`,
		`DELETE FROM embedding_config`,
	} {
		if _, err := pre.db.ExecContext(context.Background(), stmt); err != nil {
			t.Fatalf("clean-slate %q: %v", stmt, err)
		}
	}
	_ = pre.Close()

	lib, err := Open(context.Background(), testDSN, WithEmbedding(8, "hnsw-test@8"), WithHNSW(8, 32, 0))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = lib.Close() })
	if !lib.SemanticEnabled() {
		t.Fatal("semantic should be enabled")
	}

	var opts sql.NullString
	if err := lib.db.QueryRowContext(context.Background(),
		`SELECT array_to_string(reloptions, ',') FROM pg_class WHERE relname='saved_trajectories_embedding_idx'`).Scan(&opts); err != nil {
		t.Fatalf("read index reloptions: %v", err)
	}
	if !strings.Contains(opts.String, "m=8") || !strings.Contains(opts.String, "ef_construction=32") {
		t.Fatalf("index reloptions = %q; want m=8 and ef_construction=32", opts.String)
	}
}

func TestBootstrap_MismatchNonEmptyCorpusDegrades(t *testing.T) {
	lib := openWithEmbedding(t, 4, "model-A@4")
	id := seedGeneration(t, lib)
	in := validSaveInput(t, id, "w1")
	in.Embedding = []float32{1, 0, 0, 0}
	if _, err := lib.SaveAndComplete(context.Background(), in); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Re-open with a different model at the SAME dim and a non-empty corpus.
	lib2, err := Open(context.Background(), testDSN, WithEmbedding(4, "model-B@4"))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer lib2.Close()
	if lib2.SemanticEnabled() {
		t.Fatal("expected recency mode (semantic disabled) on model mismatch vs non-empty corpus")
	}
}
