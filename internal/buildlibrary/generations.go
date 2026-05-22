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
)

// Generation is the typed projection of one row in the generations
// table. Times are nil-able where the schema is.
//
// For nullable string-ish columns (failure_reason, error_detail), an
// empty value means NULL in the DB. The FailureReason enum's
// constants are all non-empty, so callers can use `g.FailureReason
// == ""` to detect "not yet set." Same convention for ErrorDetail.
// TraceJSON uses a nil slice instead of an empty slice; for
// downstream JSON encoders both produce a null, so nil is the canonical
// "not set" representation.
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

// ErrAlreadyTerminal is returned by MarkCompleted / MarkFailed when
// the row is no longer in a queued/running state; i.e., it was
// already transitioned to a terminal status by a prior call. Workers
// suppress this with errors.Is during shutdown drain to avoid logging
// expected race conditions as real errors.
var ErrAlreadyTerminal = errors.New("generation already in terminal state")

// Enqueue inserts a new generations row with status=queued and returns
// the generated id (128-bit hex). request is the original API request
// payload preserved verbatim.
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
		 VALUES (?, 'queued', ?, ?)`,
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
		 FROM generations WHERE id = ?`, id)

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
// 'queued' or 'running'. Used by the POST handler for queue cap
// backpressure.
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

// ClaimNext atomically transitions the oldest queued generation to
// status=running and returns its row. Returns (nil, nil) when no rows
// are queued; callers treat that as "go back to sleep."
//
// Multiple workers calling ClaimNext concurrently race on SQLite's
// writer lock; exactly one wins each available row, the others get
// (nil, nil).
func (l *Library) ClaimNext(ctx context.Context) (*Generation, error) {
	if l == nil || l.db == nil {
		return nil, errors.New("buildlibrary.ClaimNext: nil library")
	}
	now := time.Now().UTC()
	row := l.db.QueryRowContext(ctx, `
UPDATE generations
   SET status='running', started_at=?, attempts=attempts+1
 WHERE id = (
   SELECT id FROM generations
    WHERE status='queued'
    ORDER BY created_at LIMIT 1
 )
RETURNING id, status, created_at, started_at, completed_at,
          request_json, attempts, failure_reason, error_detail, trace_json
`, now)
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

// MarkCompleted flips an in-flight generation to status=completed and
// records the completion timestamp. The caller must have already
// written the corresponding saved_trajectories row (it FK-references
// this id) before calling MarkCompleted.
func (l *Library) MarkCompleted(ctx context.Context, id string) error {
	return l.markTerminal(ctx, id, GenStatusCompleted, "", "", nil)
}

// MarkFailed flips an in-flight generation to status=failed with the
// given reason + detail + (optionally) the LLM conversation trace for
// forensics. Reason must be one of the FailureReason constants; the
// status table treats it as a closed enum.
func (l *Library) MarkFailed(ctx context.Context, id string, reason FailureReason, detail string, trace json.RawMessage) error {
	return l.markTerminal(ctx, id, GenStatusFailed, reason, detail, trace)
}

func (l *Library) markTerminal(ctx context.Context, id string, status GenStatus, reason FailureReason, detail string, trace json.RawMessage) error {
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
   SET status=?, completed_at=?, failure_reason=?, error_detail=?, trace_json=?
 WHERE id=? AND status IN ('queued','running')`,
		string(status), now, reasonArg, detailArg, traceArg, id)
	if err != nil {
		return fmt.Errorf("buildlibrary.markTerminal (%s): %w", status, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("buildlibrary.markTerminal id=%s: %w", id, ErrAlreadyTerminal)
	}
	return nil
}

// RecoverOrphans flips every generations row in status='running' to
// status='failed' with reason='interrupted_on_restart'. Run once at
// startup before any worker begins polling. Returns the number of rows
// recovered.
func (l *Library) RecoverOrphans(ctx context.Context) (int, error) {
	if l == nil || l.db == nil {
		return 0, errors.New("buildlibrary.RecoverOrphans: nil library")
	}
	res, err := l.db.ExecContext(ctx, `
UPDATE generations
   SET status='failed',
       completed_at=?,
       failure_reason=?,
       error_detail='process restarted while job was running'
 WHERE status='running'`,
		time.Now().UTC(), string(FailureInterruptedRestart))
	if err != nil {
		return 0, fmt.Errorf("buildlibrary.RecoverOrphans: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ErrQueueAtCapacity is returned by EnqueueIfUnderCap when the number
// of in-flight generations (status IN ('queued','running')) is at or
// above the supplied cap. The caller (POST handler) maps this to HTTP
// 429.
var ErrQueueAtCapacity = errors.New("generation queue at capacity")

// EnqueueIfUnderCap atomically checks count(*) WHERE status IN
// ('queued','running') against maxInFlight and inserts a queued row if
// under. Returns ErrQueueAtCapacity if the cap is met or exceeded.
//
// Uses BEGIN IMMEDIATE on a dedicated connection so the count check
// and insert serialize against concurrent callers. database/sql's
// BeginTx defaults to BEGIN DEFERRED, which would let two callers
// both take a SHARED-lock snapshot showing count=maxInFlight-1 and
// both insert; the cap would be exceeded. BEGIN IMMEDIATE acquires
// the RESERVED writer lock at BEGIN time, so concurrent callers wait
// for the busy_timeout the DSN sets (5s) before proceeding.
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

	conn, err := l.db.Conn(ctx)
	if err != nil {
		return "", fmt.Errorf("acquire conn: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return "", fmt.Errorf("begin immediate: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	var n int
	if err := conn.QueryRowContext(ctx,
		`SELECT count(*) FROM generations WHERE status IN ('queued','running')`,
	).Scan(&n); err != nil {
		return "", fmt.Errorf("count in-flight: %w", err)
	}
	if n >= maxInFlight {
		return "", ErrQueueAtCapacity
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO generations (id, status, created_at, request_json)
		 VALUES (?, 'queued', ?, ?)`,
		id, time.Now().UTC(), string(request),
	); err != nil {
		return "", fmt.Errorf("insert generation: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	committed = true
	return id, nil
}

// newID returns a 128-bit hex token. Used by Enqueue for the generation
// id. Saved trajectories reuse the same id their generation owns.
func newID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}
