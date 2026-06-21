package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

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
