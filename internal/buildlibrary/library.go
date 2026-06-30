// Package buildlibrary persists generated trajectories the orchestrator
// produces, so future requests can retrieve them as reference points.
//
// PostgreSQL-backed via database/sql + the pgx stdlib driver (pure-Go,
// CGO-free). Schema is managed by embedded golang-migrate migrations,
// applied on Open. Two tables: an async-generation queue (with lease
// columns for multi-pod-safe claim/recovery) and the saved trajectories.
// Vector search over reasoning text is a future enhancement.
//
// Save semantics: only trajectories whose scored snapshots clear all
// quality gates (no fail / fail_hard severity) get persisted.
package buildlibrary

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Library wraps a PostgreSQL connection pool. Construct with Open; close
// with Close. Methods are safe for concurrent use.
type Library struct {
	db *sql.DB
}

// Open connects to the Postgres database at dsn, verifies the connection,
// and applies embedded migrations. dsn "" is rejected; callers must supply
// an explicit connection string.
func Open(ctx context.Context, dsn string) (*Library, error) {
	if dsn == "" {
		return nil, fmt.Errorf("buildlibrary.Open: dsn is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxIdleTime(5 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Library{db: db}, nil
}

// runMigrations applies embedded migrations using the app's *sql.DB.
// golang-migrate takes a cluster advisory lock for the duration, so
// concurrent pod startups serialize safely (one migrates, others no-op).
// We never call m.Close(): that would close the shared *sql.DB.
func runMigrations(db *sql.DB) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("open migrations fs: %w", err)
	}
	drv, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	if err != nil {
		return fmt.Errorf("migrate driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "pgx5", drv)
	if err != nil {
		return fmt.Errorf("migrate init: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// Close releases the database connection pool.
func (l *Library) Close() error {
	if l == nil || l.db == nil {
		return nil
	}
	return l.db.Close()
}

// Ping verifies the underlying connection is healthy. Used by the
// /readyz probe.
func (l *Library) Ping(ctx context.Context) error {
	if l == nil || l.db == nil {
		return fmt.Errorf("buildlibrary: not opened")
	}
	return l.db.PingContext(ctx)
}

// Truncate removes all rows from both tables. Used in tests to reset state.
func (l *Library) Truncate(ctx context.Context) error {
	if l == nil || l.db == nil {
		return errors.New("buildlibrary.Truncate: nil library")
	}
	_, err := l.db.ExecContext(ctx, `TRUNCATE saved_trajectories, generations CASCADE`)
	return err
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
