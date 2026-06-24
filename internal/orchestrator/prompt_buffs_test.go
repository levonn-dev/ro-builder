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

func TestFormatUserPrompt_ProfessorLand(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, _ := cat.ClassBuffs("professor")
	out := formatUserPrompt(GenerateRequest{Class: "professor"}, nil, nil, buffs, 99, 50, 0)
	if !strings.Contains(out, "volcano") {
		t.Fatalf("prompt missing Scholar land buffs:\n%s", out)
	}
	// The prompt must include the land guidance sentence (not just the word
	// "land" appearing in a buff-kind field). The guidance sentence teaches
	// the LLM that kind=land buffs are ground effects that amplify all damage
	// of their element.
	const wantGuidance = "amplifies ALL damage"
	if !strings.Contains(out, wantGuidance) {
		t.Fatalf("prompt missing land guidance (want %q):\n%s", wantGuidance, out)
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

func TestFormatUserPrompt_SuperNoviceInnate(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, _ := cat.ClassBuffs("super_novice")
	innate, _ := cat.ClassInnateBuffs("super_novice")
	merged := mergeInnateBuffsForPrompt(buffs, innate) // helper added in this task
	out := formatUserPrompt(GenerateRequest{Class: "super_novice"}, nil, nil, merged, 99, 99, 0)
	if !strings.Contains(out, "no_death_bonus") {
		t.Fatalf("prompt missing no_death_bonus:\n%s", out)
	}
	if !strings.Contains(out, "(innate)") {
		t.Fatalf("prompt missing innate anchor marker:\n%s", out)
	}
	// The innate-buff guidance sentence must be present so the model knows the
	// buff is exempt from the "anchor allocated" rule.
	if !strings.Contains(out, "exempt") {
		t.Fatalf("prompt missing innate-buff guidance:\n%s", out)
	}
}
