package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

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
