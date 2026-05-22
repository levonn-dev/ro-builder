package catalog

import "testing"

// All catalog tests use the embedded data; no Hercules clone needed at
// runtime. The data was baked in at compile time by `task catalog`
// (cmd/build-catalog).

func TestLoad_DataIsPresent(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.ItemCount() < 1000 {
		t.Errorf("items: got %d, want > 1000", c.ItemCount())
	}
	if c.MobCount() < 100 {
		t.Errorf("mobs: got %d, want > 100", c.MobCount())
	}
	if c.SkillCount() < 100 {
		t.Errorf("skills: got %d, want > 100", c.SkillCount())
	}
}

func TestSkill_KnownEntries(t *testing.T) {
	c, _ := Load()
	swordMastery, ok := c.Skill(2)
	if !ok {
		t.Fatal("Sword Mastery (2) not found")
	}
	if swordMastery.AegisName != "SM_SWORD" {
		t.Errorf("aegis: got %q want SM_SWORD", swordMastery.AegisName)
	}
	if swordMastery.Name != "Sword Mastery" {
		t.Errorf("name: got %q", swordMastery.Name)
	}
	if swordMastery.MaxLevel != 10 {
		t.Errorf("max_level: got %d want 10", swordMastery.MaxLevel)
	}
}

func TestSkill_Missing(t *testing.T) {
	c, _ := Load()
	if _, ok := c.Skill(99999999); ok {
		t.Error("expected miss for impossible id")
	}
}

func TestItem_KnownEntries(t *testing.T) {
	c, _ := Load()
	cases := []struct {
		id   int
		name string
		typ  string
		atk  int
	}{
		{id: 1201, name: "Knife", typ: "IT_WEAPON", atk: 17},
		{id: 2301, name: "Cotton_Shirt", typ: "IT_ARMOR"},
	}
	for _, tc := range cases {
		it, ok := c.Item(tc.id)
		if !ok {
			t.Errorf("item %d not found", tc.id)
			continue
		}
		if it.Type != tc.typ {
			t.Errorf("item %d type: got %q want %q", tc.id, it.Type, tc.typ)
		}
		if tc.atk != 0 && it.Atk != tc.atk {
			t.Errorf("item %d atk: got %d want %d", tc.id, it.Atk, tc.atk)
		}
	}
}

func TestItem_Missing(t *testing.T) {
	c, _ := Load()
	if _, ok := c.Item(99999999); ok {
		t.Error("expected miss for impossible id")
	}
}

func TestMob_KnownEntries(t *testing.T) {
	c, _ := Load()
	poring, ok := c.Mob(1002)
	if !ok {
		t.Fatal("Poring (1002) not found")
	}
	if poring.Name != "Poring" {
		t.Errorf("name: got %q", poring.Name)
	}
	if poring.Hp != 50 {
		t.Errorf("hp: got %d want 50", poring.Hp)
	}
}

func TestSearchItems_ByType(t *testing.T) {
	c, _ := Load()
	weapons := c.SearchItems(ItemFilter{Type: "IT_WEAPON", Limit: 50})
	if len(weapons) < 10 {
		t.Errorf("expected >10 weapons, got %d", len(weapons))
	}
	for _, it := range weapons {
		if it.Type != "IT_WEAPON" {
			t.Errorf("non-weapon in result: id=%d type=%q", it.ID, it.Type)
		}
	}
}

func TestSearchItems_ByNameContains(t *testing.T) {
	c, _ := Load()
	knives := c.SearchItems(ItemFilter{Type: "IT_WEAPON", NameContains: "Knife"})
	if len(knives) == 0 {
		t.Fatal("no Knife matches")
	}
	found := false
	for _, it := range knives {
		if it.ID == 1201 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("basic Knife (1201) missing from 'Knife' substring search")
	}
}

func TestSearchItems_LimitDefaults(t *testing.T) {
	c, _ := Load()
	weapons := c.SearchItems(ItemFilter{Type: "IT_WEAPON"})
	if len(weapons) != DefaultSearchLimit {
		t.Errorf("default limit: got %d want %d", len(weapons), DefaultSearchLimit)
	}
	huge := c.SearchItems(ItemFilter{Type: "IT_WEAPON", Limit: 9999})
	if len(huge) > MaxSearchLimit {
		t.Errorf("hard cap: got %d, want <= %d", len(huge), MaxSearchLimit)
	}
}

func TestSearchItems_Slots(t *testing.T) {
	c, _ := Load()
	slotted := c.SearchItems(ItemFilter{Type: "IT_WEAPON", MinSlots: 3, Limit: 50})
	for _, it := range slotted {
		if it.Slots < 3 {
			t.Errorf("MinSlots filter failed: %s has %d slots", it.Name, it.Slots)
		}
	}
}

// TestClassMaxLevels_ParsedFromHercules verifies the parsed
// job_db.conf + exp_group_db.conf data flows through to runtime. Spot-
// checks a handful of classes whose caps are well-known in the pre-re
// community; a regression here means cmd/build-catalog isn't writing
// JobMaxLevels or Load isn't indexing it under the right key.
func TestClassMaxLevels_ParsedFromHercules(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cases := []struct {
		class    string
		wantBase int
		wantJob  int
	}{
		{"novice", 99, 10},
		{"novice_high", 99, 10},
		{"super_novice", 99, 99},
		{"swordsman", 99, 50},
		{"thief", 99, 50},
		{"thief_high", 99, 50},
		{"assassin", 99, 50},
		{"assassin_cross", 99, 70},
		{"lord_knight", 99, 70},
		{"high_wizard", 99, 70},
		{"taekwon", 99, 50},
		{"taekwon_kid", 99, 50}, // alias of taekwon
	}
	for _, tc := range cases {
		t.Run(tc.class, func(t *testing.T) {
			base, job, ok := c.ClassMaxLevels(tc.class)
			if !ok {
				t.Fatalf("%s: not in catalog max-levels (catalog v%d may be stale)", tc.class, c.Version())
			}
			if base != tc.wantBase || job != tc.wantJob {
				t.Errorf("%s: got (%d, %d), want (%d, %d)", tc.class, base, job, tc.wantBase, tc.wantJob)
			}
		})
	}
}

