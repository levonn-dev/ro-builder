package orchestrator

import (
	"context"
	"sync"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

// AcceptedSubmission carries a passing submit_trajectory result;
// trajectories with canonical scoring stamped, the per-snapshot gates,
// and the calc engine version. Held as the session's current answer of
// record under first-clean-wins (see Session.Accept).
type AcceptedSubmission struct {
	Primary      domain.Trajectory
	Alternatives []domain.Trajectory
	CalcVersion  string
}

// Session is the per-request state the orchestrator threads through
// the LLM tool-use loop. submit_trajectory writes Accepted via Accept;
// the orchestrator reads it after end_turn.
//
// Acceptance is "first clean pass wins": a warning-free submission locks
// the session and can't be replaced; until one arrives, the latest
// submission (which may carry warnings such as an under-spent stat
// budget) is kept and a cleaner resubmit replaces it.
//
// Attempts increments on every submit_trajectory call regardless of
// outcome; useful for ops telemetry.
//
// Safe for concurrent use, though in practice the tool dispatch loop
// is single-goroutine per request.
type Session struct {
	mu       sync.Mutex
	accepted *AcceptedSubmission
	locked   bool
	attempts int
}

// RecordAttempt increments the attempts counter. Called by the
// orchestrator's dispatch path each time the LLM invokes
// submit_trajectory, regardless of outcome.
func (s *Session) RecordAttempt() {
	s.mu.Lock()
	s.attempts++
	s.mu.Unlock()
}

// Attempts returns the current count of submit_trajectory invocations
// recorded against this session.
func (s *Session) Attempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts
}

// Accept records a passing submission as the current answer of record.
// clean reports whether the submission's primary trajectory carried no
// warnings. The first clean submission locks the session; once locked,
// later submissions are ignored. Until then the most recent submission is
// kept, so a cleaner resubmit replaces a provisional (warned) one.
//
// Returns true when the session is now locked (this is the final answer of
// record), false when it remains provisional and a cleaner resubmit could
// still replace it.
func (s *Session) Accept(a AcceptedSubmission, clean bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return true
	}
	s.accepted = &a
	if clean {
		s.locked = true
	}
	return s.locked
}

// Accepted returns a pointer to the accepted submission, or nil if no
// submit_trajectory call has passed gates yet.
func (s *Session) Accepted() *AcceptedSubmission {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accepted
}

type sessionCtxKey struct{}

// WithSession attaches s to ctx. The orchestrator calls this once
// per Generate; tools retrieve via SessionFromContext.
func WithSession(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, sessionCtxKey{}, s)
}

// SessionFromContext returns the session attached by WithSession, or
// nil if none was attached.
func SessionFromContext(ctx context.Context) *Session {
	if v, ok := ctx.Value(sessionCtxKey{}).(*Session); ok {
		return v
	}
	return nil
}
