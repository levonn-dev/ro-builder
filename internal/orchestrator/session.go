package orchestrator

import (
	"context"
	"sync"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

// AcceptedSubmission carries a passing submit_trajectory result;
// trajectories with canonical scoring stamped, the per-snapshot gates,
// and the calc engine version. Recorded once per session,
// first-pass-wins: later submit_trajectory calls return a success no-op
// and don't replace it.
type AcceptedSubmission struct {
	Primary      domain.Trajectory
	Alternatives []domain.Trajectory
	CalcVersion  string
}

// Session is the per-request state the orchestrator threads through
// the LLM tool-use loop. submit_trajectory writes Accepted on the
// first passing call; the orchestrator reads it after end_turn.
//
// Attempts increments on every submit_trajectory call regardless of
// outcome; useful for ops telemetry.
//
// Safe for concurrent use, though in practice the tool dispatch loop
// is single-goroutine per request.
type Session struct {
	mu       sync.Mutex
	accepted *AcceptedSubmission
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

// Accept records the first passing submission. Subsequent calls are
// no-ops. Returns true if this call set the value, false if a previous
// call had already set it (first-pass-wins).
func (s *Session) Accept(a AcceptedSubmission) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accepted != nil {
		return false
	}
	s.accepted = &a
	return true
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
