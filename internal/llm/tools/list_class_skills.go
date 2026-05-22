package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/llm"
)

const listClassSkillsSchema = `{
  "type": "object",
  "required": ["class"],
  "properties": {
    "class": {"type": "string", "description": "Class name in shim form (e.g. \"taekwon_kid\", \"knight\", \"high_wizard\"). Same value the API request's class field uses."}
  }
}`

type listClassSkillsTool struct {
	cat *catalog.Catalog
}

// NewListClassSkills constructs the list_class_skills tool. Source is
// Hercules's skill_tree.conf (loaded into the catalog at build time,
// inherits flattened); the player's allocatable skill tree. Different
// from rocalc's m_JobBuff (which is the calc's stat-effect-skill set
// and is the wrong list for allocation).
func NewListClassSkills(cat *catalog.Catalog) Tool {
	if cat == nil {
		panic("tools.NewListClassSkills: catalog is required")
	}
	return &listClassSkillsTool{cat: cat}
}

func (t *listClassSkillsTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "list_class_skills",
		Description: "Enumerate every skill the given class can allocate, with iRO id, aegis name, display name, max level, and cast/cooldown metadata. " +
			"Source: Hercules's skill_tree.conf with inherits flattened; Novice's skills appear under every class that inherits from it. " +
			"Use this once at the start of building a snapshot's Skills allocation; avoids the spam of guessing iRO IDs and calling lookup_skill repeatedly. " +
			"Note: the calc engine (rocalc) silently drops skills it can't model (e.g. Taekwon kicks). Score numbers fall back to auto-attack-tier in those cases; that's expected behavior, not an error to fix by changing skill choice.",
		InputSchema: json.RawMessage(listClassSkillsSchema),
	}
}

type listClassSkillsInput struct {
	Class string `json:"class"`
}

type listClassSkillsEntry struct {
	ID            int    `json:"id"`
	AegisName     string `json:"aegis_name"`
	Name          string `json:"name,omitempty"`
	MaxLevel      int    `json:"max_level"`
	AttackType    string `json:"attack_type,omitempty"`
	Element       string `json:"element,omitempty"`
	CastTimeMs    int    `json:"cast_time_ms,omitempty"`
	CooldownMs    int    `json:"cooldown_ms,omitempty"`
	Interruptible bool   `json:"interruptible,omitempty"`
}

func (t *listClassSkillsTool) Execute(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var in listClassSkillsInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("decode list_class_skills input: %w", err)
	}
	if in.Class == "" {
		return nil, fmt.Errorf("class is required")
	}

	skills, ok := t.cat.ClassSkills(in.Class)
	if !ok {
		return nil, fmt.Errorf("class %q not found in catalog", in.Class)
	}

	out := make([]listClassSkillsEntry, 0, len(skills))
	for _, s := range skills {
		out = append(out, listClassSkillsEntry{
			ID:            s.ID,
			AegisName:     s.AegisName,
			Name:          s.Name,
			MaxLevel:      s.MaxLevel,
			AttackType:    s.AttackType,
			Element:       s.Element,
			CastTimeMs:    s.CastTimeMs,
			CooldownMs:    s.CooldownMs,
			Interruptible: s.Interruptible,
		})
	}

	// Echo the caller-supplied class name so the LLM sees the same key it
	// asked for in subsequent tool calls. The catalog's canonical name
	// (e.g. "taekwon" for an alias like "taekwon_kid") is an internal
	// detail.
	body, err := json.Marshal(map[string]any{
		"class":  in.Class,
		"skills": out,
	})
	if err != nil {
		return nil, fmt.Errorf("encode list_class_skills output: %w", err)
	}
	return body, nil
}
