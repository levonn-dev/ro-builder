package catalog

import "testing"

func TestClassAttackSkills_TaekwonKidHasKicks(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	skills, ok := c.ClassAttackSkills("taekwon_kid")
	if !ok {
		t.Fatal("taekwon_kid not in catalog")
	}
	names := map[string]bool{}
	for _, s := range skills {
		names[s.AttackSkill.Name] = true
	}
	for _, want := range []string{"tornado_kick", "heel_drop", "roundhouse", "counter_kick", "flying_kick"} {
		if !names[want] {
			t.Errorf("missing attack skill %q (got %v)", want, names)
		}
	}
}

func TestClassAttackSkills_NonKickerHasNone(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	skills, ok := c.ClassAttackSkills("acolyte")
	if !ok {
		t.Fatal("acolyte not in catalog")
	}
	if len(skills) != 0 {
		t.Errorf("acolyte should have no attack skills, got %d", len(skills))
	}
}
