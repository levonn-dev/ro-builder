package gates

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

func TestEffectiveStats_FromScore(t *testing.T) {
	in := Inputs{Score: &scoring.ScoreResponse{
		Derived: scoring.DerivedStats{TotalStats: domain.Stats{Dex: 108}},
	}}
	got, ok := effectiveStats(in)
	if !ok || got.Dex != 108 {
		t.Fatalf("effectiveStats = %+v, %v; want Dex 108, true", got, ok)
	}
}

func TestEffectiveStats_NoScore(t *testing.T) {
	if _, ok := effectiveStats(Inputs{}); ok {
		t.Fatal("effectiveStats should report false with no score")
	}
}
