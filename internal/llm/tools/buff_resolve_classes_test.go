// Per-class resolveBuffs unit tests: verify that declared buffs are filled from skill
// allocations and that class/allocation gates reject invalid inputs.
package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

// resolveBuffs fills a declared buff's level from the anchor skill's allocation
// and gates by class. axe_mastery anchors to AM_AXEMASTERY.
func TestResolveBuffs_Alchemist_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "alchemist", "axe_mastery"), Level: 10},
	}
	out, err := resolveBuffs("alchemist", skills, []domain.ActiveBuff{{Name: "axe_mastery"}}, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["axe_mastery"] != 10 {
		t.Fatalf("level not filled from allocation: %+v", got)
	}
}

// Cross-class gating: berserk is a Lord Knight-only buff; an Alchemist cannot
// learn it, so resolveBuffs must reject it.
func TestResolveBuffs_Alchemist_RejectsForeignBuff(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("alchemist", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "berserk"}}, cat)
	if err == nil {
		t.Fatal("expected error: berserk not available to class alchemist")
	}
}

// axe_mastery is a valid Alchemist buff, but its anchor skill (AM_AXEMASTERY) is
// not allocated on this snapshot, so it must be rejected.
func TestResolveBuffs_Alchemist_RejectsUnallocated(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("alchemist", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "axe_mastery"}}, cat)
	if err == nil {
		t.Fatal("expected error: axe_mastery requires its anchor skill allocated")
	}
}

func TestResolveBuffs_Gunslinger_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "gunslinger", "single_action"), Level: 10},
		{ID: buffAnchorID(t, cat, "gunslinger", "madness_canceller"), Level: 1},
	}
	buffs := []domain.ActiveBuff{
		{Name: "single_action"},
		{Name: "madness_canceller"},
	}
	out, err := resolveBuffs("gunslinger", skills, buffs, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["single_action"] != 10 || got["madness_canceller"] != 1 {
		t.Fatalf("levels not filled from allocation: %+v", got)
	}
}

func TestResolveBuffs_Knight_RejectsSingleAction(t *testing.T) {
	cat := loadCat(t)
	// GS_SINGLEACTION (Single Action) is a Gunslinger skill; a Knight cannot learn it.
	_, err := resolveBuffs("knight", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "single_action"}}, cat)
	if err == nil {
		t.Fatal("expected error: single_action not available to class knight")
	}
}

func TestResolveBuffs_Gunslinger_RejectsUnallocated(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("gunslinger", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "single_action"}}, cat)
	if err == nil {
		t.Fatal("expected error: single_action declared but GS_SINGLEACTION not allocated")
	}
}

// resolveBuffs fills a declared buff's level from the anchor skill's allocation and
// gates by class. soul_drain anchors to HW_SOULDRAIN.
func TestResolveBuffs_HighWizard_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "high_wizard", "soul_drain"), Level: 10},
	}
	out, err := resolveBuffs("high_wizard", skills, []domain.ActiveBuff{{Name: "soul_drain"}}, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["soul_drain"] != 10 {
		t.Fatalf("level not filled from allocation: %+v", got)
	}
}

// Cross-class gating: soul_drain is High Wizard-only (HW_SOULDRAIN absent from the
// Wizard tree), so resolveBuffs must reject it for a Wizard.
func TestResolveBuffs_Wizard_RejectsHighWizardOnly(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("wizard", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "soul_drain"}}, cat)
	if err == nil {
		t.Fatal("expected error: soul_drain not available to class wizard")
	}
}

// Cross-class gating: berserk is a Lord Knight-only buff; a High Wizard cannot learn
// it, so resolveBuffs must reject it.
func TestResolveBuffs_HighWizard_RejectsForeignBuff(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("high_wizard", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "berserk"}}, cat)
	if err == nil {
		t.Fatal("expected error: berserk not available to class high_wizard")
	}
}

// resolveBuffs fills a declared buff's level from the anchor skill's allocation
// and gates by class. ninja_aura anchors to NJ_NEN.
func TestResolveBuffs_Ninja_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "ninja", "ninja_aura"), Level: 5},
	}
	out, err := resolveBuffs("ninja", skills, []domain.ActiveBuff{{Name: "ninja_aura"}}, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["ninja_aura"] != 5 {
		t.Fatalf("level not filled from allocation: %+v", got)
	}
}

