package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

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
