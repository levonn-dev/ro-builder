package domain

import (
	"strings"
	"testing"
)

// validTrajectory returns a minimal valid 3-snapshot SinX trajectory:
// thief at 30 → assassin at 60 → assassin_cross post-rebirth at 99/70.
// Helper builds the smallest example that exercises ordering, class
// change within a cycle, and a rebirth boundary in one trajectory.
func validTrajectory() Trajectory {
	return Trajectory{
		Class: "assassin_cross",
		Snapshots: []Snapshot{
			{
				Class: "thief",
				Level: Level{Base: 30, Job: 30},
				Stats: Stats{Str: 9, Agi: 9, Vit: 1, Int: 1, Dex: 9, Luk: 1},
				Skills: []SkillAlloc{
					{ID: 13, Level: 5}, // Double Attack
				},
			},
			{
				Class: "assassin",
				Level: Level{Base: 60, Job: 30},
				Stats: Stats{Str: 30, Agi: 30, Vit: 1, Int: 1, Dex: 20, Luk: 1},
				Skills: []SkillAlloc{
					{ID: 13, Level: 10}, // Double Attack capped
					{ID: 79, Level: 5},  // Right-Hand Mastery (sin)
				},
			},
			{
				Class:       "assassin_cross",
				Level:       Level{Base: 99, Job: 70},
				PostRebirth: true,
				Stats:       Stats{Str: 1, Agi: 1, Vit: 1, Int: 1, Dex: 1, Luk: 1},
				Skills: []SkillAlloc{
					{ID: 13, Level: 1},
				},
			},
		},
	}
}

func TestTrajectory_Validate_Happy(t *testing.T) {
	traj := validTrajectory()
	if err := traj.Validate(); err != nil {
		t.Fatalf("expected valid trajectory, got: %v", err)
	}
}

func TestTrajectory_Validate_EmptyClass(t *testing.T) {
	traj := validTrajectory()
	traj.Class = ""
	if err := traj.Validate(); err == nil || !strings.Contains(err.Error(), "empty class") {
		t.Fatalf("expected empty-class error, got: %v", err)
	}
}

func TestTrajectory_Validate_NoSnapshots(t *testing.T) {
	traj := Trajectory{Class: "novice", Snapshots: nil}
	if err := traj.Validate(); err == nil || !strings.Contains(err.Error(), "no snapshots") {
		t.Fatalf("expected no-snapshots error, got: %v", err)
	}
}

func TestTrajectory_Validate_LastSnapshotMustMatchClass(t *testing.T) {
	traj := validTrajectory()
	traj.Snapshots[len(traj.Snapshots)-1].Class = "lord_knight"
	if err := traj.Validate(); err == nil || !strings.Contains(err.Error(), "last snapshot class") {
		t.Fatalf("expected last-snapshot-class mismatch error, got: %v", err)
	}
}

// Within a rebirth cycle, base level must non-decrease.
func TestTrajectory_Validate_LevelRegressionWithinCycle(t *testing.T) {
	traj := validTrajectory()
	traj.Snapshots[1].Level.Base = 20 // less than snapshots[0].Level.Base=30
	err := traj.Validate()
	if err == nil || !strings.Contains(err.Error(), "base level") {
		t.Fatalf("expected base-level-regression error, got: %v", err)
	}
}

// Within a rebirth cycle, individual stats may not regress.
func TestTrajectory_Validate_StatsRegressionWithinCycle(t *testing.T) {
	traj := validTrajectory()
	traj.Snapshots[1].Stats.Str = 5 // less than snapshots[0].Stats.Str=9
	err := traj.Validate()
	if err == nil || !strings.Contains(err.Error(), "STR regressed") {
		t.Fatalf("expected STR regression error, got: %v", err)
	}
}

// Skills already taken cannot drop in level within a cycle.
func TestTrajectory_Validate_SkillsRegressionWithinCycle(t *testing.T) {
	traj := validTrajectory()
	traj.Snapshots[1].Skills = []SkillAlloc{{ID: 13, Level: 3}} // dropped from 5 → 3
	err := traj.Validate()
	if err == nil || !strings.Contains(err.Error(), "skill id 13 regressed") {
		t.Fatalf("expected skill regression error, got: %v", err)
	}
}

