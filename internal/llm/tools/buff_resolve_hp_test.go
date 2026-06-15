package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/domain"
)

// buffAnchorID returns the Gravity skill id anchoring the named buff for a
// class, read from the catalog (avoids hardcoding ids). Shared with the e2e
// test in Task 8.
func buffAnchorID(t *testing.T, cat *catalog.Catalog, class, buffName string) int {
	t.Helper()
	buffs, ok := cat.ClassBuffs(class)
	if !ok {
		t.Fatalf("ClassBuffs(%q) not found", class)
	}
	for _, b := range buffs {
		if b.SelfBuff != nil && b.SelfBuff.Name == buffName {
			return b.ID
		}
	}
	t.Fatalf("buff %q not in %s ClassBuffs", buffName, class)
	return 0
}

func TestResolveBuffs_HighPriest_FillsLevelFromAllocation(t *testing.T) {
	cat := loadCat(t) // helper in buff_resolve_test.go
	skills := []domain.SkillAlloc{
		{ID: 34, Level: 10}, // AL_BLESSING
		{ID: 361, Level: 5}, // HP_ASSUMPTIO
		{ID: buffAnchorID(t, cat, "high_priest", "lex_aeterna"), Level: 1}, // PR_LEXAETERNA
	}
	buffs := []domain.ActiveBuff{{Name: "blessing"}, {Name: "assumptio"}, {Name: "lex_aeterna"}}
	out, err := resolveBuffs("high_priest", skills, buffs, cat)
	if err != nil {
		t.Fatalf("resolveBuffs: %v", err)
	}
	got := map[string]int{}
	for _, b := range out {
		got[b.Name] = b.Level
	}
	if got["blessing"] != 10 || got["assumptio"] != 5 || got["lex_aeterna"] != 1 {
		t.Fatalf("levels not filled from allocation: %+v", got)
	}
}

func TestResolveBuffs_HighPriest_RejectsUnallocated(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("high_priest", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "blessing"}}, cat)
	if err == nil {
		t.Fatal("expected error: blessing declared but AL_BLESSING not allocated")
	}
}

func TestResolveBuffs_Knight_RejectsBlessingNotInTree(t *testing.T) {
	cat := loadCat(t)
	_, err := resolveBuffs("knight", []domain.SkillAlloc{}, []domain.ActiveBuff{{Name: "blessing"}}, cat)
	if err == nil {
		t.Fatal("expected error: blessing not available to class knight")
	}
}
