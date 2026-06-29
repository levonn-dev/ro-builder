package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/llm"
)

const lookupSkillSchema = `{
  "type": "object",
  "required": ["iro_id"],
  "properties": {
    "iro_id": {"type": "integer", "description": "iRO skill id (e.g. 2 for Sword Mastery / SM_SWORD, 28 for Heal / AL_HEAL, 90 for Storm Gust / WZ_STORMGUST)."}
  }
}`

type lookupSkillTool struct {
	cat *catalog.Catalog
}

// NewLookupSkill constructs the lookup_skill tool. The catalog is required;
// the API loads it from the embedded asset at startup so a nil here is a
// programming error, not a graceful-degradation case.
func NewLookupSkill(cat *catalog.Catalog) Tool {
	if cat == nil {
		panic("tools.NewLookupSkill: catalog is required")
	}
	return &lookupSkillTool{cat: cat}
}

func (l *lookupSkillTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "lookup_skill",
		Description: "Look up a single iRO skill by numeric id. Returns the canonical record: aegis name (script identifier like SM_SWORD), display name, max level, attack type (Weapon / Magic / Misc / passive), element when applicable, cast/cooldown metadata at MaxLevel (cast_time_ms, fixed_cast_ms, after_cast_ms, cooldown_ms; all milliseconds; interruptible bool indicates whether the cast can be cancelled by damage; cast_time_ms is the MaxLevel value -- cast time can scale with skill level for some skills (the bolts, Storm Gust, Meteor); use cast_time_by_level_ms[level-1] when present for the exact value at an allocated level), and status_change (the SC_X identifier of the status this skill applies; SC_FREEZE, SC_STUN, etc.; empty for skills that apply no status). " +
			"Use this to validate skill IDs and max levels before assembling Snapshot.Skills in submit_trajectory; avoids round-trips where score_build rejects a skill not in the active class's tree or level above the skill's cap. " +
			"Also use cast_time_ms / interruptible to check whether a build's primary skill needs uninterruptible-cast gear (Phen card, Orleans Gown); long interruptible casts without that gear get rejected by quality gates.",
		InputSchema: json.RawMessage(lookupSkillSchema),
	}
}

type lookupSkillInput struct {
	IRoID int `json:"iro_id"`
}

func (l *lookupSkillTool) Execute(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var in lookupSkillInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("decode lookup_skill input: %w", err)
	}
	skill, ok := l.cat.Skill(in.IRoID)
	if !ok {
		return nil, fmt.Errorf("iRO skill id %d not found in catalog", in.IRoID)
	}
	out, err := json.Marshal(skill)
	if err != nil {
		return nil, fmt.Errorf("encode lookup_skill output: %w", err)
	}
	return out, nil
}
