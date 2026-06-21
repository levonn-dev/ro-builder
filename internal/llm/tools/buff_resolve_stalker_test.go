package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

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
