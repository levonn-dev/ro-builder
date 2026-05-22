package orchestrator

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

func TestSession_FirstAcceptWins(t *testing.T) {
	s := &Session{}
	first := AcceptedSubmission{Primary: domain.Trajectory{Class: "novice"}}
	second := AcceptedSubmission{Primary: domain.Trajectory{Class: "swordsman"}}

	if !s.Accept(first) {
		t.Fatalf("first Accept should return true")
	}
	if s.Accept(second) {
		t.Fatalf("second Accept should return false")
	}
	got := s.Accepted()
	if got == nil || got.Primary.Class != "novice" {
		t.Fatalf("Accepted: got %+v, want novice", got)
	}
}

func TestSession_AttemptsCounter(t *testing.T) {
	s := &Session{}
	s.RecordAttempt()
	s.RecordAttempt()
	s.RecordAttempt()
	if got := s.Attempts(); got != 3 {
		t.Fatalf("Attempts: got %d, want 3", got)
	}
}

func TestSessionFromContext_RoundTrip(t *testing.T) {
	s := &Session{}
	ctx := WithSession(context.Background(), s)
	if got := SessionFromContext(ctx); got != s {
		t.Fatalf("round-trip lost the session")
	}
	if got := SessionFromContext(context.Background()); got != nil {
		t.Fatalf("empty ctx should return nil session, got %v", got)
	}
}