// A skill the player had cannot disappear entirely within a cycle.
func TestTrajectory_Validate_SkillDisappearedWithinCycle(t *testing.T) {
	traj := validTrajectory()
	// Drop Double Attack from snapshots[1]; snapshots[0] still has it.
	traj.Snapshots[1].Skills = []SkillAlloc{{ID: 79, Level: 5}}
	err := traj.Validate()
	if err == nil || !strings.Contains(err.Error(), "disappeared") {
		t.Fatalf("expected skill-disappeared error, got: %v", err)
	}
}

// Crossing the rebirth boundary, monotonicity does NOT apply; stats
// reset, skills can drop. This is the intended behavior for transcend.
func TestTrajectory_Validate_RebirthResetsAreFine(t *testing.T) {
	traj := validTrajectory()
	// snapshots[2] (PostRebirth) already has lower stats than snapshots[1].
	// This must be accepted, not rejected.
	if err := traj.Validate(); err != nil {
		t.Fatalf("rebirth reset wrongly rejected: %v", err)
	}
}

// Going from PostRebirth=true back to false later in the trajectory is
// nonsensical; only one rebirth is supported.
func TestTrajectory_Validate_PostRebirthCannotRegress(t *testing.T) {
	traj := Trajectory{
		Class: "assassin_cross",
		Snapshots: []Snapshot{
			{
				Class:       "high_thief",
				Level:       Level{Base: 20, Job: 10},
				PostRebirth: true,
				Stats:       Stats{Str: 5, Agi: 5, Vit: 1, Int: 1, Dex: 1, Luk: 1},
			},
			{
				Class:       "assassin_cross",
				Level:       Level{Base: 99, Job: 70},
				PostRebirth: false, // regression; invalid
				Stats:       Stats{Str: 90, Agi: 90, Vit: 1, Int: 1, Dex: 30, Luk: 1},
			},
		},
	}
	err := traj.Validate()
	if err == nil || !strings.Contains(err.Error(), "regresses from post-rebirth") {
		t.Fatalf("expected post-rebirth regression error, got: %v", err)
	}
}

func TestSnapshot_Validate_NegativeLevel(t *testing.T) {
	s := Snapshot{Class: "novice", Level: Level{Base: -1, Job: 1}, Stats: Stats{}}
	err := s.Validate()
	if err == nil || !strings.Contains(err.Error(), "must be >= 1") {
		t.Fatalf("expected level-floor error, got: %v", err)
	}
}

// TestSnapshot_Validate_ZeroLevelRejected locks down the >= 1 floor for
// trajectory snapshots. Pre-renewal characters start at base 1, job 1;
// a 0/0 snapshot is junk that the shim would silently score as Novice
// defaults and let pass quality gates.
func TestSnapshot_Validate_ZeroLevelRejected(t *testing.T) {
	cases := []Level{
		{Base: 0, Job: 0},
		{Base: 0, Job: 50},
		{Base: 99, Job: 0},
	}
	for _, lv := range cases {
		s := Snapshot{Class: "novice", Level: lv, Stats: Stats{}}
		err := s.Validate()
		if err == nil || !strings.Contains(err.Error(), "must be >= 1") {
			t.Errorf("level %+v: expected >= 1 error, got: %v", lv, err)
		}
	}
}

func TestSnapshot_Validate_RefineOutOfRange(t *testing.T) {
	s := Snapshot{
		Class: "novice",
		Level: Level{Base: 1, Job: 1},
		Stats: Stats{},
		Equipment: map[SlotKey]EquipSpec{
			"rh": {Refine: 11},
		},
	}
	err := s.Validate()
	if err == nil || !strings.Contains(err.Error(), "refine 11 out of range") {
		t.Fatalf("expected refine-out-of-range error, got: %v", err)
	}
}

func TestSkillAlloc_Validate(t *testing.T) {
	cases := []struct {
		name string
		a    SkillAlloc
		want string
	}{
		{"zero id", SkillAlloc{ID: 0, Level: 1}, "must be > 0"},
		{"negative id", SkillAlloc{ID: -3, Level: 1}, "must be > 0"},
		{"zero level", SkillAlloc{ID: 13, Level: 0}, "must be >= 1"},
		{"negative level", SkillAlloc{ID: 13, Level: -2}, "must be >= 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSkillAlloc(tc.a)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got: %v", tc.want, err)
			}
		})
	}
}
