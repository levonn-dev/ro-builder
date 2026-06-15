package orchestrator

import (
	"strings"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/catalog"
)

func TestFormatUserPrompt_HighPriestDebuffs(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, _ := cat.ClassBuffs("high_priest")
	out := formatUserPrompt(GenerateRequest{Class: "high_priest"}, nil, nil, buffs, 99, 70, 0)
	if !strings.Contains(out, "lex_aeterna") {
		t.Fatalf("prompt missing HP debuffs:\n%s", out)
	}
	// The prompt must include the debuff guidance sentence (not just the word
	// "debuff" appearing in a buff-kind field). The guidance sentence teaches
	// the LLM that kind=debuff buffs are cast on the target.
	const wantGuidance = "cast on the target"
	if !strings.Contains(out, wantGuidance) {
		t.Fatalf("prompt missing debuff guidance (want %q):\n%s", wantGuidance, out)
	}
}

func TestFormatUserPrompt_IncludesBuffs(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, _ := cat.ClassBuffs("taekwon_kid")
	if len(buffs) == 0 {
		t.Fatal("no taekwon buffs in catalog; Task 2 not applied?")
	}
	out := formatUserPrompt(GenerateRequest{Class: "taekwon_kid"}, nil, nil, buffs, 99, 50, 0)
	if !strings.Contains(out, "mild_wind") || !strings.Contains(strings.ToLower(out), "self-buff") {
		t.Fatalf("prompt missing buffs block:\n%s", out)
	}

	var mildLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "mild_wind") {
			mildLine = line
			break
		}
	}
	if mildLine == "" {
		t.Fatal("no mild_wind line found in prompt")
	}
	for _, want := range []string{"anchor", "weapon_endow", "holy"} {
		if !strings.Contains(mildLine, want) {
			t.Errorf("mild_wind line missing %q: %s", want, mildLine)
		}
	}
}
