// Package buildlibrary persists generated trajectories the orchestrator
// produces, so future requests can retrieve them as reference points.
//
// The library is the organic replacement for hand-authored archetypes:
// real, validated builds accumulate over time and the LLM draws on them
// or diverges, never picking from a fixed template list.
//
// Currently SQLite-backed (one table, simple recency-ordered retrieval),
// pure-Go via modernc.org/sqlite (no CGO, cross-compiles cleanly).
// Vector search over reasoning text is a future enhancement.
//
// Save semantics: only trajectories whose scored snapshots clear all
// quality gates (no fail / fail_hard severity) get persisted. Warns
// flow through as breakdown context. The library doesn't accept junk.
package buildlibrary

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

// Library wraps a SQLite database file. Construct with Open; close with
// Close. Methods are safe to call concurrently; SQLite handles
// serialization internally and the connection pool is sized for it.
type Library struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and runs schema
// migration. Idempotent: opening an existing library is a no-op
// beyond opening the file. Path "" is rejected; callers must supply
// an explicit location so production deploys don't accidentally write
// to /tmp.
func Open(path string) (*Library, error) {
	if path == "" {
		return nil, fmt.Errorf("buildlibrary.Open: path is required")
	}
	// Apply FK enforcement via the DSN so the driver re-applies the
	// PRAGMA to every connection it opens. With MaxOpenConns=4, the pool
	// may spawn fresh connections; a one-shot ExecContext only reaches
	// the first connection and leaves the others with FK checks off.
	// _pragma= is supported by modernc.org/sqlite.
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	dsn := path + sep + "_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite at %s: %w", path, err)
	}
	// Connection pool sized for low-concurrency single-process use. The
	// API serves one /generate at a time per request, and saves are
	// fire-and-forget after the response goes out; no need for a deep
	// pool.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite at %s: %w", path, err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return &Library{db: db}, nil
}

// Close releases the database file.
func (l *Library) Close() error {
	if l == nil || l.db == nil {
		return nil
	}
	return l.db.Close()
}

// Ping verifies the underlying connection is healthy. Used by the
// /readyz probe; a panicked sqlite handle or unreachable file shows up
// here before any real query touches it.
func (l *Library) Ping(ctx context.Context) error {
	if l == nil || l.db == nil {
		return fmt.Errorf("buildlibrary: not opened")
	}
	return l.db.PingContext(ctx)
}

func migrate(db *sql.DB) error {
	ctx := context.Background()

	// Detect whether the schema has already been applied. If
	// `generations` exists, this DB was opened by a previous run and
	// migration is a no-op.
	var count int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='generations'`,
	).Scan(&count); err != nil {
		return fmt.Errorf("probe schema: %w", err)
	}

	if count == 0 {
		// Fresh DB: create both tables with the FK constraint inline.
		const initial = `
PRAGMA foreign_keys=ON;

CREATE TABLE generations (
	id              TEXT PRIMARY KEY,
	status          TEXT NOT NULL CHECK (status IN ('queued','running','completed','failed')),
	created_at      TIMESTAMP NOT NULL,
	started_at      TIMESTAMP,
	completed_at    TIMESTAMP,
	request_json    TEXT NOT NULL,
	attempts        INTEGER NOT NULL DEFAULT 0,
	failure_reason  TEXT,
	error_detail    TEXT,
	trace_json      TEXT
);
CREATE INDEX idx_generations_status_created
	ON generations(status, created_at);

CREATE TABLE saved_trajectories (
	id                TEXT PRIMARY KEY,
	created_at        TIMESTAMP NOT NULL,
	class             TEXT NOT NULL,
	server            TEXT NOT NULL,
	playstyle         TEXT NOT NULL,
	mode              TEXT NOT NULL,
	description       TEXT,
	primary_json      TEXT NOT NULL,
	alternatives_json TEXT,
	final_text        TEXT,
	gate_summary      TEXT NOT NULL,
	calc_version      TEXT,
	catalog_version   INTEGER,
	FOREIGN KEY (id) REFERENCES generations(id)
);
CREATE INDEX idx_saved_trajectories_lookup
	ON saved_trajectories (server, class, created_at DESC);
`
		if _, err := db.ExecContext(ctx, initial); err != nil {
			return fmt.Errorf("apply initial schema: %w", err)
		}
	}
	return nil
}

// SavedTrajectory is the full record as stored. Marshaled JSON columns
// inflate back into the domain types via json.Unmarshal; preserves the
// scored snapshots, gate results, and any annotations the orchestrator
// stamped.
type SavedTrajectory struct {
	ID             string              `json:"id"`
	CreatedAt      time.Time           `json:"created_at"`
	Class          string              `json:"class"`
	Server         string              `json:"server"`
	Playstyle      string              `json:"playstyle"`
	Mode           string              `json:"mode"`
	Description    string              `json:"description,omitempty"`
	Primary        domain.Trajectory   `json:"primary"`
	Alternatives   []domain.Trajectory `json:"alternatives,omitempty"`
	FinalText      string              `json:"final_text,omitempty"`
	GateSummary    GateSummary         `json:"gate_summary"`
	CalcVersion    string              `json:"calc_version,omitempty"`
	CatalogVersion int                 `json:"catalog_version,omitempty"`
}

// GateSummary aggregates per-snapshot gate counts so retrieval queries
// can show "this build passed 12 gates, has 2 warns" without parsing the
// full trajectory JSON.
type GateSummary struct {
	Pass     int `json:"pass"`
	Warn     int `json:"warn"`
	Fail     int `json:"fail"`      // Always 0 for saved records; fail-rejected before save.
	FailHard int `json:"fail_hard"` // Always 0 for saved records.
}

// Summary is the lightweight view returned by Find; just enough for
// the LLM's get_similar_past_builds tool to pick reference points
// without loading every full trajectory JSON.
type Summary struct {
	ID          string      `json:"id"`
	CreatedAt   time.Time   `json:"created_at"`
	Class       string      `json:"class"`
	Server      string      `json:"server"`
	Playstyle   string      `json:"playstyle"`
	Description string      `json:"description,omitempty"`
	GateSummary GateSummary `json:"gate_summary"`
}
