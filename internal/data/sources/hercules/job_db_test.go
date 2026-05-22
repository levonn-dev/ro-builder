package hercules

import (
	"os"
	"path/filepath"
	"testing"
)

// herculesPreReRoot returns the pre-renewal db path for tests, skipping
// the test entirely when no Hercules clone is present at the conventional
// sibling location. Catalog tests run against the embedded JSON; these
// parser tests run only when raw upstream data is available.
func herculesPreReRoot(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{
		"../../../../../Hercules/db/pre-re",
		"../../../../Hercules/db/pre-re",
	} {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}
	t.Skip("Hercules clone not present at ../Hercules; skipping parser test")
	return ""
}

func TestLoadJobDB_KnownEntries(t *testing.T) {
	root := herculesPreReRoot(t)
	refs, err := LoadJobDB(filepath.Join(root, "job_db.conf"))
	if err != nil {
		t.Fatalf("LoadJobDB: %v", err)
	}
	// Spot-check a Novice (1st-job baseline) and Assassin_Cross
	// (trans 2nd, the gate user pinned the budget on).
	novice, ok := refs["Novice"]
	if !ok {
		t.Fatal("Novice missing from job_db parse")
	}
	if novice.BaseExpGroup != "FirstClasses" || novice.JobExpGroup != "Novice" {
		t.Errorf("Novice: got base=%q job=%q, want FirstClasses/Novice",
			novice.BaseExpGroup, novice.JobExpGroup)
	}
	sinx, ok := refs["Assassin_Cross"]
	if !ok {
		t.Fatal("Assassin_Cross missing from job_db parse")
	}
	if sinx.BaseExpGroup != "TranscendedClasses" || sinx.JobExpGroup != "TranscendedSecondClasses" {
		t.Errorf("Assassin_Cross: got base=%q job=%q, want TranscendedClasses/TranscendedSecondClasses",
			sinx.BaseExpGroup, sinx.JobExpGroup)
	}
}

func TestLoadExpGroupDB_KnownGroups(t *testing.T) {
	root := herculesPreReRoot(t)
	groups, err := LoadExpGroupDB(filepath.Join(root, "exp_group_db.conf"))
	if err != nil {
		t.Fatalf("LoadExpGroupDB: %v", err)
	}
	cases := []struct {
		bucket string
		group  string
		want   int
	}{
		{"base", "FirstClasses", 99},
		{"base", "TranscendedClasses", 99},
		{"job", "Novice", 10},
		{"job", "FirstClasses", 50},
		{"job", "SecondClasses", 50},
		{"job", "HighNovice", 10},
		{"job", "TranscendedFirstClasses", 50},
		{"job", "TranscendedSecondClasses", 70},
	}
	for _, tc := range cases {
		var m map[string]int
		if tc.bucket == "base" {
			m = groups.Base
		} else {
			m = groups.Job
		}
		got, ok := m[tc.group]
		if !ok {
			t.Errorf("%s_exp_group %q: missing", tc.bucket, tc.group)
			continue
		}
		if got != tc.want {
			t.Errorf("%s_exp_group %q MaxLevel: got %d, want %d", tc.bucket, tc.group, got, tc.want)
		}
	}
}

func TestLoadJobMaxLevelsDB_JoinedNumbers(t *testing.T) {
	root := herculesPreReRoot(t)
	out, err := LoadJobMaxLevelsDB(
		filepath.Join(root, "job_db.conf"),
		filepath.Join(root, "exp_group_db.conf"),
	)
	if err != nil {
		t.Fatalf("LoadJobMaxLevelsDB: %v", err)
	}
	byName := make(map[string]JobMaxLevels, len(out))
	for _, j := range out {
		byName[j.Name] = j
	}
	cases := []struct {
		name              string
		wantBase, wantJob int
	}{
		{"Novice", 99, 10},
		{"Swordsman", 99, 50},
		{"Knight", 99, 50},
		{"Assassin", 99, 50},
		{"Thief_High", 99, 50},
		{"Novice_High", 99, 10},
		{"Assassin_Cross", 99, 70},
		{"Lord_Knight", 99, 70},
		{"High_Wizard", 99, 70},
		{"Super_Novice", 99, 99},
	}
	for _, tc := range cases {
		j, ok := byName[tc.name]
		if !ok {
			t.Errorf("%s: missing from joined result", tc.name)
			continue
		}
		if j.MaxBaseLevel != tc.wantBase || j.MaxJobLevel != tc.wantJob {
			t.Errorf("%s: got base=%d job=%d, want base=%d job=%d",
				tc.name, j.MaxBaseLevel, j.MaxJobLevel, tc.wantBase, tc.wantJob)
		}
	}
}
