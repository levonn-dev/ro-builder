package scoring

import (
	"context"
	"testing"
	"time"
)

// TestBuffs_E2E proves that ScoreRequest.Buffs flows through the Go client into
// the calc sidecar over HTTP and produces buffed numbers. A Taekwon Kid
// scored WITH taekwon_ranker active must have >= 2.5x the maxHp of the same
// build scored WITHOUT it (ranker grants 3x HP; 2.5x is a safe floor).
//
// This is an integration test: it spawns a real calc sidecar subprocess and
// hits it over HTTP. It is skipped under -short.
func TestBuffs_E2E(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping in -short mode")
	}
	baseURL, stop := startSidecar(t)
	defer stop()

	c := NewClient(baseURL, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Level-99/50 Taekwon Kid with a realistic stat spread.
	class := "taekwon_kid"
	lvl := &Level{Base: 99, Job: 50}
	stats := &Stats{Str: 80, Agi: 90, Vit: 40, Int: 1, Dex: 60, Luk: 30}

	unbuffedReq := &ScoreRequest{
		Class: &class,
		Level: lvl,
		Stats: stats,
	}
	buffedReq := &ScoreRequest{
		Class: &class,
		Level: lvl,
		Stats: stats,
		Buffs: []Buff{{Name: "taekwon_ranker", Level: 1}},
	}

	unbuffed, err := c.Score(ctx, unbuffedReq)
	if err != nil {
		t.Fatalf("score (unbuffed): %v", err)
	}

	buffed, err := c.Score(ctx, buffedReq)
	if err != nil {
		t.Fatalf("score (buffed): %v", err)
	}

	t.Logf("maxHp unbuffed=%d  buffed=%d", unbuffed.Derived.MaxHP, buffed.Derived.MaxHP)

	// taekwon_ranker grants 3x HP; require >= 2.5x as a safe floor.
	floor := unbuffed.Derived.MaxHP * 5 / 2
	if buffed.Derived.MaxHP < floor {
		t.Errorf(
			"expected buffed maxHp >= %d (2.5x unbuffed %d), got %d",
			floor, unbuffed.Derived.MaxHP, buffed.Derived.MaxHP,
		)
	}
}
