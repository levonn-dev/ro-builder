package buildlibrary

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	pgvector "github.com/pgvector/pgvector-go"
)

// FindParams narrows a Find call.
type FindParams struct {
	Class  string
	Server string
	Limit  int
	// MaxDistance, when > 0, drops rows whose cosine distance to the query
	// vector exceeds it (the Tier B list floor). 0 = no distance filter.
	// Applies to FindSimilar only; Find is recency-ordered and ignores it.
	MaxDistance float64
	// Accepted filters by acceptance state: nil = all, true = accepted only
	// (the RAG retrieval surface), false = pending only. RAG callers set true;
	// the user-facing list leaves it nil (or honours the ?accepted= param).
	Accepted *bool
}

// acceptedClause returns the SQL predicate for p.Accepted (empty when nil).
// The predicate binds no parameter (IS [NOT] NULL), so callers append it
// without consuming a placeholder index.
func (p FindParams) acceptedClause() string {
	if p.Accepted == nil {
		return ""
	}
	if *p.Accepted {
		return " AND accepted_at IS NOT NULL"
	}
	return " AND accepted_at IS NULL"
}

const (
	defaultFindLimit = 5
	maxFindLimit     = 50
)

// Find returns the most recent saved trajectories matching the params.
func (l *Library) Find(ctx context.Context, p FindParams) ([]Summary, error) {
	if l == nil || l.db == nil {
		return nil, errors.New("buildlibrary.Find: nil library")
	}
	limit := p.Limit
	if limit <= 0 {
		limit = defaultFindLimit
	}
	if limit > maxFindLimit {
		limit = maxFindLimit
	}

	q := `
SELECT id, created_at, class, server, playstyle, description, gate_summary, accepted_at
FROM saved_trajectories
WHERE 1=1
`
	args := []any{}
	n := 0
	if p.Class != "" {
		n++
		q += fmt.Sprintf(" AND class = $%d", n)
		args = append(args, p.Class)
	}
	if p.Server != "" {
		n++
		q += fmt.Sprintf(" AND server = $%d", n)
		args = append(args, p.Server)
	}
	q += p.acceptedClause()
	n++
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", n)
	args = append(args, limit)

	rows, err := l.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var out []Summary
	for rows.Next() {
		var s Summary
		var gateJSON string
		var acceptedAt sql.NullTime
		if err := rows.Scan(&s.ID, &s.CreatedAt, &s.Class, &s.Server, &s.Playstyle, &s.Description, &gateJSON, &acceptedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if acceptedAt.Valid {
			s.AcceptedAt = &acceptedAt.Time
		}
		if gateJSON != "" {
			if err := json.Unmarshal([]byte(gateJSON), &s.GateSummary); err != nil {
				return nil, fmt.Errorf("unmarshal gate_summary for %s: %w", s.ID, err)
			}
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}

// Get loads a full SavedTrajectory by id.
func (l *Library) Get(ctx context.Context, id string) (*SavedTrajectory, error) {
	if l == nil || l.db == nil {
		return nil, errors.New("buildlibrary.Get: nil library")
	}
	const q = `
SELECT id, created_at, class, server, playstyle, mode, description,
       primary_json, alternatives_json, final_text, gate_summary,
       calc_version, catalog_version, accepted_at
FROM saved_trajectories WHERE id = $1
`
	var (
		st         SavedTrajectory
		primaryJS  string
		altsJS     sql.NullString
		gateJS     string
		acceptedAt sql.NullTime
	)
	err := l.db.QueryRowContext(ctx, q, id).Scan(
		&st.ID, &st.CreatedAt, &st.Class, &st.Server, &st.Playstyle, &st.Mode, &st.Description,
		&primaryJS, &altsJS, &st.FinalText, &gateJS, &st.CalcVersion, &st.CatalogVersion, &acceptedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	if err := json.Unmarshal([]byte(primaryJS), &st.Primary); err != nil {
		return nil, fmt.Errorf("unmarshal primary: %w", err)
	}
	if altsJS.Valid && altsJS.String != "" {
		if err := json.Unmarshal([]byte(altsJS.String), &st.Alternatives); err != nil {
			return nil, fmt.Errorf("unmarshal alternatives: %w", err)
		}
	}
	if gateJS != "" {
		if err := json.Unmarshal([]byte(gateJS), &st.GateSummary); err != nil {
			return nil, fmt.Errorf("unmarshal gate_summary: %w", err)
		}
	}
	if acceptedAt.Valid {
		st.AcceptedAt = &acceptedAt.Time
	}
	return &st, nil
}

// ErrNotFound is returned by Get when no trajectory with the given id exists.
var ErrNotFound = errors.New("buildlibrary: trajectory not found")

// SimilarSummary is a Find summary plus its cosine distance to the query
// (0 = identical, 2 = opposite). Lower is closer.
type SimilarSummary struct {
	Summary
	Distance float64 `json:"distance"`
}

// FindSimilar returns saved trajectories matching p.Class/p.Server that
// carry an embedding, ordered nearest-first by cosine distance to queryVec.
// When p.MaxDistance > 0, rows beyond that cosine distance are dropped. When
// the deployment configured a query-time hnsw.ef_search (WithHNSW), the query
// runs inside a read-only transaction that SET LOCALs it, so the recall/speed
// setting applies without leaking to other callers that later borrow the same
// pooled connection (database/sql does not reset session state on return).
// Returns nil (no error) when the library is recency-only.
func (l *Library) FindSimilar(ctx context.Context, queryVec []float32, p FindParams) ([]SimilarSummary, error) {
	if l == nil || l.db == nil {
		return nil, errors.New("buildlibrary.FindSimilar: nil library")
	}
	if !l.semanticEnabled || len(queryVec) == 0 {
		return nil, nil
	}
	limit := p.Limit
	if limit <= 0 {
		limit = defaultFindLimit
	}
	if limit > maxFindLimit {
		limit = maxFindLimit
	}
	q, args := buildSimilarQuery(queryVec, p, limit)

	if l.hnswEfSearch > 0 {
		conn, err := l.db.Conn(ctx)
		if err != nil {
			return nil, fmt.Errorf("acquire conn: %w", err)
		}
		defer func() { _ = conn.Close() }()
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback() }() // read-only; discards the SET LOCAL
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`SET LOCAL hnsw.ef_search = %d`, l.hnswEfSearch)); err != nil {
			return nil, fmt.Errorf("set ef_search: %w", err)
		}
		return scanSimilarRows(tx.QueryContext(ctx, q, args...))
	}
	return scanSimilarRows(l.db.QueryContext(ctx, q, args...))
}

// buildSimilarQuery assembles the nearest-neighbour SELECT and its bound args.
func buildSimilarQuery(queryVec []float32, p FindParams, limit int) (string, []any) {
	q := `
SELECT id, created_at, class, server, playstyle, description, gate_summary,
       accepted_at, embedding <=> $1 AS distance
FROM saved_trajectories
WHERE embedding IS NOT NULL
`
	args := []any{pgvector.NewVector(queryVec)}
	n := 1
	if p.MaxDistance > 0 {
		n++
		q += fmt.Sprintf(" AND (embedding <=> $1) <= $%d", n)
		args = append(args, p.MaxDistance)
	}
	if p.Class != "" {
		n++
		q += fmt.Sprintf(" AND class = $%d", n)
		args = append(args, p.Class)
	}
	if p.Server != "" {
		n++
		q += fmt.Sprintf(" AND server = $%d", n)
		args = append(args, p.Server)
	}
	q += p.acceptedClause()
	n++
	q += fmt.Sprintf(" ORDER BY distance ASC LIMIT $%d", n)
	args = append(args, limit)
	return q, args
}

// scanSimilarRows drains a similar-query result set. It accepts (rows, err)
// so callers can hand it a QueryContext result directly.
func scanSimilarRows(rows *sql.Rows, err error) ([]SimilarSummary, error) {
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()
	var out []SimilarSummary
	for rows.Next() {
		var s SimilarSummary
		var gateJSON string
		var acceptedAt sql.NullTime
		if err := rows.Scan(&s.ID, &s.CreatedAt, &s.Class, &s.Server, &s.Playstyle, &s.Description, &gateJSON, &acceptedAt, &s.Distance); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if acceptedAt.Valid {
			s.AcceptedAt = &acceptedAt.Time
		}
		if gateJSON != "" {
			if err := json.Unmarshal([]byte(gateJSON), &s.GateSummary); err != nil {
				return nil, fmt.Errorf("unmarshal gate_summary for %s: %w", s.ID, err)
			}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SimilarTop returns the single closest saved trajectory (full record) plus
// its distance. ok=false when recency-only or no embedded match exists.
func (l *Library) SimilarTop(ctx context.Context, queryVec []float32, class, server string) (*SavedTrajectory, float64, bool, error) {
	acceptedOnly := true
	sims, err := l.FindSimilar(ctx, queryVec, FindParams{Class: class, Server: server, Limit: 1, Accepted: &acceptedOnly})
	if err != nil || len(sims) == 0 {
		return nil, 0, false, err
	}
	st, err := l.Get(ctx, sims[0].ID)
	if err != nil {
		return nil, 0, false, err
	}
	return st, sims[0].Distance, true, nil
}

// Count returns the total number of saved trajectories.
func (l *Library) Count(ctx context.Context) (int, error) {
	if l == nil || l.db == nil {
		return 0, errors.New("buildlibrary.Count: nil library")
	}
	var n int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM saved_trajectories`).Scan(&n); err != nil {
		return 0, fmt.Errorf("query: %w", err)
	}
	return n, nil
}
