package buildlibrary

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
)

// embeddingBootstrapAdvisoryKey serializes the config-driven bootstrap
// across concurrently-booting replicas. golang-migrate holds its own
// advisory lock only across m.Up(); this covers the post-migration DDL.
// Value is ASCII "ro_embed"; distinct from migrate's DB-name-derived key.
const embeddingBootstrapAdvisoryKey int64 = 0x726F5F656D626564

// bootstrapEmbedding materializes the model-dependent schema (pgvector
// extension, vector column, HNSW index) and reconciles the singleton
// embedding_config stamp, all under a Postgres advisory lock so N replicas
// booting together serialize. It NEVER returns an error for a recoverable
// condition (missing extension privilege, model mismatch on a non-empty
// corpus); those drop to recency mode with a loud log and leave
// l.semanticEnabled = false. It returns an error only for unexpected DB
// failures.
func (l *Library) bootstrapEmbedding(ctx context.Context) error {
	conn, err := l.db.Conn(ctx) // pin one connection; advisory locks are session-scoped
	if err != nil {
		return fmt.Errorf("acquire conn: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, embeddingBootstrapAdvisoryKey); err != nil {
		return fmt.Errorf("advisory lock: %w", err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, embeddingBootstrapAdvisoryKey) }()

	// 1. Ensure the pgvector extension, tolerantly.
	if _, err := conn.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		var present bool
		_ = conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='vector')`).Scan(&present)
		if !present {
			slog.Error("embedding: pgvector extension is unavailable and could not be created; "+
				"running RECENCY-ONLY. Have a superuser run `CREATE EXTENSION vector` (or enable it via your "+
				"platform) and restart to enable semantic retrieval.", slog.String("create_error", err.Error()))
			l.semanticEnabled = false
			return nil
		}
	}

	// 2. Read the current stamp.
	var haveStamp bool
	var stampedModel string
	var stampedDim int
	err = conn.QueryRowContext(ctx, `SELECT model_id, dimensions FROM embedding_config WHERE id=1`).Scan(&stampedModel, &stampedDim)
	switch {
	case err == nil:
		haveStamp = true
	case errors.Is(err, sql.ErrNoRows):
		haveStamp = false
	default:
		return fmt.Errorf("read embedding_config: %w", err)
	}

	// 3. Does the column already exist, and is the corpus non-empty?
	var columnExists bool
	_ = conn.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='saved_trajectories' AND column_name='embedding')`).Scan(&columnExists)
	hasEmbeddings := false
	if columnExists {
		if err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM saved_trajectories WHERE embedding IS NOT NULL)`).Scan(&hasEmbeddings); err != nil {
			// Fail safe: an unreadable corpus must NOT be treated as empty, or
			// the dim-change re-key below could DROP a populated column. Assume
			// non-empty so the mismatch branch degrades to recency.
			slog.Error("embedding: could not probe corpus for existing embeddings; assuming non-empty to avoid a destructive re-key", slog.String("err", err.Error()))
			hasEmbeddings = true
		}
	}

	matches := haveStamp && stampedModel == l.embeddingModelID && stampedDim == l.embeddingDim

	// Mismatch against a non-empty corpus: degrade, do not alter/stamp.
	if haveStamp && !matches && hasEmbeddings {
		slog.Error("embedding: configured model/dim does not match the embedded corpus; running RECENCY-ONLY. "+
			"Re-embed with cmd/backfill-embeddings to restore semantic retrieval.",
			slog.String("corpus_model", stampedModel), slog.Int("corpus_dim", stampedDim),
			slog.String("configured_model", l.embeddingModelID), slog.Int("configured_dim", l.embeddingDim))
		l.semanticEnabled = false
		return nil
	}

	// degradeMaterialize logs and sets recency-only mode when a column/index
	// DDL step fails. pgvector installed but older than 0.5 (no HNSW support)
	// is the canonical trigger: CREATE EXTENSION succeeds but CREATE INDEX
	// USING hnsw hard-errors. Self-heals on next boot after an upgrade.
	degradeMaterialize := func(step string, err error) {
		slog.Error("embedding: vector column/HNSW index could not be materialized (pgvector may be older than 0.5); "+
			"running RECENCY-ONLY. Upgrade pgvector and restart to enable semantic retrieval.",
			slog.String("step", step), slog.String("err", err.Error()))
		l.semanticEnabled = false
	}

	// Safe to (re)materialize: first run, exact match, or mismatch with an
	// empty corpus. If the column exists at a different dimension, drop and
	// recreate (only reachable with no embeddings present).
	if columnExists && haveStamp && stampedDim != l.embeddingDim {
		if _, err := conn.ExecContext(ctx, `DROP INDEX IF EXISTS saved_trajectories_embedding_idx`); err != nil {
			degradeMaterialize("drop stale index", err)
			return nil
		}
		if _, err := conn.ExecContext(ctx, `ALTER TABLE saved_trajectories DROP COLUMN IF EXISTS embedding`); err != nil {
			degradeMaterialize("drop stale column", err)
			return nil
		}
		columnExists = false
	}
	if !columnExists {
		if _, err := conn.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE saved_trajectories ADD COLUMN IF NOT EXISTS embedding vector(%d)`, l.embeddingDim)); err != nil {
			degradeMaterialize("add embedding column", err)
			return nil
		}
	}
	// CREATE INDEX IF NOT EXISTS won't rebuild an index that already exists,
	// so changing m/ef_construction on a live deployment needs a manual
	// reindex (documented as a tuning-runbook step). On a fresh build the
	// WITH clause bakes in the configured graph parameters.
	idxSQL := `CREATE INDEX IF NOT EXISTS saved_trajectories_embedding_idx ON saved_trajectories USING hnsw (embedding vector_cosine_ops)`
	if l.hnswM > 0 || l.hnswEfConstruction > 0 {
		m := l.hnswM
		if m <= 0 {
			m = 16 // pgvector default
		}
		efc := l.hnswEfConstruction
		if efc <= 0 {
			efc = 64 // pgvector default
		}
		idxSQL += fmt.Sprintf(" WITH (m = %d, ef_construction = %d)", m, efc)
	}
	if _, err := conn.ExecContext(ctx, idxSQL); err != nil {
		degradeMaterialize("create hnsw index", err)
		return nil
	}
	if _, err := conn.ExecContext(ctx, `
		INSERT INTO embedding_config (id, model_id, dimensions, updated_at)
		VALUES (1, $1, $2, now())
		ON CONFLICT (id) DO UPDATE SET model_id=EXCLUDED.model_id, dimensions=EXCLUDED.dimensions, updated_at=now()`,
		l.embeddingModelID, l.embeddingDim); err != nil {
		return fmt.Errorf("stamp embedding_config: %w", err)
	}
	l.semanticEnabled = true
	return nil
}