// Cross-class gating: berserk is a Lord Knight-only buff; a Ninja cannot learn
// it, so resolveBuffs must reject it.
func TestResolveBuffs_Ninja_RejectsForeignBuff(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("ninja", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "berserk"}}, cat)
	if err == nil {
		t.Fatal("expected error: berserk not available to class ninja")
	}
}

// ninja_aura is a valid Ninja buff, but its anchor skill (NJ_NEN) is not
// allocated on this snapshot, so it must be rejected.
func TestResolveBuffs_Ninja_RejectsUnallocated(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("ninja", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "ninja_aura"}}, cat)
	if err == nil {
		t.Fatal("expected error: ninja_aura requires its anchor skill allocated")
	}
}

func TestResolveBuffs_Paladin_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "paladin", "faith"), Level: 10},
		{ID: buffAnchorID(t, cat, "paladin", "spear_quicken"), Level: 10},
	}
	buffs := []domain.ActiveBuff{
		{Name: "faith"},
		{Name: "spear_quicken"},
	}
	out, err := resolveBuffs("paladin", skills, buffs, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["faith"] != 10 || got["spear_quicken"] != 10 {
		t.Fatalf("levels not filled from allocation: %+v", got)
	}
}

func TestResolveBuffs_Knight_RejectsFaith(t *testing.T) {
	cat := loadCat(t)
	// CR_TRUST (Faith) is a Crusader/Paladin skill; a Knight cannot learn it.
	_, err := resolveBuffs("knight", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "faith"}}, cat)
	if err == nil {
		t.Fatal("expected error: faith not available to class knight")
	}
}

func TestResolveBuffs_Paladin_RejectsUnallocated(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("paladin", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "faith"}}, cat)
	if err == nil {
		t.Fatal("expected error: faith declared but CR_TRUST not allocated")
	}
}

// resolveBuffs fills a declared buff's level from the anchor skill's allocation
// and gates by class. kaina anchors to SL_KAINA.
func TestResolveBuffs_SoulLinker_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "soul_linker", "kaina"), Level: 7},
	}
	out, err := resolveBuffs("soul_linker", skills, []domain.ActiveBuff{{Name: "kaina"}}, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["kaina"] != 7 {
		t.Fatalf("level not filled from allocation: %+v", got)
	}
}

// Cross-class gating: berserk is a Lord Knight-only buff; a Soul Linker cannot
// learn it, so resolveBuffs must reject it.
func TestResolveBuffs_SoulLinker_RejectsForeignBuff(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("soul_linker", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "berserk"}}, cat)
	if err == nil {
		t.Fatal("expected error: berserk not available to class soul_linker")
	}
}

// taekwon_ranker is TaeKwon-Kid-only -- a Soul Linker cannot use it, so resolveBuffs
// must reject it (the usability exclusion keeps TK_MISSION out of the SL tree).
func TestResolveBuffs_SoulLinker_RejectsTaekwonRanker(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("soul_linker", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "taekwon_ranker"}}, cat)
	if err == nil {
		t.Fatal("expected error: taekwon_ranker not available to class soul_linker")
	}
}

func TestResolveBuffs_Stalker_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "stalker", "stealth"), Level: 5},
		{ID: buffAnchorID(t, cat, "stalker", "close_confine"), Level: 1},
	}
	buffs := []domain.ActiveBuff{
		{Name: "stealth"},
		{Name: "close_confine"},
	}
	out, err := resolveBuffs("stalker", skills, buffs, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["stealth"] != 5 || got["close_confine"] != 1 {
		t.Fatalf("levels not filled from allocation: %+v", got)
	}
}

func TestResolveBuffs_Assassin_RejectsStealth(t *testing.T) {
	cat := loadCat(t)
	// ST_CHASEWALK (Stealth) is a Stalker skill; an Assassin cannot learn it.
	_, err := resolveBuffs("assassin", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "stealth"}}, cat)
	if err == nil {
		t.Fatal("expected error: stealth not available to class assassin")
	}
}

func TestResolveBuffs_Stalker_RejectsUnallocated(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("stalker", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "stealth"}}, cat)
	if err == nil {
		t.Fatal("expected error: stealth declared but ST_CHASEWALK not allocated")
	}
}

