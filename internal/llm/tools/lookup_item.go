package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/llm"
)

const lookupItemSchema = `{
  "type": "object",
  "required": ["iro_id"],
  "properties": {
    "iro_id": {"type": "integer", "description": "iRO item id (e.g. 1201 for Knife, 2301 for Cotton Shirt, 4043 for Andre Card)."}
  }
}`

type lookupItemTool struct {
	cat *catalog.Catalog
}

// NewLookupItem constructs the lookup_item tool. The catalog is required;
// the API loads it from the embedded asset at startup so a nil here is a
// programming error, not a graceful-degradation case.
func NewLookupItem(cat *catalog.Catalog) Tool {
	if cat == nil {
		panic("tools.NewLookupItem: catalog is required")
	}
	return &lookupItemTool{cat: cat}
}

func (l *lookupItemTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "lookup_item",
		Description: "Look up a single iRO item by numeric id. Returns the canonical record: name, type (IT_WEAPON / IT_ARMOR / IT_CARD / etc.), subtype (W_DAGGER / A_SHIELD / etc.), atk/def, slot count, equip-level requirement, weapon level, and immunity tags. " +
			"Immunity tags: grants_immunity (list of statuses the item grants FULL immunity to; frozen / stun / sleep / stone_curse / curse / silence / blind / poison / confusion) and grants_uninterruptible_cast (Phen card / Orleans Gown / equivalent). " +
			"Prefer search_items when choosing gear: its results are full records of this same shape, so you rarely need lookup_item for an item you found through search. Reach for lookup_item when you have a specific id from memory (not from a search result) and need to confirm its name / slot before use, or to inspect one item's immunity and uninterruptible-cast tags against the target's threats.",
		InputSchema: json.RawMessage(lookupItemSchema),
	}
}

type lookupItemInput struct {
	IRoID int `json:"iro_id"`
}

func (l *lookupItemTool) Execute(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var in lookupItemInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("decode lookup_item input: %w", err)
	}
	item, ok := l.cat.Item(in.IRoID)
	if !ok {
		return nil, fmt.Errorf("iRO item id %d not found in catalog", in.IRoID)
	}
	out, err := json.Marshal(item)
	if err != nil {
		return nil, fmt.Errorf("encode lookup_item output: %w", err)
	}
	return out, nil
}
