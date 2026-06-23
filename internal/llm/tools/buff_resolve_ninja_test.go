package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

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
