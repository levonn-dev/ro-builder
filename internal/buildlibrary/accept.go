package buildlibrary

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Accept marks a saved trajectory as user-accepted, making it visible to the
// RAG retrieval surface (proactive seed, get_similar_past_builds,
// get_saved_build). Idempotent: re-accepting keeps the original timestamp via
// COALESCE. Returns the accepted-at time, or ErrNotFound if no trajectory has
// the given id.
func (l *Library) Accept(ctx context.Context, id string) (time.Time, error) {
	if l == nil || l.db == nil {
		return time.Time{}, errors.New("buildlibrary.Accept: nil library")
	}
	var at time.Time
	err := l.db.QueryRowContext(ctx,
		`UPDATE saved_trajectories SET accepted_at = COALESCE(accepted_at, now()) WHERE id = $1 RETURNING accepted_at`,
		id).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, ErrNotFound
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("accept: %w", err)
	}
	return at, nil
}
