package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/llm"
)

const searchItemsSchema = `{
  "type": "object",
  "properties": {
    "type": {"type": "string", "description": "Item category. Common values: IT_WEAPON, IT_ARMOR, IT_CARD, IT_HEALING, IT_USABLE, IT_AMMO. Omit to search across all types."},
    "subtype": {"type": "string", "description": "Refines weapon / shield category. Weapon subtypes: W_DAGGER, W_1HSWORD, W_2HSWORD, W_1HSPEAR, W_2HSPEAR, W_1HAXE, W_2HAXE, W_MACE, W_2HMACE, W_STAFF, W_BOW, W_KNUCKLE, W_MUSICAL, W_WHIP, W_BOOK, W_KATAR, W_REVOLVER, W_RIFLE, W_GATLING, W_SHOTGUN, W_GRENADE, W_HUUMA, W_2HSTAFF. Armor subtype: A_SHIELD."},
    "name_contains": {"type": "string", "description": "Case-insensitive substring filter on the item's display name."},
    "min_slots": {"type": "integer", "description": "Minimum card slot count (e.g. 3 for slotted-only weapons). 0 = ignore."},
    "max_slots": {"type": "integer", "description": "Maximum card slot count. 0 = ignore."},
    "max_equip_lv": {"type": "integer", "description": "Filter to items requiring at most this base level to equip (e.g. 50 for 'gear available at lvl 50'). 0 = ignore."},
    "limit": {"type": "integer", "description": "Max results to return. Default 20, hard cap 100. Use a smaller number when you have a specific filter and want a focused list."}
  }
}`

type searchItemsTool struct {
	cat *catalog.Catalog
}

func NewSearchItems(cat *catalog.Catalog) Tool {
	if cat == nil {
		panic("tools.NewSearchItems: catalog is required")
	}
	return &searchItemsTool{cat: cat}
}

func (s *searchItemsTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "search_items",
		Description: "Find iRO items matching filters. Combine type / subtype / name_contains / slot-count / max-equip-level to discover specific gear. " +
			"Use this when you need to PICK a piece of equipment for a build; e.g. 'find slotted daggers under level 60' or 'list shields with at least 1 slot'. " +
			"Returns a list of item records (same shape as lookup_item). Filters are AND-combined; omit filters that don't apply.",
		InputSchema: json.RawMessage(searchItemsSchema),
	}
}

type searchItemsInput struct {
	Type         string `json:"type"`
	Subtype      string `json:"subtype"`
	NameContains string `json:"name_contains"`
	MinSlots     int    `json:"min_slots"`
	MaxSlots     int    `json:"max_slots"`
	MaxEquipLv   int    `json:"max_equip_lv"`
	Limit        int    `json:"limit"`
}

func (s *searchItemsTool) Execute(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var in searchItemsInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("decode search_items input: %w", err)
	}
	results := s.cat.SearchItems(catalog.ItemFilter{
		Type:         in.Type,
		Subtype:      in.Subtype,
		NameContains: in.NameContains,
		MinSlots:     in.MinSlots,
		MaxSlots:     in.MaxSlots,
		MaxEquipLv:   in.MaxEquipLv,
		Limit:        in.Limit,
	})
	// Wrap so callers see the count alongside items; useful when a search
	// hits the limit cap and the LLM needs to know there are more.
	out, err := json.Marshal(struct {
		Count int `json:"count"`
		Items any `json:"items"`
	}{
		Count: len(results),
		Items: results,
	})
	if err != nil {
		return nil, fmt.Errorf("encode search_items output: %w", err)
	}
	return out, nil
}
