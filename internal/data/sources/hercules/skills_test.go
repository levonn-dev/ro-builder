package hercules

import (
	"os"
	"reflect"
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
		if !reflect.DeepEqual(got, want) {
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

func TestCastTimeByLevel_BoltScales(t *testing.T) {
	m := map[string]any{
		"Id": 14, "Name": "MG_COLDBOLT", "Description": "Cold Bolt", "MaxLevel": 10,
		"CastTime": map[string]any{
			"Lv1": 700, "Lv2": 1400, "Lv3": 2100, "Lv4": 2800, "Lv5": 3500,
			"Lv6": 4200, "Lv7": 4900, "Lv8": 5600, "Lv9": 6300, "Lv10": 7000,
		},
	}
	s, err := skillFromMap(m)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{700, 1400, 2100, 2800, 3500, 4200, 4900, 5600, 6300, 7000}
	if got := s.CastTimeByLevelMs; len(got) != 10 || got[3] != 2800 || got[9] != 7000 {
		t.Fatalf("CastTimeByLevelMs = %v, want %v", got, want)
	}
	if got := s.CastAtLevelMs(4); got != 2800 {
		t.Errorf("CastAtLevelMs(4) = %d, want 2800", got)
	}
	if got := s.CastAtLevelMs(10); got != 7000 {
		t.Errorf("CastAtLevelMs(10) = %d, want 7000", got)
	}
}

func TestCastTimeByLevel_FlatIsNil(t *testing.T) {
	m := map[string]any{
		"Id": 254, "Name": "CR_GRANDCROSS", "Description": "Grand Cross",
		"MaxLevel": 10, "CastTime": 2000,
	}
	s, err := skillFromMap(m)
	if err != nil {
		t.Fatal(err)
	}
	if s.CastTimeByLevelMs != nil {
		t.Errorf("flat-cast skill should have nil CastTimeByLevelMs, got %v", s.CastTimeByLevelMs)
	}
	if got := s.CastAtLevelMs(3); got != 2000 {
		t.Errorf("CastAtLevelMs(3) on flat skill = %d, want 2000 (scalar fallback)", got)
	}
}

func TestIntSliceByLevel_EmptyMapIsNil(t *testing.T) {
	if got := intSliceByLevel(map[string]any{}, 10); got != nil {
		t.Errorf("empty grouped object should yield nil, got %v", got)
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
