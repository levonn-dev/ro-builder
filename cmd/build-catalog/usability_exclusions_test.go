package main

import (
	"testing"

	"github.com/levonn-dev/ro-builder/internal/data"
)

// filterSkills drops allocated-but-unusable inherited skills (e.g. TK_MISSION for
// the TaeKwon 2nd classes).
func TestFilterSkills_DropsExcludedAegisNames(t *testing.T) {
	skills := []data.ClassSkillEntry{
		{AegisName: "TK_MISSION"},
		{AegisName: "TK_SEVENWIND"},
		{AegisName: "SL_KAINA"},
	}
	out := filterSkills(skills, map[string]bool{"TK_MISSION": true})
	if len(out) != 2 {
		t.Fatalf("filterSkills: got %d skills, want 2", len(out))
	}
	for _, e := range out {
		if e.AegisName == "TK_MISSION" {
			t.Fatalf("TK_MISSION should have been dropped: %+v", out)
		}
	}
}

func TestFilterSkills_NilOrEmptyDropIsNoop(t *testing.T) {
	skills := []data.ClassSkillEntry{{AegisName: "TK_MISSION"}, {AegisName: "SL_KAINA"}}
	if got := filterSkills(skills, nil); len(got) != 2 {
		t.Fatalf("nil drop should be a no-op, got %d", len(got))
	}
	if got := filterSkills(skills, map[string]bool{}); len(got) != 2 {
		t.Fatalf("empty drop should be a no-op, got %d", len(got))
	}
}

// markUnusable flags allocated-but-unusable inherited skills (the Taekwon kicks
// a Soul Linker keeps) WITHOUT dropping them, so they still count toward the
// skill-point budget.
func TestMarkUnusable_FlagsWithoutDropping(t *testing.T) {
	skills := []data.ClassSkillEntry{
		{AegisName: "TK_STORMKICK"},
		{AegisName: "TK_SEVENWIND"},
	}
	out := markUnusable(skills, map[string]bool{"TK_STORMKICK": true})
	if len(out) != 2 {
		t.Fatalf("markUnusable must not drop skills, got %d", len(out))
	}
	for _, e := range out {
		switch e.AegisName {
		case "TK_STORMKICK":
			if !e.Unusable {
				t.Error("TK_STORMKICK should be flagged Unusable")
			}
		case "TK_SEVENWIND":
			if e.Unusable {
				t.Error("TK_SEVENWIND should remain usable")
			}
		}
	}
	// Source slice is untouched (markUnusable copies).
	if skills[0].Unusable {
		t.Error("markUnusable must not mutate the caller's slice")
	}
	// nil flag is a no-op.
	if got := markUnusable(skills, nil); len(got) != 2 {
		t.Fatalf("nil flag should be a no-op, got %d", len(got))
	}
}
