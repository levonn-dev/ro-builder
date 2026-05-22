package hercules

import (
	"fmt"
	"os"
)

// Item is one row of Hercules's item_db.conf, decoded into a typed struct.
// The fields covered here are the ones we currently need: identity (ID,
// AegisName, Name, Type), basic combat numbers (Atk, Def, Slots), and
// equipping metadata (Weight, Range, Loc, WeaponLv, EquipLv, Subtype).
//
// Hercules's item_db has many more fields; Job restrictions, Trade flags,
// Stack, Sprite, EquipLv ranges, multiple Script blocks. They're parsed by
// ParseFile but we don't decode them yet; add fields here and a line in
// itemFromMap when scoring or build validation needs them.
type Item struct {
	ID        int    `json:"id"`
	AegisName string `json:"aegis_name"`
	Name      string `json:"name"`
	Type      string `json:"type"` // "IT_WEAPON", "IT_ARMOR", "IT_CARD", "IT_HEALING", etc.
	Atk       int    `json:"atk,omitempty"`
	Def       int    `json:"def,omitempty"`
	Slots     int    `json:"slots,omitempty"`
	Weight    int    `json:"weight,omitempty"`
	Range     int    `json:"range,omitempty"`
	Loc       string `json:"loc,omitempty"`       // "EQP_WEAPON", "EQP_ARMOR", etc.; equipment slot
	WeaponLv  int    `json:"weapon_lv,omitempty"` // 1-4 for weapons, 0 otherwise
	EquipLv   int    `json:"equip_lv,omitempty"`  // minimum base level to equip (0 if unrestricted)
	Subtype   string `json:"subtype,omitempty"`   // "W_DAGGER", "W_1HSWORD", etc. for weapons; "A_SHIELD" etc. for shields

	// Hand-authored immunity / cast-resilience tags layered on at catalog
	// build time from internal/catalog/data/item_immunities.yaml. Not
	// derivable from emulator script bodies in any practical way, so we
	// annotate by hand.
	//
	// GrantsImmunity is the strict "this item makes you fully immune to
	// this status" set (Marc → frozen, Nightmare → sleep, etc.). Partial
	// resists like Diabolus Robe's "+50% stun resist" do NOT belong here;
	// they're handled by the gate logic via stat formulas / score breakdown,
	// not the binary tag.
	//
	// GrantsUninterruptibleCast tags Phen card / Orleans Gown / equivalent
	// gear that lets cast-time skills land through damage. The
	// uninterruptible_cast gate fires when any allocated skill has cast
	// time > 1s AND interruptible == true AND no equipped item carries
	// this flag.
	GrantsImmunity            []string `json:"grants_immunity,omitempty"`
	GrantsUninterruptibleCast bool     `json:"grants_uninterruptible_cast,omitempty"`
}

// LoadItemDB reads a Hercules item_db.conf file and returns one Item per
// entry. Caller chooses the path; typically db/pre-re/item_db.conf or
// db/re/item_db.conf depending on the active mode.
func LoadItemDB(path string) ([]Item, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	raw, err := ParseFile(src)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	items := make([]Item, 0, len(raw))
	for i, e := range raw {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s entry %d: expected object, got %T", path, i, e)
		}
		item, err := itemFromMap(m)
		if err != nil {
			return nil, fmt.Errorf("%s entry %d: %w", path, i, err)
		}
		items = append(items, item)
	}
	return items, nil
}

func itemFromMap(m map[string]any) (Item, error) {
	id, ok := m["Id"].(int)
	if !ok {
		return Item{}, fmt.Errorf("missing or non-int Id (got %T)", m["Id"])
	}
	item := Item{ID: id}
	item.AegisName, _ = m["AegisName"].(string)
	item.Name, _ = m["Name"].(string)
	item.Type, _ = m["Type"].(string)
	item.Loc = stringOrFirstArrayString(m["Loc"])
	item.Subtype, _ = m["Subtype"].(string)
	if v, ok := m["Atk"].(int); ok {
		item.Atk = v
	}
	if v, ok := m["Def"].(int); ok {
		item.Def = v
	}
	if v, ok := m["Slots"].(int); ok {
		item.Slots = v
	}
	if v, ok := m["Weight"].(int); ok {
		item.Weight = v
	}
	if v, ok := m["Range"].(int); ok {
		item.Range = v
	}
	if v, ok := m["WeaponLv"].(int); ok {
		item.WeaponLv = v
	}
	item.EquipLv = intOrFirstArrayInt(m["EquipLv"])
	return item, nil
}

// intOrFirstArrayInt handles Hercules's dual-form numeric fields: scalar
// int or [min, max] array. EquipLv is the canonical case; level-
// restricted items like Combo Battle Robe use the array form to express
// "equippable from level 55 onward."
func intOrFirstArrayInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case []any:
		if len(x) > 0 {
			if n, ok := x[0].(int); ok {
				return n
			}
		}
	}
	return 0
}

// stringOrFirstArrayString handles Hercules's dual-form string fields:
// scalar string or a string-array form (used for multi-slot equipment
// like dual mid+low headgear). For multi-slot, the first slot suffices
// for the catalog's equip-slot filter purposes.
func stringOrFirstArrayString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		if len(x) > 0 {
			if s, ok := x[0].(string); ok {
				return s
			}
		}
	}
	return ""
}
