package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/llm"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// scoreBuildSchema is the JSON Schema the LLM sees for score_build's
// input. Mirrors api.scoreRequestBody but emitted as a static schema so
// the LLM has explicit structural guidance; without this, models try to
// invent field names ("stats.strength", "weapon.id" vs "weapon"."id").
//
// Kept as a string literal rather than reflected from Go types because
// the schema gates the model's output: drift between Go struct tags and
// the schema is exactly what causes the model to send unmarshalable JSON.
// One source of truth, hand-curated.
const scoreBuildSchema = `{
  "type": "object",
  "required": ["build"],
  "properties": {
    "build": {
      "type": "object",
      "description": "The character setup to score.",
      "required": ["class"],
      "properties": {
        "class": {"type": "string", "description": "Class name. Common values: novice, swordman, magician, archer, acolyte, merchant, thief, knight, priest, wizard, blacksmith, hunter, assassin, crusader, monk, sage, rogue, alchemist, bard, dancer, super_novice, gunslinger, ninja, taekwon_kid, star_gladiator, soul_linker. Aliases like 'mage' (= magician) and 'swordsman' (= swordman) are accepted; case and separator (space/hyphen/underscore) are normalized. Omit or empty = Novice (default)."},
        "level": {
          "type": "object",
          "properties": {
            "base": {"type": "integer", "description": "Base level, 1-99 in pre-renewal."},
            "job": {"type": "integer", "description": "Job level. Cap depends on class."}
          }
        },
        "stats": {
          "type": "object",
          "properties": {
            "str": {"type": "integer"},
            "agi": {"type": "integer"},
            "vit": {"type": "integer"},
            "int": {"type": "integer"},
            "dex": {"type": "integer"},
            "luk": {"type": "integer"}
          }
        },
        "skills": {
          "type": "array",
          "description": "Allocated skills for this build. iRO skill ids; the calc shim expects the full allocation, not a delta. Skills not in the active class's tree are silently skipped by the shim (auto-attack-tier numbers result), so include only skills you confirmed via list_class_skills or lookup_skill.",
          "items": {
            "type": "object",
            "required": ["id", "level"],
            "properties": {
              "id": {"type": "integer", "description": "iRO skill id (Gravity-canonical; same in iRO/Hercules/rAthena/rocalc)."},
              "level": {"type": "integer", "description": "Skill level, 1..max (max from list_class_skills)."}
            }
          }
        },
        "equipment": {
          "type": "object",
          "description": "Map of slot key (weapon, headTop, headMid, headBot, shield, armor, garment, footgear, accessory1, accessory2) to equipment spec.",
          "additionalProperties": {
            "type": "object",
            "required": ["id"],
            "properties": {
              "id": {"type": "integer", "description": "iRO item id."},
              "refine": {"type": "integer", "description": "Refinement level 0-10."},
              "cards": {"type": "array", "items": {"type": "integer"}, "description": "iRO card ids slotted into this item."}
            }
          }
        }
      }
    },
    "scenario": {
      "type": "object",
      "description": "Target encounter for the combat sim. Omit to get derived stats only.",
      "properties": {
        "target": {"type": "integer", "description": "iRO mob id (Poring=1002, Eddga=1115)."}
      }
    }
  }
}`

// scoreBuildTool wraps scoring.Client as an LLM-callable tool. The tool's
// input mirrors api/score's request shape: { build: domain.Build,
// scenario?: domain.Scenario }. The output is the calc sidecar's full
// scoring.ScoreResponse: derived stats always, combat sim when scenario
// is set.
type scoreBuildTool struct {
	client *scoring.Client
}

// NewScoreBuild constructs the tool. The orchestrator wires this in at
// startup with a shared scoring.Client. Panics on nil client to match
// the fail-fast convention of every other tool constructor in this
// package; a nil client would otherwise NPE inside Execute on the first
// real build, far from the wiring bug.
func NewScoreBuild(client *scoring.Client) Tool {
	if client == nil {
		panic("tools.NewScoreBuild: scoring client is required")
	}
	return &scoreBuildTool{client: client}
}

