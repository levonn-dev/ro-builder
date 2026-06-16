package catalog

import (
	"testing"
)

// Uses the embedded catalog.json (regenerated in Task 2 with TK's buffs).
func TestClassBuffs_Taekwon(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("taekwon_kid") // alias of "taekwon"
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for taekwon_kid")
	}
	want := map[string]bool{"mild_wind": false, "spurt": false, "taekwon_ranker": false}
	for _, b := range buffs {
		if b.SelfBuff == nil {
			t.Errorf("ClassBuffs returned a skill with nil SelfBuff: %s", b.AegisName)
			continue
		}
		want[b.SelfBuff.Name] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected buff %q in taekwon ClassBuffs, missing", name)
		}
	}
}

// TestMildWindOrder pins the element unlock order for Mild Wind (TK_SEVENWIND)
// read from the catalog. This is the SOURCE OF TRUTH for the unlock sequence;
// the sidecar's MILD_WIND_ORDER const in
// calc-sidecar/src/backends/rocalc/index.ts is a defense-in-depth mirror of
// exactly this list. If either diverges, the behavioral tests in
// calc-sidecar/test/backends/rocalc/buffs.test.ts (sidecar side) or this test
// (catalog side) will fail. Changing this order is a coordinated two-file edit.
func TestMildWindOrder(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("taekwon_kid")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for taekwon_kid")
	}
	var mildWind *ClassSkill
	for i := range buffs {
		if buffs[i].SelfBuff != nil && buffs[i].SelfBuff.Name == "mild_wind" {
			mildWind = &buffs[i]
			break
		}
	}
	if mildWind == nil {
		t.Fatal("mild_wind buff not found in taekwon_kid ClassBuffs")
	}
	if mildWind.SelfBuff.Endow == nil {
		t.Fatal("mild_wind SelfBuff.Endow is nil; expected an EndowSpec")
	}

	// Canonical order: index (1-based) == required Mild Wind level.
	// earth=1, wind=2, water=3, fire=4, ghost=5, shadow=6, holy=7.
	// Mirrors MILD_WIND_ORDER in calc-sidecar/src/backends/rocalc/index.ts.
	want := []string{"earth", "wind", "water", "fire", "ghost", "shadow", "holy"}
	got := mildWind.SelfBuff.Endow.Elements
	if len(got) != len(want) {
		t.Fatalf("mild_wind endow.elements: got %v (len %d), want %v (len %d)", got, len(got), want, len(want))
	}
	for i, el := range want {
		if got[i] != el {
			t.Errorf("endow.elements[%d]: got %q, want %q", i, got[i], el)
		}
	}
}

func TestClassBuffs_HighPriest(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("high_priest")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for high_priest")
	}
	got := map[string]string{} // name -> kind
	for _, b := range buffs {
		if b.SelfBuff == nil {
			t.Errorf("buff skill %s has nil SelfBuff", b.AegisName)
			continue
		}
		got[b.SelfBuff.Name] = b.SelfBuff.Kind
	}
	want := map[string]string{
		"blessing": "stat_buff", "increase_agi": "stat_buff", "impositio_manus": "stat_buff",
		"gloria": "stat_buff", "angelus": "stat_buff", "assumptio": "status",
		"suffragium": "stat_buff", "aspersio": "weapon_endow",
		"lex_aeterna": "debuff", "decrease_agi": "debuff", "signum_crucis": "debuff",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("buff %q: kind %q, want %q (present=%v)", name, got[name], kind, got[name] != "")
		}
	}
}

func TestClassBuffs_Professor(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("professor")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for professor")
	}
	got := map[string]string{} // name -> kind
	for _, b := range buffs {
		if b.SelfBuff == nil {
			continue
		}
		got[b.SelfBuff.Name] = b.SelfBuff.Kind
	}
	want := map[string]string{
		"flame_launcher": "weapon_endow", "frost_weapon": "weapon_endow",
		"lightning_loader": "weapon_endow", "seismic_weapon": "weapon_endow",
		"volcano": "land", "deluge": "land", "violent_gale": "land",
		"mind_breaker": "debuff", "spider_web": "debuff",
		"energy_coat": "status", "study": "stat_buff", "dragonology": "stat_buff",
		"foresight": "stat_buff", "double_casting": "stat_buff",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("buff %q: kind %q, want %q", name, got[name], kind)
		}
	}
	if len(want) != 14 {
		t.Fatalf("test expects 14 professor buffs, listed %d", len(want))
	}
}

func TestClassBuffs_Sage_ExcludesProfessorOnly(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("sage")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for sage")
	}
	have := map[string]bool{}
	for _, b := range buffs {
		if b.SelfBuff != nil {
			have[b.SelfBuff.Name] = true
		}
	}
	for _, name := range []string{"flame_launcher", "volcano", "energy_coat", "study", "dragonology"} {
		if !have[name] {
			t.Errorf("sage should have buff %q", name)
		}
	}
	for _, name := range []string{"mind_breaker", "spider_web", "foresight", "double_casting"} {
		if have[name] {
			t.Errorf("sage must NOT have Professor-only buff %q", name)
		}
	}
}

func TestClassBuffs_AssassinCross(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("assassin_cross")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for assassin_cross")
	}
	got := map[string]string{} // name -> kind
	for _, b := range buffs {
		if b.SelfBuff == nil {
			continue
		}
		got[b.SelfBuff.Name] = b.SelfBuff.Kind
	}
	want := map[string]string{
		"enchant_deadly_poison":  "stat_buff",
		"enchant_poison":         "weapon_endow",
		"advanced_katar_mastery": "stat_buff",
		"katar_mastery":          "stat_buff",
		"right_hand_mastery":     "stat_buff",
		"left_hand_mastery":      "stat_buff",
		"double_attack":          "stat_buff",
		"improve_dodge":          "stat_buff",
		"sonic_acceleration":     "status",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("buff %q: kind %q, want %q", name, got[name], kind)
		}
	}
	if len(want) != 9 {
		t.Fatalf("test expects 9 assassin_cross buffs, listed %d", len(want))
	}
}

func TestClassBuffs_Assassin_ExcludesTransOnly(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("assassin")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for assassin")
	}
	have := map[string]bool{}
	for _, b := range buffs {
		if b.SelfBuff != nil {
			have[b.SelfBuff.Name] = true
		}
	}
	// Base Assassin (and inherited Thief) skills.
	for _, name := range []string{
		"enchant_poison", "katar_mastery", "right_hand_mastery",
		"left_hand_mastery", "double_attack", "improve_dodge", "sonic_acceleration",
	} {
		if !have[name] {
			t.Errorf("assassin should have buff %q", name)
		}
	}
	// Trans-only (Assassin Cross) skills must NOT surface for base Assassin.
	for _, name := range []string{"enchant_deadly_poison", "advanced_katar_mastery"} {
		if have[name] {
			t.Errorf("assassin must NOT have trans-only buff %q", name)
		}
	}
}
