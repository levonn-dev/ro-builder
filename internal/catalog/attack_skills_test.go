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

func TestClassAttackSkills_OnboardedClasses(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	// Representative skills per onboarded class group, including skills reached
	// through job inheritance (Champion's asura_strike from Monk, Lord Knight's
	// bowling_bash from Knight, Sniper's double_strafe from Hunter, High
	// Wizard's storm_gust from Wizard) and the full-coverage additions (the
	// Wizard nuke set, Hunter traps, Charge Attack, Gravitation Field).
	cases := map[string][]string{
		"assassin_cross": {"sonic_blow", "venom_splasher", "soul_breaker", "envenom"},
		"knight":         {"bowling_bash", "charge_attack", "brandish_spear"},
		"lord_knight":    {"spiral_pierce", "head_crush", "joint_beat", "bowling_bash"},
		"hunter":         {"blitz_beat", "land_mine", "blast_mine", "claymore_trap"},
		"sniper":         {"double_strafe", "sharp_shooting", "falcon_assault"},
		"wizard":         {"lightning_bolt", "storm_gust", "fire_ball", "meteor_storm"},
		"high_wizard":    {"storm_gust", "gravitation_field", "napalm_vulcan"},
		"monk":           {"asura_strike", "investigate", "chain_combo", "holy_light", "finger_offensive", "excruciating_palm"},
		"champion":       {"asura_strike", "chain_crush_combo", "palm_push_strike", "finger_offensive", "excruciating_palm"},
	}
	for class, wants := range cases {
		skills, ok := c.ClassAttackSkills(class)
		if !ok {
			t.Errorf("%s not in catalog", class)
			continue
		}
		have := make(map[string]bool, len(skills))
		for _, s := range skills {
			have[s.AttackSkill.Name] = true
		}
		for _, want := range wants {
			if !have[want] {
				t.Errorf("class %q: missing attack skill %q", class, want)
			}
		}
	}
}

func TestClassAttackSkills_NonKickerHasNone(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	// The Merchant economy line has in-game damage skills (Mammonite, Cart
	// Termination) but the overlay tags none of them, so the scoreable filter
	// returns empty. (Acolyte is no longer a valid "none" case: comprehensive
	// onboarding tags its Holy Light.)
	skills, ok := c.ClassAttackSkills("merchant")
	if !ok {
		t.Fatal("merchant not in catalog")
	}
	if len(skills) != 0 {
		t.Errorf("merchant should have no attack skills, got %d", len(skills))
	}
}