// TestClassMaxLevels_UnknownReturnsFalse confirms the boundary behavior
// matches the documented contract: empty input and class-not-in-catalog
// both return ok=false rather than guessing a default.
func TestClassMaxLevels_UnknownReturnsFalse(t *testing.T) {
	c, _ := Load()
	for _, in := range []string{"", "not_a_real_class"} {
		base, job, ok := c.ClassMaxLevels(in)
		if ok || base != 0 || job != 0 {
			t.Errorf("%q: got (%d, %d, %v), want (0, 0, false)", in, base, job, ok)
		}
	}
}

// TestClassSkillPointBudget locks down the trans-rebirth-respecting
// progression rule. Numbers verified against pre-renewal job mechanics:
// every job grants (maxJob − 1) skill points; transcending resets and
// the trans chain restarts at high_novice. The catalog's `Inherit` chain
// (skill-tree inheritance) is NOT a valid input for the budget; trans
// 2nd-jobs must traverse the post-rebirth path explicitly.
func TestClassSkillPointBudget(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cases := []struct {
		class string
		want  int
		note  string
	}{
		// Pre-existing prompt assertion (orchestrator_test.go locks 58 in).
		{class: "taekwon_kid", want: 58, note: "novice 9 + taekwon 49"},
		{class: "taekwon", want: 58, note: "direct (taekwon_kid alias target)"},

		// Non-trans 2nd jobs: 9 (novice) + 49 (1st) + 49 (2nd) = 107.
		{class: "assassin", want: 107, note: "novice + thief + assassin"},
		{class: "knight", want: 107, note: "novice + swordsman + knight"},
		{class: "wizard", want: 107, note: "novice + magician + wizard"},
		{class: "hunter", want: 107, note: "novice + archer + hunter"},
		{class: "priest", want: 107, note: "novice + acolyte + priest"},
		{class: "blacksmith", want: 107, note: "novice + merchant + blacksmith"},

		// Trans 2nd jobs: 9 (high_novice) + 49 (trans 1st) + 69 (trans 2nd) = 127.
		{class: "assassin_cross", want: 127, note: "high_novice + thief_high + assassin_cross"},
		{class: "lord_knight", want: 127, note: "high_novice + swordsman_high + lord_knight"},
		{class: "high_wizard", want: 127, note: "high_novice + magician_high + high_wizard"},
		{class: "sniper", want: 127, note: "high_novice + archer_high + sniper"},
		{class: "high_priest", want: 127, note: "high_novice + acolyte_high + high_priest"},
		{class: "whitesmith", want: 127, note: "high_novice + merchant_high + whitesmith"},
		{class: "paladin", want: 127, note: "high_novice + swordsman_high + paladin"},
		{class: "champion", want: 127, note: "high_novice + acolyte_high + champion"},

		// Trans 1st-jobs in isolation: 9 + 49 = 58 (no 2nd-job tier yet).
		{class: "thief_high", want: 58, note: "high_novice + thief_high"},
		{class: "swordsman_high", want: 58, note: "high_novice + swordsman_high"},

		// 1st jobs in isolation: 9 + 49 = 58.
		{class: "thief", want: 58, note: "novice + thief"},
		{class: "swordsman", want: 58, note: "novice + swordsman"},

		// Novice alone: 9.
		{class: "novice", want: 9, note: "novice"},
		{class: "novice_high", want: 9, note: "novice_high"},
	}
	for _, tc := range cases {
		t.Run(tc.class, func(t *testing.T) {
			got, ok := c.ClassSkillPointBudget(tc.class)
			if !ok {
				t.Fatalf("%s: not found in catalog", tc.class)
			}
			if got != tc.want {
				t.Errorf("%s: got %d, want %d (%s)", tc.class, got, tc.want, tc.note)
			}
		})
	}
}

// TestClassSkillPointBudget_TransPathSkipsNonTransLineage is the
// non-regression check for the trans-rebirth fix. Walking the catalog's
// Inherit chain for assassin_cross would yield
// novice + thief + assassin + assassin_cross = 9+49+49+69 = 176; the
// correct trans path is novice_high + thief_high + assassin_cross =
// 9+49+69 = 127. Lock 127 in so future refactors can't quietly fall
// back to the wrong chain.
func TestClassSkillPointBudget_TransPathSkipsNonTransLineage(t *testing.T) {
	c, _ := Load()
	got, _ := c.ClassSkillPointBudget("assassin_cross")
	if got == 176 {
		t.Fatalf("assassin_cross budget is 176; the inherit-walk regressed; got the pre-rebirth lineage (novice + thief + assassin + assassin_cross) instead of the trans path (high_novice + thief_high + assassin_cross)")
	}
	if got != 127 {
		t.Errorf("assassin_cross budget: got %d, want 127 (high_novice 9 + thief_high 49 + assassin_cross 69)", got)
	}
}

// TestClassSkillPointBudget_Unknown returns (0, false) for inputs that
// aren't in the catalog at all.
func TestClassSkillPointBudget_Unknown(t *testing.T) {
	c, _ := Load()
	got, ok := c.ClassSkillPointBudget("not_a_real_class")
	if ok || got != 0 {
		t.Errorf("not_a_real_class: got (%d, %v), want (0, false)", got, ok)
	}
}
