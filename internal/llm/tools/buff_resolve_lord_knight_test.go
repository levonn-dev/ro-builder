package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

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
