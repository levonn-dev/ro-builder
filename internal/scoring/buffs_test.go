package scoring

import (
	"encoding/json"
	"testing"
)

func TestScoreRequest_BuffsRoundTrip(t *testing.T) {
	req := ScoreRequest{
		Buffs: []Buff{
			{Name: "taekwon_ranker", Level: 1},
			{Name: "mild_wind", Level: 7, Element: "holy"},
		},
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); !json.Valid(b) || got == "" {
		t.Fatalf("marshal produced invalid json: %s", got)
	}
	var back ScoreRequest
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Buffs) != 2 || back.Buffs[1].Element != "holy" || back.Buffs[0].Level != 1 {
		t.Fatalf("round-trip mismatch: %+v", back.Buffs)
	}
}
