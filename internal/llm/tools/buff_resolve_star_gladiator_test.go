package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

// resolveBuffs fills a declared buff's level from the anchor skill's allocation
// and gates by class. sls_lunar_wrath anchors to SG_MOON_ANGER.
func TestResolveBuffs_StarGladiator_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "star_gladiator", "sls_lunar_wrath"), Level: 3},
	}
	out, err := resolveBuffs("star_gladiator", skills, []domain.ActiveBuff{{Name: "sls_lunar_wrath"}}, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["sls_lunar_wrath"] != 3 {
		t.Fatalf("level not filled from allocation: %+v", got)
	}
}

// Cross-class gating: berserk is a Lord Knight-only buff; a Star Gladiator
// cannot learn it, so resolveBuffs must reject it.
func TestResolveBuffs_StarGladiator_RejectsForeignBuff(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("star_gladiator", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "berserk"}}, cat)
	if err == nil {
		t.Fatal("expected error: berserk not available to class star_gladiator")
	}
}

// sls_union is a valid SG buff, but its anchor skill (SG_FUSION) is not
// allocated on this snapshot, so it must be rejected.
func TestResolveBuffs_StarGladiator_RejectsUnallocated(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("star_gladiator", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "sls_union"}}, cat)
	if err == nil {
		t.Fatal("expected error: sls_union requires its anchor skill allocated")
	}
}
