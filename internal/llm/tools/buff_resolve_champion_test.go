package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

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
