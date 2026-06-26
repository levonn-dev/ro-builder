package main

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/data"
)

func TestApplyAttackSkills_Merges(t *testing.T) {
	skills := []data.Skill{{AegisName: "TK_STORMKICK"}}
	overlay := map[string]*data.AttackSkill{"TK_STORMKICK": {Name: "tornado_kick"}}
	n, err := applyAttackSkills(skills, overlay)
	if err != nil || n != 1 {
		t.Fatalf("apply: n=%d err=%v", n, err)
	}
	if skills[0].AttackSkill == nil || skills[0].AttackSkill.Name != "tornado_kick" {
		t.Fatalf("AttackSkill not merged: %+v", skills[0].AttackSkill)
	}
}

func TestApplyAttackSkills_UnknownAegisErrors(t *testing.T) {
	skills := []data.Skill{{AegisName: "TK_STORMKICK"}}
	overlay := map[string]*data.AttackSkill{"NOPE": {Name: "x"}}
	if _, err := applyAttackSkills(skills, overlay); err == nil {
		t.Fatal("expected error for unknown aegis_name")
	}
}
