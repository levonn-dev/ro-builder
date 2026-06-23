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

func TestClassBuffs_Sniper(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("sniper")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for sniper")
	}
	got := map[string]string{} // name -> kind
	for _, b := range buffs {
		if b.SelfBuff == nil {
			continue
		}
		got[b.SelfBuff.Name] = b.SelfBuff.Kind
	}
	want := map[string]string{
		"owls_eye":              "stat_buff",
		"vultures_eye":          "stat_buff",
		"improve_concentration": "stat_buff",
		"beast_bane":            "stat_buff",
		"steel_crow":            "status",
		"true_sight":            "stat_buff",
		"wind_walk":             "stat_buff",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("buff %q: kind %q, want %q", name, got[name], kind)
		}
	}
	if len(want) != 7 {
		t.Fatalf("test expects 7 sniper buffs, listed %d", len(want))
	}
}

func TestClassBuffs_Hunter_ExcludesTransOnly(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("hunter")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for hunter")
	}
	have := map[string]bool{}
	for _, b := range buffs {
		if b.SelfBuff != nil {
			have[b.SelfBuff.Name] = true
		}
	}
	// Archer/Hunter skills available to base Hunter.
	for _, name := range []string{
		"owls_eye", "vultures_eye", "improve_concentration", "beast_bane", "steel_crow",
	} {
		if !have[name] {
			t.Errorf("hunter should have buff %q", name)
		}
	}
	// Trans-only (Sniper) skills must NOT surface for base Hunter.
	for _, name := range []string{"true_sight", "wind_walk"} {
		if have[name] {
			t.Errorf("hunter must NOT have trans-only buff %q", name)
		}
	}
}

func TestClassBuffs_Whitesmith(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("whitesmith")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for whitesmith")
	}
	got := map[string]string{} // name -> kind
	for _, b := range buffs {
		if b.SelfBuff == nil {
			continue
		}
		got[b.SelfBuff.Name] = b.SelfBuff.Kind
	}
	want := map[string]string{
		"over_thrust":              "stat_buff",
		"maximum_over_thrust":      "stat_buff",
		"maximize_power":           "stat_buff",
		"adrenaline_rush":          "stat_buff",
		"advanced_adrenaline_rush": "stat_buff",
		"weaponry_research":        "stat_buff",
		"crazy_uproar":             "stat_buff",
		"hilt_binding":             "stat_buff",
		"skin_tempering":           "status",
		"weapon_perfection":        "stat_buff",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("buff %q: kind %q, want %q", name, got[name], kind)
		}
	}
	if len(want) != 10 {
		t.Fatalf("test expects 10 whitesmith buffs, listed %d", len(want))
	}
}

func TestClassBuffs_Blacksmith_ExcludesTransOnly(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("blacksmith")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for blacksmith")
	}
	have := map[string]bool{}
	for _, b := range buffs {
		if b.SelfBuff != nil {
			have[b.SelfBuff.Name] = true
		}
	}
	// Merchant/Blacksmith skills available to base Blacksmith (9 of 10).
	for _, name := range []string{
		"over_thrust", "maximize_power", "adrenaline_rush", "advanced_adrenaline_rush",
		"weaponry_research", "crazy_uproar", "hilt_binding", "skin_tempering", "weapon_perfection",
	} {
		if !have[name] {
			t.Errorf("blacksmith should have buff %q", name)
		}
	}
	// Trans-only (Whitesmith) skill must NOT surface for base Blacksmith.
	for _, name := range []string{"maximum_over_thrust"} {
		if have[name] {
			t.Errorf("blacksmith must NOT have trans-only buff %q", name)
		}
	}
}

func TestClassBuffs_LordKnight(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("lord_knight")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for lord_knight")
	}
	got := map[string]string{} // name -> kind
	for _, b := range buffs {
		if b.SelfBuff == nil {
			continue
		}
		got[b.SelfBuff.Name] = b.SelfBuff.Kind
	}
	want := map[string]string{
		"sword_mastery":            "stat_buff",
		"two_handed_sword_mastery": "stat_buff",
		"spear_mastery":            "stat_buff",
		"twohand_quicken":          "stat_buff",
		"onehand_quicken":          "stat_buff",
		"aura_blade":               "stat_buff",
		"concentration":            "stat_buff",
		"berserk":                  "stat_buff",
		"auto_berserk":             "stat_buff",
		"endure":                   "status",
		"increase_hp_recovery":     "status",
		"cavalier_mastery":         "status",
		"parrying":                 "status",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("buff %q: kind %q, want %q", name, got[name], kind)
		}
	}
	if len(want) != 13 {
		t.Fatalf("test expects 13 lord_knight buffs, listed %d", len(want))
	}
}

