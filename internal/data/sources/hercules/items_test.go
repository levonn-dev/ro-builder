package hercules

import (
	"os"
	"reflect"
	"testing"
)

// Hercules lives outside the repo (user-installed). Tests skip cleanly when
// it isn't present so the suite stays runnable on machines without it.
const herculesPreRePath = "../../../../../Hercules/db/pre-re/item_db.conf"

func requireHercules(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(herculesPreRePath); err != nil {
		t.Skipf("Hercules db not at %s; clone HerculesWS/Hercules at sibling path", herculesPreRePath)
	}
}

func TestLoadItemDB_PreReKnownEntries(t *testing.T) {
	requireHercules(t)
	items, err := LoadItemDB(herculesPreRePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 1000 {
		t.Fatalf("expected >1000 items in pre-re item_db, got %d", len(items))
	}

	byID := make(map[int]Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}

	cases := []Item{
		// Knife; pre-re 3-slot dagger, the canonical weapon for our tests.
		{
			ID: 1201, AegisName: "Knife", Name: "Knife",
			Type: "IT_WEAPON", Atk: 17, Slots: 3, Weight: 400, Range: 1,
			Loc: "EQP_WEAPON", WeaponLv: 1, EquipLv: 1, Subtype: "W_DAGGER",
		},
		// Cotton Shirt; basic novice armor.
		{
			ID: 2301, AegisName: "Cotton_Shirt", Name: "Cotton Shirt",
			Type: "IT_ARMOR", Def: 1, Slots: 0, Weight: 100,
			Loc: "EQP_ARMOR", EquipLv: 0,
		},
		// Andre Card; +20 ATK on weapon.
		{
			ID: 4043, AegisName: "Andre_Card", Name: "Andre Card",
			Type: "IT_CARD", Weight: 10,
			Loc: "EQP_WEAPON",
		},
		// Soldier Skeleton Card; +9 CRI on weapon.
		{
			ID: 4086, AegisName: "Soldier_Skeleton_Card", Name: "Soldier Skeleton Card",
			Type: "IT_CARD", Weight: 10,
			Loc: "EQP_WEAPON",
		},
	}
	for _, want := range cases {
		got, ok := byID[want.ID]
		if !ok {
			t.Errorf("item %d (%s) missing", want.ID, want.Name)
			continue
		}
		// reflect.DeepEqual rather than == because Item now has a slice
		// field (GrantsImmunity) which makes the struct non-comparable.
		// The hand-authored overlay populates that field at catalog build
		// time, not via the parser, so for parser-only tests it must be nil.
		if !reflect.DeepEqual(got, want) {
			t.Errorf("item %d mismatch:\nwant %+v\ngot  %+v", want.ID, want, got)
		}
	}
}
