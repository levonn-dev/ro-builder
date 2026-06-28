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
	// bowling_bash from Knight, Paladin's holy_cross from Crusader, the kicks
	// Soul Linker / Star Gladiator inherit from Taekwon) and cascade coverage
	// (Sage/Professor get the Mage bolts, Super Novice gets Mammonite + bolts).
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
		"crusader":       {"holy_cross", "grand_cross", "shield_charge", "shield_boomerang"},
		"paladin":        {"pressure", "shield_chain", "martyrs_reckoning", "holy_cross"},
		"blacksmith":     {"mammonite", "cart_revolution"},
		"whitesmith":     {"cart_termination", "mammonite"},
		"alchemist":      {"acid_terror", "demonstration"},
		"creator":        {"acid_demonstration", "acid_terror"},
		"bard":           {"musical_strike", "double_strafe"},
		"clown":          {"arrow_vulcan", "musical_strike"},
		"dancer":         {"slinging_arrow", "double_strafe"},
		"gypsy":          {"arrow_vulcan", "slinging_arrow"},
		"rogue":          {"back_stab", "raid"},
		"stalker":        {"back_stab", "raid"},
		"gunslinger":     {"desperado", "full_buster", "tracking", "ground_drift"},
		"ninja":          {"throw_kunai", "crimson_fire_petal", "final_strike", "kamaitachi"},
		"soul_linker":    {"esma", "estin", "estun"},
		"super_novice":   {"mammonite", "bash", "fire_bolt"},
		"priest":         {"turn_undead", "magnus_exorcismus", "holy_light"},
		"high_priest":    {"turn_undead", "magnus_exorcismus"},
		"sage":           {"fire_bolt", "earth_spike"},
		"professor":      {"fire_bolt", "heavens_drive"},
		"star_gladiator": {"tornado_kick", "flying_kick", "sun_warmth", "moon_warmth", "star_warmth"},
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

func TestClassAttackSkills_SoulLinkerKicksAllocatableNotScoreable(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	kickAegis := []string{"TK_STORMKICK", "TK_DOWNKICK", "TK_TURNKICK", "TK_COUNTER", "TK_JUMPKICK"}

	// The kicks STAY in the tree: a Soul Linker spent Taekwon job points on them,
	// so they must count toward the skill-point budget and carry forward (skill
	// monotonicity). They're flagged Unusable, not dropped.
	tree, ok := c.ClassSkills("soul_linker")
	if !ok {
		t.Fatal("soul_linker not in catalog")
	}
	inTree := make(map[string]ClassSkill, len(tree))
	for _, s := range tree {
		inTree[s.AegisName] = s
	}
	for _, k := range kickAegis {
		s, present := inTree[k]
		if !present {
			t.Errorf("soul_linker tree should still contain %s (points spent as Taekwon)", k)
			continue
		}
		if !s.Unusable {
			t.Errorf("soul_linker %s should be flagged Unusable", k)
		}
	}

	// ...but they are NOT scoreable as Soul Linker attacks (only the utility TK
	// skills carry over usable). Star Gladiator, the fighter branch, keeps them
	// usable -- covered by TestClassAttackSkills_OnboardedClasses.
	atk, ok := c.ClassAttackSkills("soul_linker")
	if !ok {
		t.Fatal("soul_linker not in catalog")
	}
	haveAtk := make(map[string]bool, len(atk))
	for _, s := range atk {
		haveAtk[s.AttackSkill.Name] = true
	}
	for _, kick := range []string{"tornado_kick", "heel_drop", "roundhouse", "counter_kick", "flying_kick"} {
		if haveAtk[kick] {
			t.Errorf("soul_linker should not have scoreable kick %q", kick)
		}
	}
	for _, want := range []string{"esma", "estin", "estun"} {
		if !haveAtk[want] {
			t.Errorf("soul_linker missing scoreable skill %q", want)
		}
	}
}

func TestClassAttackSkills_NonKickerHasNone(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	// Novice has no damage skills at all, so the scoreable filter returns empty.
	// (Merchant is no longer a valid "none" case: comprehensive onboarding tags
	// its Mammonite and Cart Revolution, which cascade from the Blacksmith rows.)
	skills, ok := c.ClassAttackSkills("novice")
	if !ok {
		t.Fatal("novice not in catalog")
	}
	if len(skills) != 0 {
		t.Errorf("novice should have no attack skills, got %d", len(skills))
	}
}
