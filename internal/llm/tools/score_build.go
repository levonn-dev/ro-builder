package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/levonn-dev/ro-builder/internal/catalog"
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
// buildObjectSchema is the JSON Schema for one build. Shared by score_build
// and score_builds (via string composition) so the build shape the two
// tools accept can't drift apart.
const buildObjectSchema = `{
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
              "id": {"type": "integer", "description": "iRO skill id (Gravity-canonical; same in iRO/Hercules/rAthena)."},
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
        },
        "active_buffs": {
          "type": "array",
          "description": "Self-buffs active for this build (e.g. Taekwon Ranker, Mild Wind). name is a semantic buff key from this class's available buffs (see list_class_buffs); element (lowercase, e.g. \"holy\") is required only for weapon-endow buffs and must be within the buff's level. Buff level comes from the anchor skill's allocation in skills; do not restate it. Only declare buffs whose anchor skill this build allocates. Scored the same way submit_trajectory scores them.",
          "items": {
            "type": "object",
            "required": ["name"],
            "properties": {
              "name": {"type": "string"},
              "element": {"type": "string"}
            }
          }
        },
        "scored_skills": {
          "type": "array",
          "description": "Attack skills to score for this build. Each name is a scoreable attack skill from this class, tagged [scoreable as scored_skills: NAME] in the injected class-skills block (e.g. tornado_kick, roundhouse). Exactly one must have primary=true: its damage drives the combat gates (EHP/TTK/flee/hit); the rest are reported as a per-skill breakdown. Skill level comes from the allocation in skills; only declare skills this build allocates. Omit to score auto-attack.",
          "items": {
            "type": "object",
            "required": ["name"],
            "properties": {
              "name": {"type": "string"},
              "primary": {"type": "boolean"}
            }
          }
        }
      }
    }`

// scenarioObjectSchema is the shared combat-sim target schema.
const scenarioObjectSchema = `{
      "type": "object",
      "description": "Target encounter for the combat sim. Omit to get derived stats only.",
      "properties": {
        "target": {"type": "integer", "description": "iRO mob id (Poring=1002, Eddga=1115)."}
      }
    }`

const scoreBuildSchema = `{
  "type": "object",
  "required": ["build"],
  "properties": {
    "build": ` + buildObjectSchema + `,
    "scenario": ` + scenarioObjectSchema + `
  }
}`

// scoreBuildTool wraps scoring.Client as an LLM-callable tool. The tool's
// input mirrors api/score's request shape: { build: domain.Build,
// scenario?: domain.Scenario }. The output is the calc sidecar's full
// scoring.ScoreResponse: derived stats always, combat sim when scenario
// is set.
type scoreBuildTool struct {
	client *scoring.Client
	cat    *catalog.Catalog
}

// NewScoreBuild constructs the tool. The orchestrator wires this in at
// startup with a shared scoring.Client. Panics on nil client to match
// the fail-fast convention of every other tool constructor in this
// package; a nil client would otherwise NPE inside Execute on the first
// real build, far from the wiring bug. cat may be nil; it is only needed to
// resolve a build's active_buffs, and a build without buffs scores fine
// without it.
func NewScoreBuild(client *scoring.Client, cat *catalog.Catalog) Tool {
	if client == nil {
		panic("tools.NewScoreBuild: scoring client is required")
	}
	return &scoreBuildTool{client: client, cat: cat}
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
	Class        string                              `json:"class,omitempty"`
	Level        domain.Level                        `json:"level"`
	Stats        domain.Stats                        `json:"stats"`
	Equipment    map[domain.SlotKey]domain.EquipSpec `json:"equipment,omitempty"`
	Skills       []domain.SkillAlloc                 `json:"skills,omitempty"`
	ActiveBuffs  []domain.ActiveBuff                 `json:"active_buffs,omitempty"`
	ScoredSkills []domain.ScoredSkill                `json:"scored_skills,omitempty"`
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
	resp, err := scoreOne(ctx, s.client, s.cat, in.Build, in.Scenario)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("encode score_build output: %w", err)
	}
	return out, nil
}

// errBuildInvalid tags a build that failed domain validation before any
// sidecar call. score_builds uses errors.Is to classify it as a
// per-candidate error (one malformed build doesn't abort the batch), while
// genuine infrastructure failures (sidecar 5xx / transport) propagate.
var errBuildInvalid = errors.New("invalid build")

// scoreOne validates one build, converts it to a score request, and calls
// the sidecar. Shared by score_build and score_builds so a single build is
// scored identically whichever tool the model reaches for.
//
// The active server profile, if present, swaps custom-mob targets into the
// inline-stats path so the calc shim doesn't look them up in its own
// mob table; nil profile means no overlay. ToScoreRequest can't see
// scoreBuildBuild.Skills (domain.Build has no Skills field), so skills are
// set on the request explicitly after conversion.
//
// Errors preserve their type in the chain: a validation failure (or a bad
// active_buff or scored_skill) wraps errBuildInvalid, and a sidecar failure wraps the typed
// *scoring.Error (or transport error). This lets score_builds tell a
// candidate's bad input (validation, 4xx) apart from a global calc-engine
// outage (5xx, transport) and decide whether to capture it per-candidate or
// abort the batch.
//
// active_buffs and scored_skills are resolved here (each level filled from the
// build's skill allocation, buff elements validated, exactly one scored skill
// primary) so a previewed build is scored with the same buffs and attack skills
// canonical submit_trajectory scoring applies; cat must be non-nil when the
// build declares either.
func scoreOne(ctx context.Context, client *scoring.Client, cat *catalog.Catalog, b scoreBuildBuild, scenario *domain.Scenario) (*scoring.ScoreResponse, error) {
	build := b.toBuild()
	if err := build.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", errBuildInvalid, err)
	}
	profile := domain.ServerProfileFromContext(ctx)
	req := build.ToScoreRequest(scenario, profile)
	if len(b.Skills) > 0 {
		req.Skills = b.Skills
	}
	buffs, err := resolveBuffs(b.Class, b.Skills, b.ActiveBuffs, cat)
	if err != nil {
		// A bad buff is the caller's input mistake, like a malformed build, so
		// tag it errBuildInvalid for score_builds' per-candidate classification.
		return nil, fmt.Errorf("%w: %w", errBuildInvalid, err)
	}
	req.Buffs = buffs
	attackSkills, err := resolveAttackSkills(b.Class, b.Skills, b.ScoredSkills, cat)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errBuildInvalid, err)
	}
	req.AttackSkills = attackSkills
	resp, err := client.Score(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("calc sidecar: %w", err)
	}
	return resp, nil
}
