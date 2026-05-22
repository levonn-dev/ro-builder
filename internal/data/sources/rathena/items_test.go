package rathena

import (
	"os"
	"testing"
)

const rathenaRoot = "../../../../../rathena"

func requireRathena(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(rathenaRoot + "/db/pre-re/item_db_equip.yml"); err != nil {
		t.Skipf("rAthena not at %s; clone rathena/rathena at sibling path", rathenaRoot)
	}
}

func TestLoadItemDB_PreReKnownEntries(t *testing.T) {
	requireRathena(t)
	items, err := LoadItemDB(rathenaRoot, "pre-re")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 1000 {
		t.Fatalf("expected >1000 items in rAthena pre-re, got %d", len(items))
	}

	byID := make(map[int]Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}

	// Same canonical four items the Hercules suite spot-checks. Verifies
	// rAthena and Hercules agree on the iRO ID space; and that our YAML
	// parser produces the same Item shape Hercules's libconfig parser does.
	cases := []struct {
		id        int
		aegisName string
		typ       string
		atk       int
		def       int
		slots     int
	}{
		{id: 1201, aegisName: "Knife", typ: "IT_WEAPON", atk: 17, slots: 3},
		{id: 2301, aegisName: "Cotton_Shirt", typ: "IT_ARMOR", def: 1, slots: 0},
		{id: 4043, aegisName: "Andre_Card", typ: "IT_CARD"},
		{id: 4086, aegisName: "Soldier_Skeleton_Card", typ: "IT_CARD"},
	}
	for _, tc := range cases {
		got, ok := byID[tc.id]
		if !ok {
			t.Errorf("item %d (%s) missing from rAthena pre-re", tc.id, tc.aegisName)
			continue
		}
		if got.AegisName != tc.aegisName {
			t.Errorf("item %d AegisName: got %q, want %q", tc.id, got.AegisName, tc.aegisName)
		}
		if got.Type != tc.typ {
			t.Errorf("item %d Type: got %q, want %q (Hercules form)", tc.id, got.Type, tc.typ)
		}
		if tc.atk != 0 && got.Atk != tc.atk {
			t.Errorf("item %d Atk: got %d, want %d", tc.id, got.Atk, tc.atk)
		}
		if tc.def != 0 && got.Def != tc.def {
			t.Errorf("item %d Def: got %d, want %d", tc.id, got.Def, tc.def)
		}
		if tc.slots != 0 && got.Slots != tc.slots {
			t.Errorf("item %d Slots: got %d, want %d", tc.id, got.Slots, tc.slots)
		}
	}
}