// resolveBuffs fills a declared buff's level from the anchor skill's allocation
// and gates by class. sls_lunar_wrath anchors to SG_MOON_ANGER.
func TestResolveBuffs_StarGladiator_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "star_gladiator", "sls_lunar_wrath"), Level: 3},
	}
	out, err := resolveBuffs("star_gladiator", skills, []domain.ActiveBuff{{Name: "sls_lunar_wrath"}}, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["sls_lunar_wrath"] != 3 {
		t.Fatalf("level not filled from allocation: %+v", got)
	}
}

// Cross-class gating: berserk is a Lord Knight-only buff; a Star Gladiator
// cannot learn it, so resolveBuffs must reject it.
func TestResolveBuffs_StarGladiator_RejectsForeignBuff(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("star_gladiator", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "berserk"}}, cat)
	if err == nil {
		t.Fatal("expected error: berserk not available to class star_gladiator")
	}
}

// sls_union is a valid SG buff, but its anchor skill (SG_FUSION) is not
// allocated on this snapshot, so it must be rejected.
func TestResolveBuffs_StarGladiator_RejectsUnallocated(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("star_gladiator", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "sls_union"}}, cat)
	if err == nil {
		t.Fatal("expected error: sls_union requires its anchor skill allocated")
	}
}

func TestResolveBuffs_Champion_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "champion", "iron_fists"), Level: 10},
		{ID: buffAnchorID(t, cat, "champion", "triple_attack"), Level: 10},
		{ID: buffAnchorID(t, cat, "champion", "fury"), Level: 5},
	}
	buffs := []domain.ActiveBuff{
		{Name: "iron_fists"},
		{Name: "triple_attack"},
		{Name: "fury"},
	}
	out, err := resolveBuffs("champion", skills, buffs, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["iron_fists"] != 10 || got["triple_attack"] != 10 || got["fury"] != 5 {
		t.Fatalf("levels not filled from allocation: %+v", got)
	}
}

func TestResolveBuffs_Knight_RejectsFury(t *testing.T) {
	cat := loadCat(t)
	// MO_EXPLOSIONSPIRITS (Fury) is a Monk/Champion skill; a Knight cannot learn it.
	_, err := resolveBuffs("knight", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "fury"}}, cat)
	if err == nil {
		t.Fatal("expected error: fury not available to class knight")
	}
}

func TestResolveBuffs_Champion_RejectsUnallocated(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("champion", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "fury"}}, cat)
	if err == nil {
		t.Fatal("expected error: fury declared but MO_EXPLOSIONSPIRITS not allocated")
	}
}

func TestResolveBuffs_LordKnight_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "lord_knight", "two_handed_sword_mastery"), Level: 10},
		{ID: buffAnchorID(t, cat, "lord_knight", "concentration"), Level: 5},
		{ID: buffAnchorID(t, cat, "lord_knight", "berserk"), Level: 1},
	}
	buffs := []domain.ActiveBuff{
		{Name: "two_handed_sword_mastery"},
		{Name: "concentration"},
		{Name: "berserk"},
	}
	out, err := resolveBuffs("lord_knight", skills, buffs, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["two_handed_sword_mastery"] != 10 || got["concentration"] != 5 || got["berserk"] != 1 {
		t.Fatalf("levels not filled from allocation: %+v", got)
	}
}

func TestResolveBuffs_Knight_RejectsTransOnlyBerserk(t *testing.T) {
	cat := loadCat(t)
	// LK_BERSERK is not in base knight's tree.
	_, err := resolveBuffs("knight", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "berserk"}}, cat)
	if err == nil {
		t.Fatal("expected error: berserk not available to class knight")
	}
}

func TestResolveBuffs_LordKnight_RejectsUnallocated(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("lord_knight", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "concentration"}}, cat)
	if err == nil {
		t.Fatal("expected error: concentration declared but LK_CONCENTRATION not allocated")
	}
}

func TestResolveBuffs_Sniper_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "sniper", "owls_eye"), Level: 10},
		{ID: buffAnchorID(t, cat, "sniper", "improve_concentration"), Level: 10},
		{ID: buffAnchorID(t, cat, "sniper", "true_sight"), Level: 10},
	}
	buffs := []domain.ActiveBuff{
		{Name: "owls_eye"},
		{Name: "improve_concentration"},
		{Name: "true_sight"},
	}
	out, err := resolveBuffs("sniper", skills, buffs, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["owls_eye"] != 10 || got["improve_concentration"] != 10 || got["true_sight"] != 10 {
		t.Fatalf("levels not filled from allocation: %+v", got)
	}
}

