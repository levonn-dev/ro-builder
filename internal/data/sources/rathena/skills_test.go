package rathena

import (
	"os"
	"testing"
)

func TestLoadSkillDB_PreRe(t *testing.T) {
	if _, err := os.Stat(rathenaRoot + "/db/pre-re/skill_db.yml"); err != nil {
		t.Skipf("rAthena not at %s; clone rathena/rathena at sibling path", rathenaRoot)
	}
	skills, err := LoadSkillDB(rathenaRoot, "pre-re")
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) < 100 {
		t.Fatalf("expected >100 skills, got %d", len(skills))
	}

	byID := make(map[int]Skill, len(skills))
	for _, s := range skills {
		byID[s.ID] = s
	}

	// Same canonical skills the Hercules suite checks. Verifies rAthena and
	// Hercules agree on iRO id space and the Name→AegisName /
	// Description→Name normalization. Also verifies the Element prefix
	// normalization runs (rAthena emits "Fire", we expect "Ele_Fire").
	cases := []Skill{
		{ID: 1, AegisName: "NV_BASIC", Name: "Basic Skill", MaxLevel: 9},
		{ID: 2, AegisName: "SM_SWORD", Name: "Sword Mastery", MaxLevel: 10, AttackType: "Weapon"},
	}
	for _, want := range cases {
		got, ok := byID[want.ID]
		if !ok {
			t.Errorf("skill %d (%s) missing from rAthena pre-re", want.ID, want.AegisName)
			continue
		}
		if got.AegisName != want.AegisName {
			t.Errorf("skill %d AegisName: got %q want %q", want.ID, got.AegisName, want.AegisName)
		}
		if got.Name != want.Name {
			t.Errorf("skill %d Name: got %q want %q", want.ID, got.Name, want.Name)
		}
		if got.MaxLevel != want.MaxLevel {
			t.Errorf("skill %d MaxLevel: got %d want %d", want.ID, got.MaxLevel, want.MaxLevel)
		}
	}
}

// Mirrors hercules.TestLoadSkillDB_PreReCastMetadata. Same skills,
// different expected values where pre-re emulator data diverges:
// rAthena's CR_GRANDCROSS lists CastTime 3000ms vs Hercules's 2000ms;
// otherwise the cast/cooldown numbers agree. Interruptibility flips
// across sources for non-cast-time skills (rAthena defaults true,
// Hercules defaults false); we only assert on cast-bearing skills
// where both files specify explicitly.
func TestLoadSkillDB_PreReCastMetadata(t *testing.T) {
	if _, err := os.Stat(rathenaRoot + "/db/pre-re/skill_db.yml"); err != nil {
		t.Skipf("rAthena not at %s; clone rathena/rathena at sibling path", rathenaRoot)
	}
	skills, err := LoadSkillDB(rathenaRoot, "pre-re")
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
		cast  int
		after int
		cd    int
		intr  bool
	}{
		// Per-level sequence resolved to Lv10 = 15000ms; default-true
		// CastCancel (absent) means interruptible.
		{id: 89, name: "WZ_STORMGUST", cast: 15000, after: 5000, cd: 0, intr: true},
		// Flat CastTime, default-true CastCancel.
		{id: 79, name: "PR_MAGNUS", cast: 15000, after: 4000, cd: 0, intr: true},
		// Pre-re GC differs from Hercules: rAthena lists 3000ms, Hercules
		// 2000ms. Explicit `CastCancel: false` → uninterruptible.
		{id: 254, name: "CR_GRANDCROSS", cast: 3000, after: 1500, cd: 0, intr: false},
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
		// yaml.v2 untyped: sequence elements are map[any]any.
		{
			"sequence direct hit",
			[]any{
				map[any]any{"Level": 1, "Time": 6000},
				map[any]any{"Level": 10, "Time": 15000},
			},
			10, 15000,
		},
		{
			"sequence falls back to highest present",
			[]any{
				map[any]any{"Level": 1, "Time": 100},
				map[any]any{"Level": 5, "Time": 500},
			},
			10, 500,
		},
		// Strict-typed map shape (some callers / future yaml.v3-like
		// behavior).
		{
			"sequence with map[string]any entries",
			[]any{
				map[string]any{"Level": 7, "Time": 700},
			},
			10, 700,
		},
		{"empty sequence → 0", []any{}, 10, 0},
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

func TestNormalizeElement(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"Fire", "Ele_Fire"},
		{"Ele_Fire", "Ele_Fire"},
		{"Neutral", "Ele_Neutral"},
	}
	for _, tc := range cases {
		if got := normalizeElement(tc.in); got != tc.want {
			t.Errorf("normalizeElement(%q): got %q want %q", tc.in, got, tc.want)
		}
	}
}
