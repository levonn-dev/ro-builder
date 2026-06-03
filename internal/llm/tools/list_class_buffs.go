package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/llm"
)

const listClassBuffsSchema = `{
  "type": "object",
  "required": ["class"],
  "properties": {
    "class": {"type": "string", "description": "Class name in shim form (e.g. \"taekwon_kid\"). Same value the API request's class field uses."}
  }
}`

type listClassBuffsTool struct {
	cat *catalog.Catalog
}

// NewListClassBuffs constructs the list_class_buffs tool. Source is the
// catalog's ClassBuffs (the class's allocatable skills that carry self-buff
// metadata from skill_buffs.yaml). The buff's level comes from the anchor
// skill's allocation, so the model declares only name (+ element for endows).
func NewListClassBuffs(cat *catalog.Catalog) Tool {
	if cat == nil {
		panic("tools.NewListClassBuffs: catalog is required")
	}
	return &listClassBuffsTool{cat: cat}
}

func (t *listClassBuffsTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "list_class_buffs",
		Description: "Enumerate the self-buffs the given class can activate (e.g. Taekwon: Mild Wind, Spurt, Taekwon Ranker), with the anchor skill, kind (weapon_endow/stat_buff/status), persistence, and (for endow buffs) the element options unlocked by level. " +
			"Declare active buffs per snapshot via submit_trajectory's active_buffs. Buff level is taken from the anchor skill's allocation; only the element is chosen for endow buffs. " +
			"Fallback only: the user prompt already injects this class's available buffs, so call this only when that block is absent.",
		InputSchema: json.RawMessage(listClassBuffsSchema),
	}
}

type listClassBuffsInput struct {
	Class string `json:"class"`
}

type listClassBuffsEntry struct {
	Name          string   `json:"name"`
	AnchorSkill   string   `json:"anchor_skill"`
	AnchorSkillID int      `json:"anchor_skill_id"`
	Kind          string   `json:"kind"`
	Persistence   string   `json:"persistence"`
	EndowElements []string `json:"endow_elements,omitempty"`
}

func (t *listClassBuffsTool) Execute(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var in listClassBuffsInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("decode list_class_buffs input: %w", err)
	}
	if in.Class == "" {
		return nil, fmt.Errorf("class is required")
	}
	buffs, ok := t.cat.ClassBuffs(in.Class)
	if !ok {
		return nil, fmt.Errorf("class %q not found in catalog", in.Class)
	}
	out := make([]listClassBuffsEntry, 0, len(buffs))
	for _, b := range buffs {
		e := listClassBuffsEntry{
			Name:          b.SelfBuff.Name,
			AnchorSkill:   b.AegisName,
			AnchorSkillID: b.ID,
			Kind:          b.SelfBuff.Kind,
			Persistence:   b.SelfBuff.Persistence,
		}
		if b.SelfBuff.Endow != nil {
			e.EndowElements = b.SelfBuff.Endow.Elements
		}
		out = append(out, e)
	}
	body, err := json.Marshal(map[string]any{"class": in.Class, "buffs": out})
	if err != nil {
		return nil, fmt.Errorf("encode list_class_buffs output: %w", err)
	}
	return body, nil
}
