package hercules

import (
	"os"
	"testing"
)

const herculesSkillDBPath = "../../../../../Hercules/db/pre-re/skill_db.conf"

func requireHerculesSkills(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(herculesSkillDBPath); err != nil {
		t.Skipf("Hercules skill_db not at %s", herculesSkillDBPath)
	}
}

func TestLoadSkillDB_PreReKnownEntries(t *testing.T) {
	requireHerculesSkills(t)
	skills, err := LoadSkillDB(herculesSkillDBPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) < 100 {
		t.Fatalf("expected >100 skills in pre-re skill_db, got %d", len(skills))
	}

	byID := make(map[int]Skill, len(skills))
	for _, s := range skills {
		byID[s.ID] = s
	}

	cases := []Skill{
		// Basic Skill; every Novice's job-change gate. No AttackType.
		{ID: 1, AegisName: "NV_BASIC", Name: "Basic Skill", MaxLevel: 9},
		// Sword Mastery; Swordsman passive, +ATK with swords.
		{ID: 2, AegisName: "SM_SWORD", Name: "Sword Mastery", MaxLevel: 10, AttackType: "Misc"},
		// Two-Handed Sword Mastery.
		{ID: 3, AegisName: "SM_TWOHAND", Name: "Two-Handed Sword Mastery", MaxLevel: 10, AttackType: "Misc"},
	}
	for _, want := range cases {
		got, ok := byID[want.ID]
		if !ok {
			t.Errorf("skill %d (%s) missing", want.ID, want.AegisName)
			continue
		}
		if got != want {
			t.Errorf("skill %d mismatch:\nwant %+v\ngot  %+v", want.ID, want, got)
		}
	}
}

// Cast-bearing skills exercise the cast/cooldown extraction. Each case
// covers a distinct shape we have to handle correctly:
//
//   - Storm Gust (89): per-level CastTime object → resolve to MaxLevel
//     value (15000ms at Lv10), interruptible
//   - Magnus Exorcismus (79): flat CastTime (15000ms), interruptible
//   - Grand Cross (254): flat CastTime (2000ms), NOT interruptible
//     (Hercules omits InterruptCast → default false per skill_db.conf doc)
//   - Sonic Blow (136): no CastTime field at all → 0; AfterCastActDelay
//     present (2000ms); Interruptible irrelevant since no cast
//
// If these stop matching, either Hercules's pre-re skill_db has been
// updated upstream (re-verify against current source) or the parser
// regressed.
func TestLoadSkillDB_PreReCastMetadata(t *testing.T) {
	requireHerculesSkills(t)
	skills, err := LoadSkillDB(herculesSkillDBPath)
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[int]Skill, len(skills))
	for _, s := range skills {
		byID[s.ID] = s
	}

	cases := []struct {
		id    int
		name  string
		cast  int  // CastTimeMs
		after int  // AfterCastMs
		cd    int  // CooldownMs
		intr  bool // Interruptible
	}{
		{id: 89, name: "WZ_STORMGUST", cast: 15000, after: 5000, cd: 0, intr: true},
		{id: 79, name: "PR_MAGNUS", cast: 15000, after: 4000, cd: 0, intr: true},
		{id: 254, name: "CR_GRANDCROSS", cast: 2000, after: 1500, cd: 0, intr: false},
		{id: 136, name: "AS_SONICBLOW", cast: 0, after: 2000, cd: 0, intr: false},
	}
	for _, c := range cases {
		got, ok := byID[c.id]
		if !ok {
			t.Errorf("skill %d (%s) missing", c.id, c.name)
			continue
		}
		if got.AegisName != c.name {
			t.Errorf("skill %d AegisName: got %q want %q", c.id, got.AegisName, c.name)
		}
		if got.CastTimeMs != c.cast {
			t.Errorf("%s CastTimeMs: got %d want %d", c.name, got.CastTimeMs, c.cast)
		}
		if got.AfterCastMs != c.after {
			t.Errorf("%s AfterCastMs: got %d want %d", c.name, got.AfterCastMs, c.after)
		}
		if got.CooldownMs != c.cd {
			t.Errorf("%s CooldownMs: got %d want %d", c.name, got.CooldownMs, c.cd)
		}
		if got.Interruptible != c.intr {
			t.Errorf("%s Interruptible: got %v want %v", c.name, got.Interruptible, c.intr)
		}
	}
}

func TestIntAtMaxLevel(t *testing.T) {
	cases := []struct {
		name     string
		v        any
		maxLevel int
		want     int
	}{
		{"nil → 0", nil, 10, 0},
		{"flat int returns as-is", 2000, 10, 2000},
		{"per-level direct hit", map[string]any{"Lv1": 6000, "Lv10": 15000}, 10, 15000},
		{"per-level falls back to highest present", map[string]any{"Lv1": 100, "Lv5": 500}, 10, 500},
		{"empty map → 0", map[string]any{}, 10, 0},
		{"non-Lv keys are ignored", map[string]any{"X": 999, "Lv3": 300}, 10, 300},
		{"wrong type → 0", "2000", 10, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := intAtMaxLevel(c.v, c.maxLevel); got != c.want {
				t.Errorf("got %d want %d", got, c.want)
			}
		})
	}
}
