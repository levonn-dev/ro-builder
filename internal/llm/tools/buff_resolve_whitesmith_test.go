package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

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
