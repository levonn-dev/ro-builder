package gates

import (
	"github.com/levonn-dev/ro-builder/internal/data"
)

// targetThreats resolves the threat profile for the snapshot's scoring
// target. Looks up the calc shim's resolved enemy id (combat.enemy
// doesn't carry the id back, so we use the scenario target). Custom mobs
// from the active server profile take precedence over the base catalog;
// same overlay-first pattern lookup_monster uses.
func targetThreats(in Inputs) *data.MobThreats {
	if in.Scenario == nil || in.Scenario.Target == 0 {
		return nil
	}
	id := in.Scenario.Target
	if in.Profile != nil {
		if cm := in.Profile.CustomMob(id); cm != nil {
			return cm.Threats
		}
	}
	if in.Catalog != nil {
		if mob, ok := in.Catalog.Mob(id); ok {
			return mob.Threats
		}
	}
	return nil
}

// buildCovers reports whether the snapshot's stats + equipment grant
// full immunity to status. Pre-re formulas:
//
//	stun    ; total VIT >= 100, OR (VIT >= 97 AND LUK >= 15)
//	           (97+15 is "effective immunity" per the WoE class
//	           spreadsheet; chance is zero even though duration may
//	           not be; treat as full immunity for gate purposes)
//	sleep   ; total INT >= 100
//	confusion; only via Giearth-equivalent card; 200 INT immunity is
//	           impossible per the spreadsheet
//	stone_curse, frozen, curse, silence, blind, poison; equipment-only
//
// "Total" stats means base + job bonus; here we take the snapshot's
// allocated values directly (they already reflect the player's stat
// sheet). Buff stacking (Bless / Inc.AGI) is NOT counted; gate sees
// the unbuffed base since buffs aren't durable.
func buildCovers(in Inputs, status string) bool {
	if in.Snapshot == nil {
		return false
	}
	st := in.Snapshot.Stats
	switch status {
	case data.ImmunityStun:
		if st.Vit >= 100 || (st.Vit >= 97 && st.Luk >= 15) {
			return true
		}
	case data.ImmunitySleep:
		if st.Int >= 100 {
			return true
		}
	}
	return equipmentCovers(in, status)
}

// equipmentCovers scans every equipped slot; the slot's main item AND
// any cards slotted into it; for an immunity tag matching status.
// Returns true on the first match.
func equipmentCovers(in Inputs, status string) bool {
	return walkEquipment(in, func(id int) bool {
		return itemGrants(in.Catalog, id, status)
	})
}

func itemGrants(cat catalogItemLookup, id int, status string) bool {
	item, ok := cat.Item(id)
	if !ok {
		return false
	}
	for _, s := range item.GrantsImmunity {
		if s == status {
			return true
		}
	}
	return false
}

// equipmentGrantsUninterruptibleCast scans every equipped slot for
// grants_uninterruptible_cast. Used by the IUC gate.
func equipmentGrantsUninterruptibleCast(in Inputs) bool {
	return walkEquipment(in, func(id int) bool {
		item, ok := in.Catalog.Item(id)
		return ok && item.GrantsUninterruptibleCast
	})
}

// walkEquipment visits every equipped item id (slot main + cards) until
// pred returns true or the equipment is exhausted. Returns true on the
// first hit, false if pred never matched. Skips slots with id 0 (empty).
func walkEquipment(in Inputs, pred func(id int) bool) bool {
	if in.Snapshot == nil || in.Catalog == nil {
		return false
	}
	for _, equip := range in.Snapshot.Equipment {
		if equip.ID == 0 {
			continue
		}
		if pred(equip.ID) {
			return true
		}
		for _, cardID := range equip.Cards {
			if pred(cardID) {
				return true
			}
		}
	}
	return false
}

// catalogItemLookup is the small subset of catalog.Catalog the gate
// helpers need. Defined as an interface so unit tests can pass a fake.
type catalogItemLookup interface {
	Item(id int) (data.Item, bool)
}
