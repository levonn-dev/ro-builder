package scoring

import (
	"encoding/json"
	"testing"
)

func TestCombatResultsSkillsRoundTrip(t *testing.T) {
	ave := 1712.0
	hits := 8.0
	in := CombatResults{Skills: []SkillDamage{{
		Name: "sonic_blow", Damage: SkillDamageTriple{Ave: &ave}, Hits: &hits, Uncertainty: "approx",
	}}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out CombatResults
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "sonic_blow" || *out.Skills[0].Hits != 8 {
		t.Fatalf("round-trip mismatch: %+v", out.Skills)
	}
}

func TestScoreRequestAttackSkillsRoundTrip(t *testing.T) {
	in := ScoreRequest{AttackSkills: []AttackSkill{{Name: "tornado_kick", Level: 7, Primary: true}}}
	b, _ := json.Marshal(in)
	var out ScoreRequest
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.AttackSkills) != 1 || !out.AttackSkills[0].Primary || out.AttackSkills[0].Level != 7 {
		t.Fatalf("round-trip mismatch: %+v", out.AttackSkills)
	}
}
