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
