package hercules

import (
	"os"
	"testing"
)

const mobDBPath = "../../../../../Hercules/db/pre-re/mob_db.conf"

func TestLoadMobDB_PreRe(t *testing.T) {
	if _, err := os.Stat(mobDBPath); err != nil {
		t.Skipf("Hercules mob_db not at %s; clone HerculesWS/Hercules at sibling path", mobDBPath)
	}
	mobs, err := LoadMobDB(mobDBPath)
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

	// Spot-check canonical iRO mob ids the build-time mapping tool joins
	// on. Poring is the universal "did anything load?" check; Eddga (1115)
	// and Golden Thief Bug (1086) are the MVPs the shim test exercises for
	// sentinel-string handling.
	cases := []struct {
		id   int
		name string
		hp   int
	}{
		{id: 1002, name: "Poring", hp: 50},
		{id: 1115, name: "Eddga", hp: 152000},
		{id: 1086, name: "Golden Thief Bug"},
	}
	for _, tc := range cases {
		got, ok := byID[tc.id]
		if !ok {
			t.Errorf("mob %d (%s) missing from pre-re", tc.id, tc.name)
			continue
		}
		if got.Name != tc.name {
			t.Errorf("mob %d Name: got %q want %q", tc.id, got.Name, tc.name)
		}
		if tc.hp != 0 && got.Hp != tc.hp {
			t.Errorf("mob %d Hp: got %d want %d", tc.id, got.Hp, tc.hp)
		}
	}
}

// Verify the extended combat-stat fields parse correctly. Poring is a
// stable reference: lvl 1, 50 HP, attack 7..10, no defense, 5 mdef,
// Plant/Water/Medium. If any of these regress, inline-stats scoring
// against custom mobs will produce wrong numbers without an obvious
// failure mode; the assertion catches the parse bug at the source.
func TestLoadMobDB_ExtendedCombatStats(t *testing.T) {
	if _, err := os.Stat(mobDBPath); err != nil {
		t.Skipf("Hercules mob_db not at %s", mobDBPath)
	}
	mobs, err := LoadMobDB(mobDBPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range mobs {
		if m.ID != 1002 {
			continue
		}
		if m.AtkMin != 7 || m.AtkMax != 10 {
			t.Errorf("Poring Atk: got [%d, %d] want [7, 10]", m.AtkMin, m.AtkMax)
		}
		if m.Def != 0 {
			t.Errorf("Poring Def: got %d want 0", m.Def)
		}
		if m.MDef != 5 {
			t.Errorf("Poring MDef: got %d want 5", m.MDef)
		}
		if m.Race != "RC_Plant" {
			t.Errorf("Poring Race: got %q want RC_Plant", m.Race)
		}
		if m.Element != "Ele_Water" {
			t.Errorf("Poring Element: got %q want Ele_Water", m.Element)
		}
		if m.ElementLv != 1 {
			t.Errorf("Poring ElementLv: got %d want 1", m.ElementLv)
		}
		if m.Size != "Size_Medium" {
			t.Errorf("Poring Size: got %q want Size_Medium", m.Size)
		}
		return
	}
	t.Fatal("Poring (1002) not found")
}
