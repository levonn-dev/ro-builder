package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

func TestResolveBuffs_Gunslinger_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "gunslinger", "single_action"), Level: 10},
		{ID: buffAnchorID(t, cat, "gunslinger", "madness_canceller"), Level: 1},
	}
	buffs := []domain.ActiveBuff{
		{Name: "single_action"},
		{Name: "madness_canceller"},
	}
	out, err := resolveBuffs("gunslinger", skills, buffs, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["single_action"] != 10 || got["madness_canceller"] != 1 {
		t.Fatalf("levels not filled from allocation: %+v", got)
	}
}

func TestResolveBuffs_Knight_RejectsSingleAction(t *testing.T) {
	cat := loadCat(t)
	// GS_SINGLEACTION (Single Action) is a Gunslinger skill; a Knight cannot learn it.
	_, err := resolveBuffs("knight", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "single_action"}}, cat)
	if err == nil {
		t.Fatal("expected error: single_action not available to class knight")
	}
}

func TestResolveBuffs_Gunslinger_RejectsUnallocated(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("gunslinger", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "single_action"}}, cat)
	if err == nil {
		t.Fatal("expected error: single_action declared but GS_SINGLEACTION not allocated")
	}
}
