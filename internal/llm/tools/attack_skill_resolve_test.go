package tools

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

func TestResolveAttackSkills_LevelFromAllocation(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{{ID: 413, Level: 7}} // TK_STORMKICK
	declared := []domain.ScoredSkill{{Name: "tornado_kick", Primary: true}}
	out, err := resolveAttackSkills("taekwon_kid", skills, declared, cat)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "tornado_kick" || out[0].Level != 7 || !out[0].Primary {
		t.Fatalf("bad resolve: %+v", out)
	}
}

func TestResolveAttackSkills_RejectsUnallocated(t *testing.T) {
	cat := loadCat(t)
	declared := []domain.ScoredSkill{{Name: "tornado_kick", Primary: true}}
	if _, err := resolveAttackSkills("taekwon_kid", nil, declared, cat); err == nil {
		t.Fatal("expected error: skill not allocated")
	}
}

func TestResolveAttackSkills_RequiresExactlyOnePrimary(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{{ID: 413, Level: 7}, {ID: 417, Level: 5}}
	zero := []domain.ScoredSkill{{Name: "tornado_kick"}, {Name: "roundhouse"}}
	if _, err := resolveAttackSkills("taekwon_kid", skills, zero, cat); err == nil {
		t.Fatal("expected error: no primary")
	}
	two := []domain.ScoredSkill{{Name: "tornado_kick", Primary: true}, {Name: "roundhouse", Primary: true}}
	if _, err := resolveAttackSkills("taekwon_kid", skills, two, cat); err == nil {
		t.Fatal("expected error: multiple primaries")
	}
}

func TestResolveAttackSkills_UnknownNameErrors(t *testing.T) {
	cat := loadCat(t)
	skills := []domain.SkillAlloc{{ID: 413, Level: 7}}
	declared := []domain.ScoredSkill{{Name: "fireball", Primary: true}}
	if _, err := resolveAttackSkills("taekwon_kid", skills, declared, cat); err == nil {
		t.Fatal("expected error: unknown attack skill name")
	}
}

func TestResolveAttackSkills_EmptyIsNil(t *testing.T) {
	out, err := resolveAttackSkills("taekwon_kid", nil, nil, nil)
	if err != nil || out != nil {
		t.Fatalf("empty declared should be (nil,nil); got %v %v", out, err)
	}
}
