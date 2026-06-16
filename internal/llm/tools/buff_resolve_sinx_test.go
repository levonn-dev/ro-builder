package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

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
