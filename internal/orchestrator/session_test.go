package orchestrator

import (
	"context"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

// "First clean pass wins": a warning-free submission locks the session;
// a submission carrying only warnings (e.g. an under-spent stat budget) is
// provisional and can be replaced by a later, cleaner resubmit.
func TestSession_CleanSubmissionLocks(t *testing.T) {
	s := &Session{}
	clean := AcceptedSubmission{Primary: domain.Trajectory{Class: "novice"}}
	if locked := s.Accept(clean, true); !locked {
		t.Fatalf("a clean submission should lock the session (locked=true)")
	}
	other := AcceptedSubmission{Primary: domain.Trajectory{Class: "swordsman"}}
	if locked := s.Accept(other, true); !locked {
		t.Fatalf("once locked, Accept should keep reporting locked=true")
	}
	if got := s.Accepted(); got == nil || got.Primary.Class != "novice" {
		t.Fatalf("a locked submission must not be replaced; got %+v, want novice", got)
	}
}

func TestSession_ProvisionalReplacedByCleanerSubmission(t *testing.T) {
	s := &Session{}
	warned := AcceptedSubmission{Primary: domain.Trajectory{Class: "novice"}}
	if locked := s.Accept(warned, false); locked {
		t.Fatalf("a warned submission is provisional, not locked")
	}
	if got := s.Accepted(); got == nil || got.Primary.Class != "novice" {
		t.Fatalf("provisional submission should be the current answer of record; got %+v", got)
	}
	clean := AcceptedSubmission{Primary: domain.Trajectory{Class: "high_novice"}}
	if locked := s.Accept(clean, true); !locked {
		t.Fatalf("a clean resubmit should replace the provisional and lock")
	}
	if got := s.Accepted(); got == nil || got.Primary.Class != "high_novice" {
		t.Fatalf("the clean submission should have replaced the provisional; got %+v", got)
	}
}

func TestSession_LatestProvisionalKeptWhileWarned(t *testing.T) {
	s := &Session{}
	s.Accept(AcceptedSubmission{Primary: domain.Trajectory{Class: "first"}}, false)
	if locked := s.Accept(AcceptedSubmission{Primary: domain.Trajectory{Class: "second"}}, false); locked {
		t.Fatalf("session should still be provisional after a second warned submission")
	}
	if got := s.Accepted(); got == nil || got.Primary.Class != "second" {
		t.Fatalf("latest provisional should win while no clean pass exists; got %+v", got)
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
