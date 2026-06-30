package buildlibrary

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// GenStatus values mirror the CHECK constraint on generations.status.
type GenStatus string

const (
	GenStatusQueued    GenStatus = "queued"
	GenStatusRunning   GenStatus = "running"
	GenStatusCompleted GenStatus = "completed"
	GenStatusFailed    GenStatus = "failed"
)

// FailureReason values are the closed enum of operator-facing reasons
// a generation row may land in status=failed with.
type FailureReason string

const (
	FailureMaxItersExhausted  FailureReason = "max_iters_exhausted"
	FailureNoSubmission       FailureReason = "no_submission"
	FailureProviderError      FailureReason = "provider_error"
	FailureProviderAuthError  FailureReason = "provider_auth_error"
	FailureProviderMaxTokens  FailureReason = "provider_max_tokens"
	FailureSidecarError       FailureReason = "sidecar_error"
	FailureValidationLate     FailureReason = "validation_error_late"
	FailureInterruptedRestart FailureReason = "interrupted_on_restart"
	FailureShutdownInterrupt  FailureReason = "shutdown_interrupted"
	FailureLeaseExpired       FailureReason = "lease_expired"
)

// enqueueAdvisoryLockKey serializes EnqueueIfUnderCap's count-then-insert
// across all connections and pods via pg_advisory_xact_lock. The value is
// arbitrary but must be stable.
const enqueueAdvisoryLockKey int64 = 4201

// Generation is the typed projection of one row in the generations table.
type Generation struct {
	ID            string
	Status        GenStatus
	CreatedAt     time.Time
	StartedAt     *time.Time
	CompletedAt   *time.Time
	RequestJSON   json.RawMessage
	Attempts      int
	FailureReason FailureReason
	ErrorDetail   string
	TraceJSON     json.RawMessage
}

// ErrGenerationNotFound is returned by GetGeneration when no row matches the id.
var ErrGenerationNotFound = errors.New("generation not found")

// ErrAlreadyTerminal is returned by MarkCompleted / MarkFailed when the row
// is no longer in a running state with the expected owner. This covers both
// "already completed/failed" and "lease was reassigned to another worker."
// Workers suppress this with errors.Is during shutdown drain.
var ErrAlreadyTerminal = errors.New("generation already in terminal state")

// Enqueue inserts a new generations row with status=queued and returns
// the generated id (128-bit hex).
func (l *Library) Enqueue(ctx context.Context, request json.RawMessage) (string, error) {
	if l == nil || l.db == nil {
		return "", errors.New("buildlibrary.Enqueue: nil library")
	}
	id, err := newID()
	if err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	_, err = l.db.ExecContext(ctx,
		`INSERT INTO generations (id, status, created_at, request_json)
		 VALUES ($1, 'queued', $2, $3::jsonb)`,
		id, time.Now().UTC(), string(request))
	if err != nil {
		return "", fmt.Errorf("insert generation: %w", err)
	}
	return id, nil
}

// GetGeneration returns the typed Generation for id, or
// ErrGenerationNotFound if no row matches.
func (l *Library) GetGeneration(ctx context.Context, id string) (*Generation, error) {
	if l == nil || l.db == nil {
		return nil, errors.New("buildlibrary.GetGeneration: nil library")
	}
	row := l.db.QueryRowContext(ctx,
		`SELECT id, status, created_at, started_at, completed_at,
		        request_json, attempts, failure_reason, error_detail, trace_json
		 FROM generations WHERE id = $1`, id)

	var g Generation
	var startedAt, completedAt sql.NullTime
	var failureReason, errorDetail, traceJSON sql.NullString
	var requestJSON string
	err := row.Scan(&g.ID, &g.Status, &g.CreatedAt, &startedAt, &completedAt,
		&requestJSON, &g.Attempts, &failureReason, &errorDetail, &traceJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrGenerationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan generation: %w", err)
	}
	if startedAt.Valid {
		g.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		g.CompletedAt = &completedAt.Time
	}
	g.RequestJSON = json.RawMessage(requestJSON)
	if failureReason.Valid {
		g.FailureReason = FailureReason(failureReason.String)
	}
	g.ErrorDetail = errorDetail.String
	if traceJSON.Valid {
		g.TraceJSON = json.RawMessage(traceJSON.String)
	}
	return &g, nil
}