func (s *scoreBuildTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "score_build",
		Description: "Score a Ragnarok Online (pre-renewal) character build against an optional target mob. " +
			"Returns derived stats (HIT, FLEE, ATK, MATK, DEF, MDEF, ASPD, MaxHP, MaxSP) and, when a scenario " +
			"target is provided, full combat-sim output (damage min/ave/max, hit/flee/dodge rates, time-to-kill). " +
			"Use this to evaluate any candidate build; never compute stat numbers yourself.",
		InputSchema: json.RawMessage(scoreBuildSchema),
	}
}

// scoreBuildInput matches the JSON schema above. Skills are kept as a
// sibling to Build (rather than nested inside) because domain.Build has
// no Skills field; Snapshot does. Lifting Skills here lets the LLM's
// exploration scoring see the same skills the canonical Snapshot scoring
// uses, closing the gap left by the trajectory redesign where Build
// flowed through the calc but skills did not.
type scoreBuildInput struct {
	Build    scoreBuildBuild  `json:"build"`
	Scenario *domain.Scenario `json:"scenario,omitempty"`
}

// scoreBuildBuild explicitly enumerates the calc-relevant Build fields
// the LLM may set. Mode / Server / Tier are deliberately omitted;
// they're orchestrator metadata stamped by the API boundary, and an LLM
// that smuggles "mode": "renewal" through the tool schema would bypass
// Validate's pre-renewal check (it runs on a value built here, not on
// the raw input). Field shapes match domain.Build verbatim so the JSON
// surface the model sees is unchanged from the previous embedded form.
type scoreBuildBuild struct {
	Class     string                              `json:"class,omitempty"`
	Level     domain.Level                        `json:"level"`
	Stats     domain.Stats                        `json:"stats"`
	Equipment map[domain.SlotKey]domain.EquipSpec `json:"equipment,omitempty"`
	Skills    []domain.SkillAlloc                 `json:"skills,omitempty"`
}

// toBuild copies the LLM-supplied fields into a domain.Build. Mode is
// fixed to PreRenewal here; every score_build call is implicitly
// pre-renewal because the API rejects renewal at the boundary, and the
// orchestrator's per-request mode never leaks into this tool.
func (b scoreBuildBuild) toBuild() domain.Build {
	return domain.Build{
		Mode:      domain.PreRenewal,
		Class:     b.Class,
		Level:     b.Level,
		Stats:     b.Stats,
		Equipment: b.Equipment,
	}
}

func (s *scoreBuildTool) Execute(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var in scoreBuildInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("decode score_build input: %w", err)
	}
	build := in.Build.toBuild()
	if err := build.Validate(); err != nil {
		return nil, fmt.Errorf("invalid build: %w", err)
	}

	// Active server profile, if present, swaps custom-mob targets into the
	// inline-stats path so the calc shim doesn't try to look them up in
	// rocalc's m_Monster table. Nil profile means no overlay; every
	// scenario target passes through as a stock iRO id.
	profile := domain.ServerProfileFromContext(ctx)
	// ToScoreRequest can't see scoreBuildBuild.Skills (it's a method on
	// domain.Build, no Skills field there), so we set req.Skills explicitly
	// after the conversion.
	req := build.ToScoreRequest(in.Scenario, profile)
	if len(in.Build.Skills) > 0 {
		req.Skills = in.Build.Skills
	}
	resp, err := s.client.Score(ctx, req)
	if err != nil {
		// Sidecar 4xx errors are caller-correctable (unknown slot, bad
		// iRO id); let those propagate as Go errors and the orchestrator
		// turns them into tool_result is_error blocks the LLM can read.
		if sErr, ok := errors.AsType[*scoring.Error](err); ok {
			return nil, fmt.Errorf("calc sidecar: %s (HTTP %d)", sErr.Message, sErr.Status)
		}
		return nil, fmt.Errorf("calc sidecar: %w", err)
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("encode score_build output: %w", err)
	}
	return out, nil
}
