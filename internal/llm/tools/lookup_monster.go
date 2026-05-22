package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/llm"
)

const lookupMonsterSchema = `{
  "type": "object",
  "required": ["iro_id"],
  "properties": {
    "iro_id": {"type": "integer", "description": "iRO mob id (e.g. 1002 for Poring, 1115 for Eddga)."}
  }
}`

type lookupMonsterTool struct {
	cat *catalog.Catalog
}

func NewLookupMonster(cat *catalog.Catalog) Tool {
	if cat == nil {
		panic("tools.NewLookupMonster: catalog is required")
	}
	return &lookupMonsterTool{cat: cat}
}

func (l *lookupMonsterTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "lookup_monster",
		Description: "Look up an iRO monster by numeric id. Returns level, HP, attack range, def/mdef, race, element, size, and threats (when annotated); useful for picking an appropriate scenario target before passing it to score_build. " +
			"Threats: applies_status (statuses the mob can inflict; frozen / stun / sleep / stone_curse / curse / silence / blind / poison / confusion) and casts_skills (aegis-named skills the mob casts). The status_immunity gate consults applies_status to decide which immunities a build needs to clear. Mobs without a threats block are unannotated, not threat-free. " +
			"Use this to verify a target exists and is appropriate for the build's level (low-level builds shouldn't be scored against MVPs), and to check what immunities / element resists the build will need. " +
			"For servers with custom mobs (overlay), this returns the server's overlay version when an id is overridden.",
		InputSchema: json.RawMessage(lookupMonsterSchema),
	}
}

type lookupMonsterInput struct {
	IRoID int `json:"iro_id"`
}

func (l *lookupMonsterTool) Execute(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var in lookupMonsterInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("decode lookup_monster input: %w", err)
	}
	// Overlay-first: if the active server profile defines or re-stats this
	// id, return that. Otherwise fall through to the base catalog. The
	// base catalog stays untouched across all servers.
	if profile := domain.ServerProfileFromContext(ctx); profile != nil {
		if cm := profile.CustomMob(in.IRoID); cm != nil {
			out, err := json.Marshal(cm)
			if err != nil {
				return nil, fmt.Errorf("encode lookup_monster output: %w", err)
			}
			return out, nil
		}
	}
	mob, ok := l.cat.Mob(in.IRoID)
	if !ok {
		return nil, fmt.Errorf("iRO mob id %d not found in catalog", in.IRoID)
	}
	out, err := json.Marshal(mob)
	if err != nil {
		return nil, fmt.Errorf("encode lookup_monster output: %w", err)
	}
	return out, nil
}