// InFlightCount returns the number of generations currently in status
// 'queued' or 'running'.
func (l *Library) InFlightCount(ctx context.Context) (int, error) {
	if l == nil || l.db == nil {
		return 0, errors.New("buildlibrary.InFlightCount: nil library")
	}
	var n int
	if err := l.db.QueryRowContext(ctx,
		`SELECT count(*) FROM generations WHERE status IN ('queued','running')`).Scan(&n); err != nil {
		return 0, fmt.Errorf("buildlibrary.InFlightCount: %w", err)
	}
	return n, nil
}

// ClaimNext atomically claims the oldest queued generation for `owner`,
// setting status=running and a lease that expires after leaseTTL. Uses
// FOR UPDATE SKIP LOCKED so concurrent workers (across pods) grab distinct
// rows without blocking. Returns (nil, nil) when nothing is queued.
func (l *Library) ClaimNext(ctx context.Context, owner string, leaseTTL time.Duration) (*Generation, error) {
	if l == nil || l.db == nil {
		return nil, errors.New("buildlibrary.ClaimNext: nil library")
	}
	now := time.Now().UTC()
	row := l.db.QueryRowContext(ctx, `
UPDATE generations
   SET status='running', started_at=$1, attempts=attempts+1,
       lease_expires_at=$2, lease_owner=$3
 WHERE id = (
   SELECT id FROM generations
    WHERE status='queued'
    ORDER BY created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
 )
RETURNING id, status, created_at, started_at, completed_at,
          request_json, attempts, failure_reason, error_detail, trace_json
`, now, now.Add(leaseTTL), owner)

	var g Generation
	var startedAt, completedAt sql.NullTime
	var failureReason, errorDetail, traceJSON sql.NullString
	var requestJSON string
	err := row.Scan(&g.ID, &g.Status, &g.CreatedAt, &startedAt, &completedAt,
		&requestJSON, &g.Attempts, &failureReason, &errorDetail, &traceJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buildlibrary.ClaimNext: %w", err)
	}
	if startedAt.Valid {
		g.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		g.CompletedAt = &completedAt.Time
	}
	g.RequestJSON = json.RawMessage(requestJSON)
	if failureReason.Valid {
		g.FailureReason = FailureReason(failureReason.String)
	}
	g.ErrorDetail = errorDetail.String
	if traceJSON.Valid {
		g.TraceJSON = json.RawMessage(traceJSON.String)
	}
	return &g, nil
}

