package buildlibrary

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

// SaveInput is what callers pass to Save. Mirrors the data the
// orchestrator already has after a successful Generate; request fields
// + the scored Primary + Alternatives. The library marshals the
// trajectories itself so the save call is a single function pass-through
// from the orchestrator's perspective.
//
// ID must be a pre-allocated identifier that already exists in the
// generations table. The worker allocates it at enqueue time so the
// async job tracking row and the saved trajectory share the same key.
type SaveInput struct {
	ID             string // required; must match an existing generations.id
	Owner          string // lease owner that claimed this generation; scopes the completion UPDATE
	Class          string
	Server         string
	Playstyle      string
	Mode           string
	Description    string
	Primary        domain.Trajectory
	Alternatives   []domain.Trajectory
	FinalText      string
	CalcVersion    string
	CatalogVersion int
}

// insertSavedTrajectory is the shared INSERT used by Save and SaveAndComplete.
const insertSavedTrajectory = `
INSERT INTO saved_trajectories
	(id, created_at, class, server, playstyle, mode, description,
	 primary_json, alternatives_json, final_text, gate_summary,
	 calc_version, catalog_version)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10, $11::jsonb, $12, $13)
`

// ErrFailingGates is returned by Save when at least one scored snapshot
// in Primary or Alternatives carries a fail / fail_hard gate result.
// The library rejects rather than persisting junk.
var ErrFailingGates = errors.New("trajectory has failing quality gates; not persisting")

// ErrNoScoredSnapshots is returned by Save when Primary contains zero
// snapshots with a non-nil Score. Without scoring, the gate guard has
// nothing to evaluate (it sees zero results, no fail severities, and
// would otherwise pass); but a build with no scoring data is junk
// from the library's perspective: trajectories must carry at least one
// scored snapshot to qualify for save.
var ErrNoScoredSnapshots = errors.New("trajectory has no scored snapshots; not persisting")

// ErrGatesNotEvaluated is returned by Save when a scored snapshot in
// Primary carries an empty Gates slice. That state means scoring ran
// (so we can't fall back to ErrNoScoredSnapshots) but the orchestrator
// was wired without a catalog; the gates evaluator was skipped, and
// without it the library would silently persist trajectories that
// would otherwise fail hit/EHP/requirements floors. Surfaced as a
// distinct error so the operator can fix the wiring (orchestrator
// needs WithCatalog) rather than chasing a malformed trajectory.
var ErrGatesNotEvaluated = errors.New("trajectory has scored snapshots without gate results; not persisting (orchestrator wired without WithCatalog?)")

// Save writes a SaveInput to the library if and only if every scored
// snapshot in Primary and every Alternative passes its quality gates
// (no fail / fail_hard severity). On gate-rejection the library returns
// ErrFailingGates and writes nothing; callers can log + continue.
//
// in.ID must be a non-empty string that already exists in the
// generations table; Save returns an error immediately if it is empty.
// Returns in.ID on success.
func (l *Library) Save(ctx context.Context, in SaveInput) (string, error) {
	if l == nil || l.db == nil {
		return "", errors.New("buildlibrary.Save: nil library")
	}
	if in.ID == "" {
		return "", errors.New("buildlibrary.Save: ID is required")
	}
	summary, err := summarizeAndGuard(in.Primary, in.Alternatives)
	if err != nil {
		return "", err
	}

	primaryJSON, err := json.Marshal(in.Primary)
	if err != nil {
		return "", fmt.Errorf("marshal primary: %w", err)
	}
	var altsJSON []byte
	if len(in.Alternatives) > 0 {
		altsJSON, err = json.Marshal(in.Alternatives)
		if err != nil {
			return "", fmt.Errorf("marshal alternatives: %w", err)
		}
	}
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return "", fmt.Errorf("marshal gate_summary: %w", err)
	}

	if _, err := l.db.ExecContext(ctx, insertSavedTrajectory,
		in.ID, time.Now().UTC(),
		in.Class, in.Server, in.Playstyle, in.Mode, in.Description,
		string(primaryJSON), nullableJSON(altsJSON), in.FinalText, string(summaryJSON),
		in.CalcVersion, in.CatalogVersion,
	); err != nil {
		return "", fmt.Errorf("insert: %w", err)
	}
	return in.ID, nil
}

