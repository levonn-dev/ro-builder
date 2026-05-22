package rathena

import (
	"os"
	"testing"
)

func TestLoadMobDB_PreRe(t *testing.T) {
	if _, err := os.Stat(rathenaRoot + "/db/pre-re/mob_db.yml"); err != nil {
		t.Skipf("rAthena not at %s; clone rathena/rathena at sibling path", rathenaRoot)
	}
	mobs, err := LoadMobDB(rathenaRoot, "pre-re")
	if err != nil {
		t.Fatal(err)
	}
	if len(mobs) < 100 {
		t.Fatalf("expected >100 mobs, got %d", len(mobs))
	}

	byID := make(map[int]Mob, len(mobs))
	for _, m := range mobs {
		byID[m.ID] = m
	}

	// Same canonical mobs the Hercules suite checks. Verifies rAthena and
	// Hercules agree on iRO id space + the AegisName→SpriteName /
	// JapaneseName→JName field-name normalization works.
	cases := []struct {
		id         int
		spriteName string
		name       string
		hp         int
	}{
		{id: 1002, spriteName: "PORING", name: "Poring", hp: 50},
		{id: 1115, spriteName: "EDDGA", name: "Eddga", hp: 152000},
	}
	for _, tc := range cases {
		got, ok := byID[tc.id]
		if !ok {
			t.Errorf("mob %d (%s) missing from rAthena pre-re", tc.id, tc.name)
			continue
		}
		if got.SpriteName != tc.spriteName {
			t.Errorf("mob %d SpriteName: got %q want %q", tc.id, got.SpriteName, tc.spriteName)
		}
		if got.Name != tc.name {
			t.Errorf("mob %d Name: got %q want %q", tc.id, got.Name, tc.name)
		}
		if tc.hp != 0 && got.Hp != tc.hp {
			t.Errorf("mob %d Hp: got %d want %d", tc.id, got.Hp, tc.hp)
		}
	}

	// Verify rAthena's bare-name normalization runs and produces the same
	// shape as Hercules. Poring is the cross-source reference point.
	poring := byID[1002]
	if poring.AtkMin != 7 || poring.AtkMax != 10 {
		t.Errorf("Poring Atk: got [%d, %d] want [7, 10]", poring.AtkMin, poring.AtkMax)
	}
	if poring.MDef != 5 {
		t.Errorf("Poring MDef: got %d want 5", poring.MDef)
	}
	if poring.Race != "RC_Plant" {
		t.Errorf("Poring Race: got %q want RC_Plant (post-normalization)", poring.Race)
	}
	if poring.Element != "Ele_Water" {
		t.Errorf("Poring Element: got %q want Ele_Water", poring.Element)
	}
	if poring.ElementLv != 1 {
		t.Errorf("Poring ElementLv: got %d want 1", poring.ElementLv)
	}
	if poring.Size != "Size_Medium" {
		t.Errorf("Poring Size: got %q want Size_Medium", poring.Size)
	}
}