// RenewLease extends the lease on a running generation the caller owns.
// Returns false (without error) when the row is no longer running or no
// longer owned by `owner` (lease lost); the caller should abort the job.
func (l *Library) RenewLease(ctx context.Context, id, owner string, leaseTTL time.Duration) (bool, error) {
	if l == nil || l.db == nil {
		return false, errors.New("buildlibrary.RenewLease: nil library")
	}
	res, err := l.db.ExecContext(ctx, `
UPDATE generations SET lease_expires_at=$1
 WHERE id=$2 AND status='running' AND lease_owner=$3`,
		time.Now().UTC().Add(leaseTTL), id, owner)
	if err != nil {
		return false, fmt.Errorf("buildlibrary.RenewLease: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// MarkCompleted flips an in-flight generation owned by `owner` to
// status=completed. owner must match the lease owner that claimed it.
func (l *Library) MarkCompleted(ctx context.Context, id, owner string) error {
	return l.markTerminal(ctx, id, owner, GenStatusCompleted, "", "", nil)
}

// MarkFailed flips an in-flight generation owned by `owner` to status=failed.
func (l *Library) MarkFailed(ctx context.Context, id, owner string, reason FailureReason, detail string, trace json.RawMessage) error {
	return l.markTerminal(ctx, id, owner, GenStatusFailed, reason, detail, trace)
}

func (l *Library) markTerminal(ctx context.Context, id, owner string, status GenStatus, reason FailureReason, detail string, trace json.RawMessage) error {
	if l == nil || l.db == nil {
		return errors.New("buildlibrary.markTerminal: nil library")
	}
	now := time.Now().UTC()
	var reasonArg sql.NullString
	if reason != "" {
		reasonArg = sql.NullString{String: string(reason), Valid: true}
	}
	var detailArg sql.NullString
	if detail != "" {
		detailArg = sql.NullString{String: detail, Valid: true}
	}
	var traceArg sql.NullString
	if len(trace) > 0 {
		traceArg = sql.NullString{String: string(trace), Valid: true}
	}
	res, err := l.db.ExecContext(ctx, `
UPDATE generations
   SET status=$1, completed_at=$2, failure_reason=$3, error_detail=$4,
       trace_json=$5::jsonb, lease_expires_at=NULL, lease_owner=NULL
 WHERE id=$6 AND status='running' AND lease_owner=$7`,
		string(status), now, reasonArg, detailArg, traceArg, id, owner)
	if err != nil {
		return fmt.Errorf("buildlibrary.markTerminal (%s): %w", status, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("buildlibrary.markTerminal id=%s: %w", id, ErrAlreadyTerminal)
	}
	return nil
}

// ErrQueueAtCapacity is returned by EnqueueIfUnderCap when the number
// of in-flight generations is at or above the supplied cap.
var ErrQueueAtCapacity = errors.New("generation queue at capacity")

// EnqueueIfUnderCap atomically checks count(*) WHERE status IN
// ('queued','running') against maxInFlight and inserts a queued row if
// under. Returns ErrQueueAtCapacity if the cap is met or exceeded.
//
// Uses pg_advisory_xact_lock so the count check and insert serialize
// against concurrent callers across all pods; the lock auto-releases at
// commit/rollback.
func (l *Library) EnqueueIfUnderCap(ctx context.Context, maxInFlight int, request json.RawMessage) (string, error) {
	if l == nil || l.db == nil {
		return "", errors.New("buildlibrary.EnqueueIfUnderCap: nil library")
	}
	if maxInFlight <= 0 {
		return "", errors.New("buildlibrary.EnqueueIfUnderCap: cap must be > 0")
	}
	id, err := newID()
	if err != nil {
		return "", fmt.Errorf("generate id: %w", err)
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

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, enqueueAdvisoryLockKey); err != nil {
		return "", fmt.Errorf("advisory lock: %w", err)
	}

	var n int
	if err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM generations WHERE status IN ('queued','running')`,
	).Scan(&n); err != nil {
		return "", fmt.Errorf("count in-flight: %w", err)
	}
	if n >= maxInFlight {
		return "", ErrQueueAtCapacity
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO generations (id, status, created_at, request_json)
		 VALUES ($1, 'queued', $2, $3::jsonb)`,
		id, time.Now().UTC(), string(request),
	); err != nil {
		return "", fmt.Errorf("insert generation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	committed = true
	return id, nil
}

// RecoverExpiredLeases handles generations whose lease has expired (worker
// died or stalled). With maxAttempts > 0 it requeues rows that still have
// retry budget (attempts < maxAttempts); the remainder (budget exhausted,
// or retry disabled with maxAttempts == 0) are failed with reason
// lease_expired. Idempotent and safe to run concurrently across pods.
// Returns counts of requeued and failed rows.
func (l *Library) RecoverExpiredLeases(ctx context.Context, maxAttempts int) (requeued, failed int, err error) {
	if l == nil || l.db == nil {
		return 0, 0, errors.New("buildlibrary.RecoverExpiredLeases: nil library")
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, fmt.Errorf("begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	now := time.Now().UTC()
	rq, err := tx.ExecContext(ctx, `
UPDATE generations
   SET status='queued', lease_expires_at=NULL, lease_owner=NULL
 WHERE status='running' AND lease_expires_at < $1
   AND $2 > 0 AND attempts < $2`, now, maxAttempts)
	if err != nil {
		return 0, 0, fmt.Errorf("requeue: %w", err)
	}
	rqN, _ := rq.RowsAffected()

	fl, err := tx.ExecContext(ctx, `
UPDATE generations
   SET status='failed', completed_at=$1, failure_reason=$2,
       error_detail='lease expired (worker died or stalled)',
       lease_expires_at=NULL, lease_owner=NULL
 WHERE status='running' AND lease_expires_at < $1`, now, string(FailureLeaseExpired))
	if err != nil {
		return 0, 0, fmt.Errorf("fail: %w", err)
	}
	flN, _ := fl.RowsAffected()

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit: %w", err)
	}
	committed = true
	return int(rqN), int(flN), nil
}

// newID returns a 128-bit hex token.
func newID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