func TestClassBuffs_Knight_ExcludesTransOnly(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("knight")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for knight")
	}
	have := map[string]bool{}
	for _, b := range buffs {
		if b.SelfBuff != nil {
			have[b.SelfBuff.Name] = true
		}
	}
	// Swordman/Knight skills available to base Knight (9 of 13).
	for _, name := range []string{
		"sword_mastery", "two_handed_sword_mastery", "spear_mastery", "twohand_quicken",
		"onehand_quicken", "endure", "increase_hp_recovery", "auto_berserk", "cavalier_mastery",
	} {
		if !have[name] {
			t.Errorf("knight should have buff %q", name)
		}
	}
	// Trans-only (Lord Knight) skills must NOT surface for base Knight.
	for _, name := range []string{"aura_blade", "concentration", "berserk", "parrying"} {
		if have[name] {
			t.Errorf("knight must NOT have trans-only buff %q", name)
		}
	}
}

func TestClassBuffs_Champion(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("champion")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for champion")
	}
	got := map[string]string{} // name -> kind
	for _, b := range buffs {
		if b.SelfBuff == nil {
			continue
		}
		got[b.SelfBuff.Name] = b.SelfBuff.Kind
	}
	want := map[string]string{
		"demon_bane":        "stat_buff",
		"iron_fists":        "stat_buff",
		"triple_attack":     "stat_buff",
		"dodge":             "stat_buff",
		"fury":              "stat_buff",
		"spiritual_cadence": "status",
		"divine_protection": "status",
		"mental_strength":   "status",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("buff %q: kind %q, want %q", name, got[name], kind)
		}
	}
	if len(want) != 8 {
		t.Fatalf("test expects 8 champion buffs, listed %d", len(want))
	}
}

// Monk and Champion share an identical rocalc job-buff bank, so Monk surfaces the
// same 8 buffs (no trans-only gating). Pins that these buffs are not Champion-gated.
func TestClassBuffs_Monk(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("monk")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for monk")
	}
	have := map[string]bool{}
	for _, b := range buffs {
		if b.SelfBuff != nil {
			have[b.SelfBuff.Name] = true
		}
	}
	for _, name := range []string{
		"demon_bane", "iron_fists", "triple_attack", "dodge", "fury",
		"spiritual_cadence", "divine_protection", "mental_strength",
	} {
		if !have[name] {
			t.Errorf("monk should have buff %q (shares Champion's bank)", name)
		}
	}
}

func TestClassBuffs_Paladin(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("paladin")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for paladin")
	}
	got := map[string]string{} // name -> kind
	for _, b := range buffs {
		if b.SelfBuff == nil {
			continue
		}
		got[b.SelfBuff.Name] = b.SelfBuff.Kind
	}
	want := map[string]string{
		"sword_mastery":            "stat_buff",
		"two_handed_sword_mastery": "stat_buff",
		"spear_mastery":            "stat_buff",
		"auto_berserk":             "stat_buff",
		"demon_bane":               "stat_buff",
		"faith":                    "stat_buff",
		"spear_quicken":            "stat_buff",
		"divine_protection":        "status",
		"cavalier_mastery":         "status",
		"endure":                   "status",
		"increase_hp_recovery":     "status",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("buff %q: kind %q, want %q", name, got[name], kind)
		}
	}
	// Regression lock on the inherited blast radius: Paladin's bank is exactly
	// these 11 (9 inherited from Lord Knight / Champion + Faith + Spear Quicken).
	// A future onboarding that lights up another Swordman/Acolyte skill for
	// Paladin must update this number deliberately.
	if len(buffs) != 11 {
		t.Fatalf("ClassBuffs(paladin) returned %d buffs, want 11", len(buffs))
	}
}

