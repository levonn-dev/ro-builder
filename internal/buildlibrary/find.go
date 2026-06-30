package buildlibrary

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// FindParams narrows a Find call.
type FindParams struct {
	Class  string
	Server string
	Limit  int
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
SELECT id, created_at, class, server, playstyle, description, gate_summary
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
		if err := rows.Scan(&s.ID, &s.CreatedAt, &s.Class, &s.Server, &s.Playstyle, &s.Description, &gateJSON); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
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
       calc_version, catalog_version
FROM saved_trajectories WHERE id = $1
`
	var (
		st        SavedTrajectory
		primaryJS string
		altsJS    sql.NullString
		gateJS    string
	)
	err := l.db.QueryRowContext(ctx, q, id).Scan(
		&st.ID, &st.CreatedAt, &st.Class, &st.Server, &st.Playstyle, &st.Mode, &st.Description,
		&primaryJS, &altsJS, &st.FinalText, &gateJS, &st.CalcVersion, &st.CatalogVersion,
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
	return &st, nil
}

// ErrNotFound is returned by Get when no trajectory with the given id exists.
var ErrNotFound = errors.New("buildlibrary: trajectory not found")

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
