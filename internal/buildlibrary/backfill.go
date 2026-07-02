package buildlibrary

import (
	"context"
	"errors"
	"fmt"
	"strings"

	pgvector "github.com/pgvector/pgvector-go"
)

// AllIDs returns every saved trajectory id (for backfill).
func (l *Library) AllIDs(ctx context.Context) ([]string, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT id FROM saved_trajectories ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SetEmbedding writes a row's vector (used by backfill). Requires semantic.
func (l *Library) SetEmbedding(ctx context.Context, id string, vec []float32) error {
	if !l.semanticEnabled {
		return errors.New("buildlibrary.SetEmbedding: semantic disabled")
	}
	_, err := l.db.ExecContext(ctx, `UPDATE saved_trajectories SET embedding=$1 WHERE id=$2`, pgvector.NewVector(vec), id)
	return err
}

// DocumentText reconstructs the embedding document for a saved row:
// playstyle + description + final_text. Mirrors the save-path composer.
func (l *Library) DocumentText(ctx context.Context, id string) (string, error) {
	st, err := l.Get(ctx, id)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(st.Playstyle + "\n" + st.Description + "\n" + st.FinalText), nil
}

// SampleVectors returns up to n stored embeddings (as query probes).
func (l *Library) SampleVectors(ctx context.Context, n int) ([][]float32, error) {
	rows, err := l.db.QueryContext(ctx,
		`SELECT embedding FROM saved_trajectories WHERE embedding IS NOT NULL ORDER BY id LIMIT $1`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]float32
	for rows.Next() {
		var v pgvector.Vector
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v.Slice())
	}
	return out, rows.Err()
}

// neighbors returns the ids of the k nearest rows to vec. exact=true forces
// a sequential scan (true KNN ground truth); exact=false uses the HNSW
// index at the given efSearch. The planner settings are applied with SET LOCAL
// inside an explicit transaction so they are transaction-scoped: rollback
// resets them automatically. This prevents settings from leaking to the next
// caller that borrows the same idle connection from the pool (database/sql
// does NOT reset session state on conn return).
func (l *Library) neighbors(ctx context.Context, vec []float32, k, efSearch int, exact bool) ([]string, error) {
	conn, err := l.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }() // discards the SET LOCAL settings; read-only tx
	if exact {
		if _, err := tx.ExecContext(ctx, `SET LOCAL enable_indexscan = off`); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `SET LOCAL enable_bitmapscan = off`); err != nil {
			return nil, err
		}
	} else if efSearch > 0 {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`SET LOCAL hnsw.ef_search = %d`, efSearch)); err != nil {
			return nil, err
		}
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM saved_trajectories WHERE embedding IS NOT NULL ORDER BY embedding <=> $1, id LIMIT $2`,
		pgvector.NewVector(vec), k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RecallAtK returns mean recall@k of the HNSW index vs exact KNN over the
// sampled probes.
func (l *Library) RecallAtK(ctx context.Context, probes [][]float32, k, efSearch int) (float64, error) {
	if len(probes) == 0 {
		return 0, fmt.Errorf("no probes (empty corpus?)")
	}
	var sum float64
	for _, p := range probes {
		exact, err := l.neighbors(ctx, p, k, 0, true)
		if err != nil {
			return 0, err
		}
		approx, err := l.neighbors(ctx, p, k, efSearch, false)
		if err != nil {
			return 0, err
		}
		set := make(map[string]struct{}, len(exact))
		for _, id := range exact {
			set[id] = struct{}{}
		}
		hit := 0
		for _, id := range approx {
			if _, ok := set[id]; ok {
				hit++
			}
		}
		if len(exact) > 0 {
			sum += float64(hit) / float64(len(exact))
		}
	}
	return sum / float64(len(probes)), nil
}
