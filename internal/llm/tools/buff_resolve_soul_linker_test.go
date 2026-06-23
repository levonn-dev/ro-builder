package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

// resolveBuffs fills a declared buff's level from the anchor skill's allocation
// and gates by class. kaina anchors to SL_KAINA.
func TestResolveBuffs_SoulLinker_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "soul_linker", "kaina"), Level: 7},
	}
	out, err := resolveBuffs("soul_linker", skills, []domain.ActiveBuff{{Name: "kaina"}}, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["kaina"] != 7 {
		t.Fatalf("level not filled from allocation: %+v", got)
	}
}

// Cross-class gating: berserk is a Lord Knight-only buff; a Soul Linker cannot
// learn it, so resolveBuffs must reject it.
func TestResolveBuffs_SoulLinker_RejectsForeignBuff(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("soul_linker", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "berserk"}}, cat)
	if err == nil {
		t.Fatal("expected error: berserk not available to class soul_linker")
	}
}

// taekwon_ranker is TaeKwon-Kid-only -- a Soul Linker cannot use it, so resolveBuffs
// must reject it (the usability exclusion keeps TK_MISSION out of the SL tree).
func TestResolveBuffs_SoulLinker_RejectsTaekwonRanker(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("soul_linker", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "taekwon_ranker"}}, cat)
	if err == nil {
		t.Fatal("expected error: taekwon_ranker not available to class soul_linker")
	}
}
