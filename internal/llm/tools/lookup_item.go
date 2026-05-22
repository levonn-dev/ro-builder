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
			"Use this to verify an item exists and fits the slot you intend BEFORE passing it to score_build (avoids round-trips for 'no rocalc mapping' or 'not in option list'), and to check whether equipment covers the immunity / uninterruptible-cast a build needs vs the target's threats.",
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