// Crusader and Paladin share an identical rocalc job-buff bank, so Crusader
// surfaces the same 11 buffs (no trans-only gating). Pins that the two new CR_
// buffs are not Paladin-gated.
func TestClassBuffs_Crusader(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("crusader")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for crusader")
	}
	have := map[string]bool{}
	for _, b := range buffs {
		if b.SelfBuff != nil {
			have[b.SelfBuff.Name] = true
		}
	}
	for _, name := range []string{
		"sword_mastery", "two_handed_sword_mastery", "spear_mastery",
		"auto_berserk", "demon_bane", "faith", "spear_quicken",
		"divine_protection", "cavalier_mastery", "endure", "increase_hp_recovery",
	} {
		if !have[name] {
			t.Errorf("crusader should have buff %q (shares Paladin's bank)", name)
		}
	}
}

func TestClassBuffs_Stalker(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("stalker")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for stalker")
	}
	got := map[string]string{} // name -> kind
	for _, b := range buffs {
		if b.SelfBuff == nil {
			continue
		}
		got[b.SelfBuff.Name] = b.SelfBuff.Kind
	}
	want := map[string]string{
		"double_attack":    "stat_buff",
		"improve_dodge":    "stat_buff",
		"sword_mastery":    "stat_buff",
		"vultures_eye":     "stat_buff",
		"close_confine":    "stat_buff",
		"stealth":          "stat_buff",
		"counter_instinct": "status",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("buff %q: kind %q, want %q", name, got[name], kind)
		}
	}
	// Regression lock: Stalker's bank is exactly these 7 (4 inherited from Assassin
	// Cross / Lord Knight / Sniper + Close Confine + Stealth + Counter Instinct). A
	// future onboarding that lights up another Thief/Swordman/Archer skill for
	// Stalker must update this number deliberately.
	if len(buffs) != 7 {
		t.Fatalf("ClassBuffs(stalker) returned %d buffs, want 7", len(buffs))
	}
}

func TestClassBuffs_Gunslinger(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("gunslinger")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for gunslinger")
	}
	got := map[string]string{} // name -> kind
	for _, b := range buffs {
		if b.SelfBuff == nil {
			continue
		}
		got[b.SelfBuff.Name] = b.SelfBuff.Kind
	}
	want := map[string]string{
		"single_action":       "stat_buff",
		"snake_eye":           "stat_buff",
		"increasing_accuracy": "stat_buff",
		"madness_canceller":   "stat_buff",
		"adjustment":          "stat_buff",
		"chain_action":        "stat_buff",
		"gatling_fever":       "stat_buff",
		"flip_the_coin":       "stat_buff",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("buff %q: kind %q, want %q", name, got[name], kind)
		}
	}
	// Regression lock: Gunslinger's bank is exactly these 8 (m_JobBuff[45] minus 537
	// ALL_INCCARRY). It is an Expanded class with no trans split and no inherited
	// buffs, so this set must not change unless a GS skill is added or removed
	// deliberately.
	if len(buffs) != 8 {
		t.Fatalf("ClassBuffs(gunslinger) returned %d buffs, want 8", len(buffs))
	}
}

func TestClassBuffs_Clown(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("clown")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for clown")
	}
	got := map[string]string{}
	for _, b := range buffs {
		if b.SelfBuff != nil {
			got[b.SelfBuff.Name] = b.SelfBuff.Kind
		}
	}
	// 3 inherited from the Archer line (Sniper) + the Bard performer passive.
	want := map[string]string{
		"owls_eye":              "stat_buff",
		"vultures_eye":          "stat_buff",
		"improve_concentration": "stat_buff",
		"musical_lesson":        "stat_buff",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("buff %q: kind %q, want %q", name, got[name], kind)
		}
	}
	if len(buffs) != 4 {
		t.Fatalf("ClassBuffs(clown) returned %d buffs, want 4", len(buffs))
	}
}

func TestClassBuffs_Gypsy(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("gypsy")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for gypsy")
	}
	got := map[string]string{}
	for _, b := range buffs {
		if b.SelfBuff != nil {
			got[b.SelfBuff.Name] = b.SelfBuff.Kind
		}
	}
	want := map[string]string{
		"owls_eye":              "stat_buff",
		"vultures_eye":          "stat_buff",
		"improve_concentration": "stat_buff",
		"dancing_lesson":        "stat_buff",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("buff %q: kind %q, want %q", name, got[name], kind)
		}
	}
	if len(buffs) != 4 {
		t.Fatalf("ClassBuffs(gypsy) returned %d buffs, want 4", len(buffs))
	}
}

