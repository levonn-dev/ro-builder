package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

// resolveBuffs fills a declared buff's level from the anchor skill's allocation and
// gates by class. soul_drain anchors to HW_SOULDRAIN.
func TestResolveBuffs_HighWizard_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{
		{ID: buffAnchorID(t, cat, "high_wizard", "soul_drain"), Level: 10},
	}
	out, err := resolveBuffs("high_wizard", skills, []domain.ActiveBuff{{Name: "soul_drain"}}, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["soul_drain"] != 10 {
		t.Fatalf("level not filled from allocation: %+v", got)
	}
}

// Cross-class gating: soul_drain is High Wizard-only (HW_SOULDRAIN absent from the
// Wizard tree), so resolveBuffs must reject it for a Wizard.
func TestResolveBuffs_Wizard_RejectsHighWizardOnly(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("wizard", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "soul_drain"}}, cat)
	if err == nil {
		t.Fatal("expected error: soul_drain not available to class wizard")
	}
}

// Cross-class gating: berserk is a Lord Knight-only buff; a High Wizard cannot learn
// it, so resolveBuffs must reject it.
func TestResolveBuffs_HighWizard_RejectsForeignBuff(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("high_wizard", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "berserk"}}, cat)
	if err == nil {
		t.Fatal("expected error: berserk not available to class high_wizard")
	}
}
