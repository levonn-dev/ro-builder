package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

// no_death_bonus is a class-innate buff: it resolves at a fixed level 1 with NO
// skill allocated (it has no anchor skill).
func TestResolveBuffs_SuperNovice_InnateAtFixedLevel(t *testing.T) {
	cat := loadCat(t)
	out, err := resolveBuffs("super_novice", nil, []domain.ActiveBuff{{Name: "no_death_bonus"}}, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	if len(out) != 1 || out[0].Name != "no_death_bonus" || out[0].Level != 1 {
		t.Fatalf("want one no_death_bonus at level 1, got %+v", out)
	}
}

// Innate buffs are class-gated: a knight cannot declare the Super Novice No-Death
// Bonus.
func TestResolveBuffs_Knight_RejectsSuperNoviceInnate(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("knight", nil, []domain.ActiveBuff{{Name: "no_death_bonus"}}, cat)
	if err == nil {
		t.Fatal("expected error: no_death_bonus not available to class knight")
	}
}

// Innate buffs are not weapon endows: an element is rejected.
func TestResolveBuffs_SuperNovice_InnateRejectsElement(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("super_novice", nil, []domain.ActiveBuff{{Name: "no_death_bonus", Element: "holy"}}, cat)
	if err == nil {
		t.Fatal("expected error: innate buff takes no element")
	}
}

// The existing skill-buff path still works alongside the innate path: owls_eye
// resolves from its allocation on a super_novice (AC_OWL is inherited).
func TestResolveBuffs_SuperNovice_SkillBuffStillWorks(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{{ID: buffAnchorID(t, cat, "super_novice", "owls_eye"), Level: 10}}
	out, err := resolveBuffs("super_novice", skills, []domain.ActiveBuff{{Name: "owls_eye"}}, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	if len(out) != 1 || out[0].Name != "owls_eye" || out[0].Level != 10 {
		t.Fatalf("want owls_eye at level 10, got %+v", out)
	}
}
