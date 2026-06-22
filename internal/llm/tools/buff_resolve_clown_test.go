package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

func TestResolveBuffs_Clown_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "clown", "musical_lesson"), Level: 10},
	}
	out, err := resolveBuffs("clown", skills, []domain.ActiveBuff{{Name: "musical_lesson"}}, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["musical_lesson"] != 10 {
		t.Fatalf("level not filled from allocation: %+v", got)
	}
}

func TestResolveBuffs_Gypsy_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "gypsy", "dancing_lesson"), Level: 10},
	}
	out, err := resolveBuffs("gypsy", skills, []domain.ActiveBuff{{Name: "dancing_lesson"}}, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["dancing_lesson"] != 10 {
		t.Fatalf("level not filled from allocation: %+v", got)
	}
}

// Cross-class gating: DC_DANCINGLESSON is a Dancer-line skill; a Clown cannot
// learn it, so resolveBuffs must reject it.
func TestResolveBuffs_Clown_RejectsDancingLesson(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("clown", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "dancing_lesson"}}, cat)
	if err == nil {
		t.Fatal("expected error: dancing_lesson not available to class clown")
	}
}