// SaveAndComplete atomically inserts the trajectory into saved_trajectories
// AND marks the corresponding generation as completed, inside a single
// transaction. A process crash between the two writes cannot leave an
// orphaned saved_trajectories row whose generation shows as failed in
// RecoverOrphans. The UPDATE is scoped to lease_owner=in.Owner so a stale
// worker that lost its lease cannot accidentally complete a job it no
// longer owns.
//
// Prefer this over calling Save + MarkCompleted separately. Save is kept
// for callers (tests, tooling) that do not own a generation row.
func (l *Library) SaveAndComplete(ctx context.Context, in SaveInput) (string, error) {
	if l == nil || l.db == nil {
		return "", errors.New("buildlibrary.SaveAndComplete: nil library")
	}
	if in.ID == "" {
		return "", errors.New("buildlibrary.SaveAndComplete: ID is required")
	}
	summary, err := summarizeAndGuard(in.Primary, in.Alternatives)
	if err != nil {
		return "", err
	}
	primaryJSON, err := json.Marshal(in.Primary)
	if err != nil {
		return "", fmt.Errorf("marshal primary: %w", err)
	}
	var altsJSON []byte
	if len(in.Alternatives) > 0 {
		altsJSON, err = json.Marshal(in.Alternatives)
		if err != nil {
			return "", fmt.Errorf("marshal alternatives: %w", err)
		}
	}
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return "", fmt.Errorf("marshal gate_summary: %w", err)
	}

	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, insertSavedTrajectory,
		in.ID, time.Now().UTC(),
		in.Class, in.Server, in.Playstyle, in.Mode, in.Description,
		string(primaryJSON), nullableJSON(altsJSON), in.FinalText, string(summaryJSON),
		in.CalcVersion, in.CatalogVersion,
	); err != nil {
		return "", fmt.Errorf("insert: %w", err)
	}

	res, err := tx.ExecContext(ctx, `
UPDATE generations
   SET status='completed', completed_at=$1, lease_expires_at=NULL, lease_owner=NULL
 WHERE id=$2 AND status='running' AND lease_owner=$3`,
		time.Now().UTC(), in.ID, in.Owner)
	if err != nil {
		return "", fmt.Errorf("mark completed: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", fmt.Errorf("buildlibrary.SaveAndComplete id=%s: %w", in.ID, ErrAlreadyTerminal)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	committed = true
	return in.ID, nil
}

// summarizeAndGuard scans every scored snapshot's Gates and returns the
// aggregate counts. Three reject paths:
//
//   - Any fail / fail_hard in Primary or Alternatives → ErrFailingGates.
//   - Zero scored snapshots in Primary → ErrNoScoredSnapshots. Catches
//     trajectories where canonical scoring couldn't run (e.g., calc
//     rejected the inputs) and the resulting empty-gates trajectory
//     would otherwise sneak through the fail-severity check.
//   - Any scored snapshot in Primary with empty Gates → ErrGatesNotEvaluated.
//     Catches the misconfigured-orchestrator case where scoring ran but
//     the catalog dependency was missing, leaving no gate results to
//     evaluate. Without this guard, junk trajectories silently persist.
//
// Alternatives can be entirely unscored without rejection; Primary is
// the user-facing answer; Alternatives are optional peer suggestions.
func summarizeAndGuard(primary domain.Trajectory, alts []domain.Trajectory) (GateSummary, error) {
	var sum GateSummary
	scoredInPrimary := 0
	for _, s := range primary.Snapshots {
		if s.Score != nil {
			scoredInPrimary++
			if len(s.Gates) == 0 {
				return sum, ErrGatesNotEvaluated
			}
		}
		if err := tallyGates(&sum, s.Gates); err != nil {
			return sum, err
		}
	}
	if scoredInPrimary == 0 {
		return sum, ErrNoScoredSnapshots
	}
	for _, alt := range alts {
		for _, s := range alt.Snapshots {
			if err := tallyGates(&sum, s.Gates); err != nil {
				return sum, err
			}
		}
	}
	return sum, nil
}

// tallyGates accumulates one snapshot's gate severities into the
// summary. Any fail / fail_hard short-circuits via ErrFailingGates;
// the library refuses to persist a trajectory carrying them.
func tallyGates(sum *GateSummary, gs []domain.GateResult) error {
	for _, g := range gs {
		switch g.Severity {
		case domain.GateSeverityPass:
			sum.Pass++
		case domain.GateSeverityWarn:
			sum.Warn++
		case domain.GateSeverityFail:
			return fmt.Errorf("%w: gate %q failed (threshold=%v actual=%v)",
				ErrFailingGates, g.Name, g.Threshold, g.Actual)
		case domain.GateSeverityFailHard:
			return fmt.Errorf("%w: gate %q hard-failed (threshold=%v actual=%v)",
				ErrFailingGates, g.Name, g.Threshold, g.Actual)
		}
	}
	return nil
}

// nullableJSON wraps a JSON byte slice for sql/driver so absent payloads
// land in the database as SQL NULL (not as the literal string "").
// Reads via sql.NullString round-trip cleanly: Valid=false ↔ NULL,
// Valid=true ↔ string value.
func nullableJSON(b []byte) sql.NullString {
	if len(b) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{String: string(b), Valid: true}
}
