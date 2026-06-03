package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/domain"
)

func loadCat(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	return c
}

func TestResolveBuffs_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	snap := &domain.Snapshot{
		Class: "taekwon_kid",
		Skills: []domain.SkillAlloc{
			{ID: 425, Level: 7}, // TK_SEVENWIND / Mild Wind
			{ID: 493, Level: 1}, // TK_MISSION / ranker anchor
		},
		ActiveBuffs: []domain.ActiveBuff{
			{Name: "mild_wind", Element: "holy"},
			{Name: "taekwon_ranker"},
		},
	}
	buffs, err := resolveBuffs(snap.Class, snap.Skills, snap.ActiveBuffs, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	byName := map[string]scoringBuffView{}
	for _, b := range buffs {
		byName[b.Name] = scoringBuffView{b.Level, b.Element}
	}
	if byName["mild_wind"].level != 7 || byName["mild_wind"].element != "holy" {
		t.Errorf("mild_wind resolved wrong: %+v", byName["mild_wind"])
	}
	if byName["taekwon_ranker"].level != 1 {
		t.Errorf("ranker level not filled from TK_MISSION allocation: %+v", byName["taekwon_ranker"])
	}
}

func TestResolveBuffs_ErrorsWhenAnchorNotAllocated(t *testing.T) {
	cat := loadCat(t)
	snap := &domain.Snapshot{
		Class:       "taekwon_kid",
		Skills:      []domain.SkillAlloc{}, // Mild Wind not allocated
		ActiveBuffs: []domain.ActiveBuff{{Name: "mild_wind", Element: "fire"}},
	}
	if _, err := resolveBuffs(snap.Class, snap.Skills, snap.ActiveBuffs, cat); err == nil {
		t.Fatal("expected error when anchor skill not allocated, got nil")
	}
}

func TestResolveBuffs_ErrorsOnElementAboveLevel(t *testing.T) {
	cat := loadCat(t)
	snap := &domain.Snapshot{
		Class:       "taekwon_kid",
		Skills:      []domain.SkillAlloc{{ID: 425, Level: 4}},                  // Mild Wind 4
		ActiveBuffs: []domain.ActiveBuff{{Name: "mild_wind", Element: "holy"}}, // holy needs 7
	}
	if _, err := resolveBuffs(snap.Class, snap.Skills, snap.ActiveBuffs, cat); err == nil {
		t.Fatal("expected error for endow element above allocated level, got nil")
	}
}

func TestResolveBuffs_ErrorsOnBuffNotAvailableToClass(t *testing.T) {
	cat := loadCat(t)
	snap := &domain.Snapshot{
		Class:       "taekwon_kid",
		ActiveBuffs: []domain.ActiveBuff{{Name: "two_hand_quicken"}}, // not a TK buff
	}
	if _, err := resolveBuffs(snap.Class, snap.Skills, snap.ActiveBuffs, cat); err == nil {
		t.Fatal("expected error for buff not available to class, got nil")
	}
}

func TestResolveBuffs_ErrorsOnElementForNonEndowBuff(t *testing.T) {
	cat := loadCat(t)
	snap := &domain.Snapshot{
		Class:       "taekwon_kid",
		Skills:      []domain.SkillAlloc{{ID: 493, Level: 1}},                       // TK_MISSION (ranker anchor) allocated
		ActiveBuffs: []domain.ActiveBuff{{Name: "taekwon_ranker", Element: "holy"}}, // status buff takes no element
	}
	if _, err := resolveBuffs(snap.Class, snap.Skills, snap.ActiveBuffs, cat); err == nil {
		t.Fatal("expected error for element supplied on a non-endow (status) buff, got nil")
	}
}

type scoringBuffView struct {
	level   int
	element string
}