func TestResolveBuffs_Hunter_RejectsTransOnlyTrueSight(t *testing.T) {
	cat := loadCat(t)
	// SN_SIGHT is not in base hunter's tree.
	_, err := resolveBuffs("hunter", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "true_sight"}}, cat)
	if err == nil {
		t.Fatal("expected error: true_sight not available to class hunter")
	}
}

func TestResolveBuffs_Sniper_RejectsUnallocated(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("sniper", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "owls_eye"}}, cat)
	if err == nil {
		t.Fatal("expected error: owls_eye declared but AC_OWL not allocated")
	}
}

func TestResolveBuffs_Whitesmith_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "whitesmith", "over_thrust"), Level: 5},
		{ID: buffAnchorID(t, cat, "whitesmith", "maximize_power"), Level: 5},
		{ID: buffAnchorID(t, cat, "whitesmith", "weaponry_research"), Level: 10},
	}
	buffs := []domain.ActiveBuff{
		{Name: "over_thrust"},
		{Name: "maximize_power"},
		{Name: "weaponry_research"},
	}
	out, err := resolveBuffs("whitesmith", skills, buffs, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["over_thrust"] != 5 || got["maximize_power"] != 5 || got["weaponry_research"] != 10 {
		t.Fatalf("levels not filled from allocation: %+v", got)
	}
}

func TestResolveBuffs_Blacksmith_RejectsTransOnlyMaxOverThrust(t *testing.T) {
	cat := loadCat(t)
	// WS_OVERTHRUSTMAX is not in base blacksmith's tree.
	_, err := resolveBuffs("blacksmith", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "maximum_over_thrust"}}, cat)
	if err == nil {
		t.Fatal("expected error: maximum_over_thrust not available to class blacksmith")
	}
}

func TestResolveBuffs_Whitesmith_RejectsUnallocated(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("whitesmith", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "over_thrust"}}, cat)
	if err == nil {
		t.Fatal("expected error: over_thrust declared but BS_OVERTHRUST not allocated")
	}
}

func TestResolveBuffs_Clown_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "clown", "musical_lesson"), Level: 10},
	}
	out, err := resolveBuffs("clown", skills, []domain.ActiveBuff{{Name: "musical_lesson"}}, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["musical_lesson"] != 10 {
		t.Fatalf("level not filled from allocation: %+v", got)
	}
}

func TestResolveBuffs_Gypsy_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "gypsy", "dancing_lesson"), Level: 10},
	}
	out, err := resolveBuffs("gypsy", skills, []domain.ActiveBuff{{Name: "dancing_lesson"}}, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["dancing_lesson"] != 10 {
		t.Fatalf("level not filled from allocation: %+v", got)
	}
}

// Cross-class gating: DC_DANCINGLESSON is a Dancer-line skill; a Clown cannot
// learn it, so resolveBuffs must reject it.
func TestResolveBuffs_Clown_RejectsDancingLesson(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("clown", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "dancing_lesson"}}, cat)
	if err == nil {
		t.Fatal("expected error: dancing_lesson not available to class clown")
	}
}

// no_death_bonus is a class-innate buff: it resolves at a fixed level 1 with NO
// skill allocated (it has no anchor skill).
func TestResolveBuffs_SuperNovice_InnateAtFixedLevel(t *testing.T) {
	cat := loadCat(t)
	out, err := resolveBuffs("super_novice", nil, []domain.ActiveBuff{{Name: "no_death_bonus"}}, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	if len(out) != 1 || out[0].Name != "no_death_bonus" || out[0].Level != 1 {
		t.Fatalf("want one no_death_bonus at level 1, got %+v", out)
	}
}

// Innate buffs are class-gated: a knight cannot declare the Super Novice No-Death
// Bonus.
func TestResolveBuffs_Knight_RejectsSuperNoviceInnate(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("knight", nil, []domain.ActiveBuff{{Name: "no_death_bonus"}}, cat)
	if err == nil {
		t.Fatal("expected error: no_death_bonus not available to class knight")
	}
}

// Innate buffs are not weapon endows: an element is rejected.
func TestResolveBuffs_SuperNovice_InnateRejectsElement(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("super_novice", nil, []domain.ActiveBuff{{Name: "no_death_bonus", Element: "holy"}}, cat)
	if err == nil {
		t.Fatal("expected error: innate buff takes no element")
	}
}

// The existing skill-buff path still works alongside the innate path: owls_eye
// resolves from its allocation on a super_novice (AC_OWL is inherited).
func TestResolveBuffs_SuperNovice_SkillBuffStillWorks(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{{ID: buffAnchorID(t, cat, "super_novice", "owls_eye"), Level: 10}}
	out, err := resolveBuffs("super_novice", skills, []domain.ActiveBuff{{Name: "owls_eye"}}, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	if len(out) != 1 || out[0].Name != "owls_eye" || out[0].Level != 10 {
		t.Fatalf("want owls_eye at level 10, got %+v", out)
	}
}

func TestResolveBuffs_Professor_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "professor", "flame_launcher"), Level: 5},
		{ID: buffAnchorID(t, cat, "professor", "volcano"), Level: 5},
		{ID: buffAnchorID(t, cat, "professor", "mind_breaker"), Level: 5},
		{ID: buffAnchorID(t, cat, "professor", "dragonology"), Level: 5},
	}
	buffs := []domain.ActiveBuff{
		{Name: "flame_launcher", Element: "fire"},
		{Name: "volcano"}, {Name: "mind_breaker"}, {Name: "dragonology"},
	}
	out, err := resolveBuffs("professor", skills, buffs, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["flame_launcher"] != 5 || got["volcano"] != 5 || got["mind_breaker"] != 5 || got["dragonology"] != 5 {
		t.Fatalf("levels not filled from allocation: %+v", got)
	}
}