// Cross-class gating: the instrument passive is Bard-line only; the whip
// passive is Dancer-line only. They must not bleed across.
func TestClassBuffs_MusicPassiveGating(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	has := func(class, buff string) bool {
		buffs, _ := c.ClassBuffs(class)
		for _, b := range buffs {
			if b.SelfBuff != nil && b.SelfBuff.Name == buff {
				return true
			}
		}
		return false
	}
	if has("gypsy", "musical_lesson") {
		t.Error("musical_lesson must not surface for gypsy")
	}
	if has("clown", "dancing_lesson") {
		t.Error("dancing_lesson must not surface for clown")
	}
}

func TestClassBuffs_StarGladiator(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("star_gladiator")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for star_gladiator")
	}
	got := map[string]string{} // name -> kind
	for _, b := range buffs {
		if b.SelfBuff == nil {
			continue
		}
		got[b.SelfBuff.Name] = b.SelfBuff.Kind
	}
	want := map[string]string{
		// 3 inherited from the Taekwon line
		"taekwon_ranker": "status",
		"spurt":          "stat_buff",
		"mild_wind":      "weapon_endow",
		// 7 scored SG buffs
		"sls_solar_wrath":        "stat_buff",
		"sls_lunar_wrath":        "stat_buff",
		"sls_stellar_wrath":      "stat_buff",
		"sls_solar_protection":   "stat_buff",
		"sls_lunar_protection":   "stat_buff",
		"sls_stellar_protection": "stat_buff",
		"sls_union":              "stat_buff",
		// 3 wired-but-inert SG buffs
		"sls_demon":     "status",
		"sls_knowledge": "status",
		"sls_blessing":  "status",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("buff %q: kind %q, want %q", name, got[name], kind)
		}
	}
	// Regression lock: 3 inherited TK buffs + 10 SG buffs. SG_ skills are
	// Star Gladiator-exclusive in pre-renewal, so this set must not change
	// unless an SG buff is added or removed deliberately.
	if len(buffs) != 13 {
		t.Fatalf("ClassBuffs(star_gladiator) returned %d buffs, want 13", len(buffs))
	}
}

// Stealth (Chase Walk) and Counter Instinct (Reject Sword) are Stalker trans-only;
// base Rogue gets only Close Confine on top of the 4 inherited buffs.
func TestClassBuffs_Rogue_ExcludesTransOnly(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("rogue")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for rogue")
	}
	have := map[string]bool{}
	for _, b := range buffs {
		if b.SelfBuff != nil {
			have[b.SelfBuff.Name] = true
		}
	}
	// Inherited + base-Rogue skills available to Rogue.
	for _, name := range []string{
		"double_attack", "improve_dodge", "sword_mastery", "vultures_eye", "close_confine",
	} {
		if !have[name] {
			t.Errorf("rogue should have buff %q", name)
		}
	}
	// Trans-only (Stalker) skills must NOT surface for base Rogue.
	for _, name := range []string{"stealth", "counter_instinct"} {
		if have[name] {
			t.Errorf("rogue must NOT have trans-only buff %q", name)
		}
	}
}

// Ninja is an Expanded class: no rebirth (no trans-gating) and no shared skill
// tree (no inherited buffs), so ClassBuffs is exactly the 3 NJ_ buffs. Only
// ninja_aura is scored; ninja_mastery and throwing_mastery are wired-but-inert.
func TestClassBuffs_Ninja(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("ninja")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for ninja")
	}
	got := map[string]string{} // name -> kind
	for _, b := range buffs {
		if b.SelfBuff == nil {
			continue
		}
		got[b.SelfBuff.Name] = b.SelfBuff.Kind
	}
	want := map[string]string{
		"ninja_aura":       "stat_buff",
		"ninja_mastery":    "status",
		"throwing_mastery": "status",
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("buff %q: kind %q, want %q", name, got[name], kind)
		}
	}
	// Regression lock: 3 NJ_ buffs, no inherited (Expanded class). 537
	// (ALL_INCCARRY) is excluded. This set must not change unless a
	// Ninja buff is added or removed deliberately.
	if len(buffs) != 3 {
		t.Fatalf("ClassBuffs(ninja) returned %d buffs, want 3", len(buffs))
	}
}