func TestResolveBuffs_Sage_RejectsProfessorOnlyBuff(t *testing.T) {
	cat := loadCat(t)
	// mind_breaker (PF_MINDBREAKER) is not in sage's tree.
	_, err := resolveBuffs("sage", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "mind_breaker"}}, cat)
	if err == nil {
		t.Fatal("expected error: mind_breaker not available to class sage")
	}
}

func TestResolveBuffs_Professor_RejectsUnallocatedLand(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("professor", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "volcano"}}, cat)
	if err == nil {
		t.Fatal("expected error: volcano declared but SA_VOLCANO not allocated")
	}
}

func TestResolveBuffs_Professor_RejectsWrongEndowElement(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{{ID: buffAnchorID(t, cat, "professor", "flame_launcher"), Level: 5}}
	// flame_launcher endow is fire-only; declaring water must be rejected.
	_, err := resolveBuffs("professor", skills, []domain.ActiveBuff{{Name: "flame_launcher", Element: "water"}}, cat)
	if err == nil {
		t.Fatal("expected error: flame_launcher endow element must be fire")
	}
}

func TestResolveBuffs_AssassinCross_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "assassin_cross", "enchant_deadly_poison"), Level: 5},
		{ID: buffAnchorID(t, cat, "assassin_cross", "enchant_poison"), Level: 5},
		{ID: buffAnchorID(t, cat, "assassin_cross", "katar_mastery"), Level: 10},
	}
	buffs := []domain.ActiveBuff{
		{Name: "enchant_deadly_poison"},
		{Name: "enchant_poison", Element: "poison"},
		{Name: "katar_mastery"},
	}
	out, err := resolveBuffs("assassin_cross", skills, buffs, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["enchant_deadly_poison"] != 5 || got["enchant_poison"] != 5 || got["katar_mastery"] != 10 {
		t.Fatalf("levels not filled from allocation: %+v", got)
	}
}

func TestResolveBuffs_Assassin_RejectsTransOnlyEDP(t *testing.T) {
	cat := loadCat(t)
	// ASC_EDP is not in base assassin's tree.
	_, err := resolveBuffs("assassin", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "enchant_deadly_poison"}}, cat)
	if err == nil {
		t.Fatal("expected error: enchant_deadly_poison not available to class assassin")
	}
}

func TestResolveBuffs_AssassinCross_RejectsUnallocated(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("assassin_cross", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "katar_mastery"}}, cat)
	if err == nil {
		t.Fatal("expected error: katar_mastery declared but AS_KATAR not allocated")
	}
}

func TestResolveBuffs_AssassinCross_RejectsWrongEndowElement(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{{ID: buffAnchorID(t, cat, "assassin_cross", "enchant_poison"), Level: 5}}
	// enchant_poison endow is poison-only; declaring fire must be rejected.
	_, err := resolveBuffs("assassin_cross", skills, []domain.ActiveBuff{{Name: "enchant_poison", Element: "fire"}}, cat)
	if err == nil {
		t.Fatal("expected error: enchant_poison endow element must be poison")
	}
}
