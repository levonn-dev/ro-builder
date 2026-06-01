package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/llm"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

const SubmitTrajectoryToolName = "submit_trajectory"

// submitTrajectorySchema is the LLM-facing schema for the structured
// trajectory submission. The LLM produces this once at the end of its
// loop; the orchestrator extracts the parsed Trajectory from the
// conversation trace post-loop and runs canonical scoring on the
// designated checkpoint snapshots.
//
// Schema fields mirror domain.Trajectory / domain.Snapshot / domain.SkillAlloc.
// Score is intentionally omitted; the orchestrator stamps that on after
// canonical scoring. LevelingTarget uses the same {target: mob_id} shape
// as scoring's Scenario.
const submitTrajectorySchema = `{
  "type": "object",
  "required": ["primary"],
  "properties": {
    "primary": {
      "type": "object",
      "description": "The primary trajectory ending at the max-level endgame for the requested class. Earlier snapshots are forward-compatible states along the path.",
      "required": ["class", "snapshots"],
      "properties": {
        "class": {"type": "string", "description": "Endgame class key; e.g. 'assassin_cross', 'taekwon_kid'. Must match the last snapshot's class."},
        "snapshots": {
          "type": "array",
          "description": "Ordered checkpoints from earliest to endgame. Stat / skill points monotonically grow within a rebirth cycle (only transcending rebirth resets; class changes are additive). Score density expectation: include checkpoints at each job change, lvl 85 if reached, and the endgame; the orchestrator decides which to score.",
          "items": {"$ref": "#/$defs/snapshot"}
        }
      }
    },
    "alternatives": {
      "type": "array",
      "description": "Optional peer trajectories leading to the same endgame goal (e.g. 'level Sin to 99/50 before transcending' is a peer trajectory of the direct-trans path). Each entry has the same shape as primary.",
      "items": {
        "type": "object",
        "required": ["class", "snapshots"],
        "properties": {
          "class": {"type": "string"},
          "snapshots": {"type": "array", "items": {"$ref": "#/$defs/snapshot"}}
        }
      }
    }
  },
  "$defs": {
    "snapshot": {
      "type": "object",
      "required": ["class", "level", "stats"],
      "properties": {
        "class": {"type": "string", "description": "Class at this checkpoint; may differ from trajectory.class mid-path (e.g. 'high_thief' on the way to 'assassin_cross')."},
        "level": {
          "type": "object",
          "required": ["base", "job"],
          "properties": {
            "base": {"type": "integer"},
            "job": {"type": "integer"}
          }
        },
        "post_rebirth": {"type": "boolean", "description": "True for snapshots after transcending rebirth. Defaults to false. Monotonicity check only compares snapshots within the same flag."},
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
          "description": "Skill allocation at this checkpoint. id is iRO skill id; level is invested level (>= 1). Skills monotonically accumulate within a rebirth cycle.",
          "items": {
            "type": "object",
            "required": ["id", "level"],
            "properties": {
              "id": {"type": "integer"},
              "level": {"type": "integer"}
            }
          }
        },
        "equipment": {
          "type": "object",
          "description": "Recommended gear by slot key (weapon, headTop, headMid, headBot, shield, armor, garment, footgear, accessory1, accessory2). Same shape as score_build.",
          "additionalProperties": {
            "type": "object",
            "required": ["id"],
            "properties": {
              "id": {"type": "integer"},
              "refine": {"type": "integer"},
              "cards": {"type": "array", "items": {"type": "integer"}}
            }
          }
        },
        "leveling_target": {
          "type": "object",
          "description": "Combat-sim target for this checkpoint. For leveling snapshots: the primary mob on the recommended map (use server profile's leveling_content quests). For the endgame snapshot: the mob the build is optimized to kill (from the request's description field).",
          "properties": {
            "target": {"type": "integer", "description": "iRO mob id."}
          }
        },
        "notes": {"type": "string", "description": "Short prose annotation for this checkpoint; e.g. 'class change to Assassin', 'transcend here', 'lvl 85 milestone', 'endgame'. Helps the orchestrator and the user understand why this checkpoint matters."}
      }
    }
  }
}`

// SubmitTrajectoryInput mirrors what submitTrajectorySchema describes.
// Used by the orchestrator's trace-extraction logic to parse the LLM's
// tool_use args back into typed Go structs.
type SubmitTrajectoryInput struct {
	Primary      domain.Trajectory   `json:"primary"`
	Alternatives []domain.Trajectory `json:"alternatives,omitempty"`
}

// ScoringClient is the subset of *scoring.Client the tool needs.
// Mirrors orchestrator.ScoringClient; duplicated here to avoid the
// tools→orchestrator import cycle.
type ScoringClient interface {
	Score(ctx context.Context, req *scoring.ScoreRequest) (*scoring.ScoreResponse, error)
}

// SubmitTrajectoryDeps wires per-request dependencies the tool needs
// for inline scoring + gate evaluation. The orchestrator constructs
// these once per Generate call, closing EvaluateGates over the request's
// catalog + profile + overrides + playstyle, and closing Accept over
// the per-request Session.
type SubmitTrajectoryDeps struct {
	Catalog *catalog.Catalog
	Scoring ScoringClient
	// Profile is the resolved server profile for the request. When non-nil,
	// scoreAndGate forwards it to ToScoreRequest so custom-mob overlays
	// (e.g. UARO's OGH ids 2464–2476) are resolved correctly. Task 8's
	// orchestrator wiring populates this; nil is safe (custom mobs fall
	// back to base-catalog stats, same as the old nil-profile path).
	Profile *domain.ServerProfile
	// EvaluateGates runs the gates evaluator against one snapshot's
	// score result. nil disables gate evaluation entirely (the tool
	// then accepts whatever validates structurally; useful in tests
	// that don't want to wire gates).
	EvaluateGates func(score *scoring.ScoreResponse, snap *domain.Snapshot) []domain.GateResult
	// Accept is called with the (canonical-scored + gates-stamped)
	// trajectories when all hard gates pass. It returns the session's
	// locked state under first-clean-wins: true once a warning-free
	// submission is the final answer of record, false while the result is
	// provisional (carries a warning) and a cleaner resubmit could still
	// replace it. Provisional submissions are kept, not ignored.
	Accept func(primary domain.Trajectory, alternatives []domain.Trajectory, calcVersion string) bool
}

type submitTrajectoryTool struct {
	deps SubmitTrajectoryDeps
}

// NewSubmitTrajectory constructs the tool. The deps struct carries the
// catalog (item-name echo), scoring client, gate evaluator closure, and
// session accept callback. All fields are optional; nil Scoring disables
// inline scoring; nil EvaluateGates disables gate evaluation; nil Accept
// skips the callback; nil Catalog disables the equipment echo.
func NewSubmitTrajectory(deps SubmitTrajectoryDeps) Tool {
	return &submitTrajectoryTool{deps: deps}
}

func (s *submitTrajectoryTool) Definition() llm.Tool {
	return llm.Tool{
		Name: SubmitTrajectoryToolName,
		Description: "Submit your final trajectory for the build request. The orchestrator runs canonical scoring + quality-gates evaluation inside this tool; it is the authoritative scorer and the commit step. Confirm each checkpoint's stat budget with score_build first, then submit; this tool reports the combat gates (flee / EHP / status immunity / hit) per checkpoint on rejection, so fix those from its result rather than pre-scoring them. Once it accepts you don't need to re-score. " +
			"On an accepted submission the result's checkpoints array reports each scored snapshot's statPointsRemaining and any non-pass gate (e.g. an under-spent stat budget, a warning). Acceptance is first-clean-wins: a warning-free submission locks in as final (locked=true); a submission carrying warnings is provisional (locked=false) and a cleaner resubmit replaces it. On a rejected submission the result is an error naming the failing gate(s) and the offending checkpoint's statPointsRemaining, so you can correct the allocation and resubmit. " +
			"Alternatives are independent peer trajectories; any alternative that fails gates is silently dropped from the accepted set (and reported in dropped_alternatives) without rejecting the submission. " +
			"Provide the primary trajectory (max-level endgame for the requested class, with backwards-derived checkpoints at job changes, lvl 85 if reached, and the endgame). " +
			"Optionally include peer alternative trajectories that lead to the same endgame goal via a different path. " +
			"The tool result echoes back the resolved catalog name for every equipment / card ID you submitted; the catalog is the only source of truth for item names. Prefer catching a wrong id before submitting (take ids from search_items); if the result is still provisional (locked=false) you can resubmit with the corrected id to replace it. " +
			"The response's locked field is true when the result is final (a warning-free submission has landed); when false the result is provisional and you may resubmit a cleaner build to replace it.",
		InputSchema: json.RawMessage(submitTrajectorySchema),
	}
}

func (s *submitTrajectoryTool) Execute(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	in, err := ParseSubmitTrajectory(raw)
	if err != nil {
		return nil, err
	}
	if err := in.Primary.Validate(); err != nil {
		return nil, fmt.Errorf("primary trajectory invalid: %w", err)
	}
	for i := range in.Alternatives {
		if err := in.Alternatives[i].Validate(); err != nil {
			return nil, fmt.Errorf("alternative[%d] invalid: %w", i, err)
		}
	}

	// Canonical scoring on the trajectory's SelectScoredIndices,
	// stamping Score + Gates on each scored snapshot in place.
	calcVersion, err := s.scoreAndGate(ctx, &in.Primary)
	if err != nil {
		return nil, err
	}

	// Per-checkpoint budget + gate report for the primary. Built once and
	// reused: it enriches the rejection error (so the model sees the
	// offending statPointsRemaining inline) and rides along on the accept
	// ack (so the model sees under-spend warnings without a separate
	// score_build pass). This is what lets the orchestrator own scoring
	// end-to-end; the model assembles, submits, and reads these numbers.
	reports := buildCheckpointReports(&in.Primary)

	// Primary must clear every gate; a fail / fail_hard short-circuits
	// with a Go error so the registry wraps it as is_error=true and the
	// LLM iterates. Alternatives are independent peer trajectories: any
	// alternative that fails gates is dropped from the accepted set,
	// not propagated as a submission-level failure.
	if msg := scanGates("primary", in.Primary); msg != "" {
		return nil, fmt.Errorf("submission rejected; %s.\n%sAdjust the flagged snapshots and submit_trajectory again", msg, formatCheckpointLines(reports))
	}
	acceptedAlts := make([]domain.Trajectory, 0, len(in.Alternatives))
	droppedAlts := make([]droppedAlternative, 0)
	for i := range in.Alternatives {
		if _, err := s.scoreAndGate(ctx, &in.Alternatives[i]); err != nil {
			// 4xx from the sidecar means this alternative's input is bad
			// (unmapped item id, bad slot, etc.); treat like a gate failure
			// and drop it rather than aborting the whole submission. 5xx or
			// transport errors propagate: they'd fail the primary too.
			if scoring.IsClientError(err) {
				droppedAlts = append(droppedAlts, droppedAlternative{Index: i, Reason: "scoring: " + err.Error()})
				continue
			}
			return nil, err
		}
		if msg := scanGates(fmt.Sprintf("alternative[%d]", i), in.Alternatives[i]); msg != "" {
			droppedAlts = append(droppedAlts, droppedAlternative{Index: i, Reason: msg})
			continue
		}
		acceptedAlts = append(acceptedAlts, in.Alternatives[i])
	}

	// Forward to the session callback. Acceptance is first-clean-wins,
	// enforced by the callback, not the tool, so we call it on every
	// successful execute. The callback's bool return is the session's
	// locked state: true once a warning-free submission is the final
	// answer of record, false while the result is provisional (carries a
	// warning) and a cleaner resubmit could still replace it. Surfaced as
	// ack.Locked so the model knows whether to stop or refine.
	locked := true
	if s.deps.Accept != nil {
		locked = s.deps.Accept(in.Primary, acceptedAlts, calcVersion)
	}

	ack := submitTrajectoryAck{
		Accepted:            true,
		Locked:              locked,
		PrimarySnapshots:    len(in.Primary.Snapshots),
		Alternatives:        len(acceptedAlts),
		DroppedAlternatives: droppedAlts,
		Checkpoints:         reports,
	}
	if s.deps.Catalog != nil {
		ack.ResolvedEquipment = s.resolveEquipment(in)
		if len(ack.ResolvedEquipment) > 0 {
			ack.VerificationInstructions = "Cross-check every item and card name above against your intent. If any name is unexpected, the iRO id is wrong; fix it (use lookup_item) and, while the result is provisional (locked=false), submit_trajectory again with corrected ids."
		}
	}
	return json.Marshal(ack)
}

// scoreAndGate runs canonical scoring + gate evaluation on the
// snapshots selected by SelectScoredIndices, stamping each snapshot's
// Score and Gates in place. Sidecar errors short-circuit with a Go
// error; gates eval results stay on the snapshot.
//
// Returns the calc_version from the first successful score (used by
// the orchestrator to stamp saved_trajectories.calc_version).
func (s *submitTrajectoryTool) scoreAndGate(ctx context.Context, t *domain.Trajectory) (string, error) {
	if s.deps.Scoring == nil {
		return "", nil
	}
	var calcVersion string
	for _, idx := range SelectScoredIndices(t) {
		snap := &t.Snapshots[idx]
		req := snap.ToScoreRequest(s.deps.Profile)
		resp, err := s.deps.Scoring.Score(ctx, req)
		if err != nil {
			return "", fmt.Errorf("canonical scoring failed for snapshot %d (%s): %w", idx, snap.Class, err)
		}
		snap.Score = resp
		if calcVersion == "" && resp.CalcVersion != "" {
			calcVersion = resp.CalcVersion
		}
		if s.deps.EvaluateGates != nil {
			snap.Gates = s.deps.EvaluateGates(resp, snap)
		}
	}
	return calcVersion, nil
}

// SelectScoredIndices applies the score-density rule to a trajectory:
// score the endgame (last snapshot), every class-change /
// rebirth-boundary snapshot, and any snapshot at base level 85.
// Exported so the orchestrator (Task 8) can use the same rule.
func SelectScoredIndices(t *domain.Trajectory) []int {
	if t == nil || len(t.Snapshots) == 0 {
		return nil
	}
	seen := make(map[int]bool)
	seen[len(t.Snapshots)-1] = true
	for i, snap := range t.Snapshots {
		if snap.Level.Base == 85 {
			seen[i] = true
		}
		if i == 0 {
			continue
		}
		prev := t.Snapshots[i-1]
		if snap.Class != prev.Class || snap.PostRebirth != prev.PostRebirth {
			seen[i] = true
		}
	}
	out := make([]int, 0, len(seen))
	for i := range seen {
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

func scanGates(label string, t domain.Trajectory) string {
	for i, snap := range t.Snapshots {
		for _, g := range snap.Gates {
			if g.IsBlocking() {
				return fmt.Sprintf("%s snapshot %d (%s): gate %q %s (actual=%v, threshold=%v)",
					label, i, snap.Class, g.Name, g.Severity, g.Actual, g.Threshold)
			}
		}
	}
	return ""
}

// levelStr renders a level as "base/job" (e.g. "99/70") for the LLM-facing
// echoes. One helper so the checkpoint report and the resolved-equipment
// block in this file can't render the same level differently.
func levelStr(l domain.Level) string {
	return fmt.Sprintf("%d/%d", l.Base, l.Job)
}

// checkpointReport is the per-snapshot scoring summary echoed back to the
// model on every submit outcome. statPointsRemaining is the headline
// number (negative = over-allocated → rejection; > underspendWarn = wasted
// budget → warn); Gates carries only the non-pass results so the echo
// stays actionable.
type checkpointReport struct {
	SnapshotIndex       int                 `json:"snapshot_index"`
	Class               string              `json:"class"`
	Level               string              `json:"level"`
	StatPointsRemaining int                 `json:"stat_points_remaining"`
	Gates               []domain.GateResult `json:"gates,omitempty"`
}

// buildCheckpointReports summarizes the canonically-scored snapshots
// (SelectScoredIndices) for the model. Snapshots without a stamped Score
// (scoring disabled, or not selected) are skipped, so an empty result
// means "nothing was scored" rather than "everything passed".
func buildCheckpointReports(t *domain.Trajectory) []checkpointReport {
	var out []checkpointReport
	for _, idx := range SelectScoredIndices(t) {
		snap := &t.Snapshots[idx]
		if snap.Score == nil {
			continue
		}
		r := checkpointReport{
			SnapshotIndex:       idx,
			Class:               snap.Class,
			Level:               levelStr(snap.Level),
			StatPointsRemaining: snap.Score.Derived.StatPointsRemaining,
		}
		for _, g := range snap.Gates {
			if !g.IsPass() {
				r.Gates = append(r.Gates, g)
			}
		}
		out = append(out, r)
	}
	return out
}

// formatCheckpointLines renders the reports as a prose block for the
// rejection error message. Empty in, empty out (no trailing noise when
// scoring was disabled).
func formatCheckpointLines(reports []checkpointReport) string {
	if len(reports) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Per-checkpoint scoring:\n")
	for _, r := range reports {
		fmt.Fprintf(&sb, "- snapshot %d (%s) %s: statPointsRemaining=%d", r.SnapshotIndex, r.Class, r.Level, r.StatPointsRemaining)
		for _, g := range r.Gates {
			fmt.Fprintf(&sb, " [%s %s: %s]", strings.ToUpper(g.Severity), g.Name, g.Reason)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// === Catalog-name echo (unchanged from prior implementation) ===

type submitTrajectoryAck struct {
	Accepted bool `json:"accepted"`
	// Locked is true when the result is final: a warning-free submission
	// is the answer of record under first-clean-wins. False means the
	// result is provisional (carries a warning, e.g. an under-spent
	// budget) and a cleaner resubmit will replace it. The LLM treats true
	// as "stop" and false as "you may refine and resubmit".
	Locked           bool `json:"locked"`
	PrimarySnapshots int  `json:"primary_snapshots"`
	Alternatives     int  `json:"alternatives"`
	// Checkpoints reports each canonically-scored snapshot's
	// statPointsRemaining and any non-pass gate. Present on every accepted
	// submission so the model can spot an under-spent budget (a warn, not a
	// rejection) and resubmit without re-running score_build itself.
	Checkpoints              []checkpointReport   `json:"checkpoints,omitempty"`
	DroppedAlternatives      []droppedAlternative `json:"dropped_alternatives,omitempty"`
	ResolvedEquipment        []resolvedSnapshotEq `json:"resolved_equipment,omitempty"`
	VerificationInstructions string               `json:"verification_instructions,omitempty"`
}

// droppedAlternative tells the LLM which peer trajectories were rejected
// for gate failures (and why) without failing the whole submission. The
// primary is still accepted; the LLM can choose to resubmit with fixed
// alternatives on a later iteration if it cares.
type droppedAlternative struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
}

type resolvedSnapshotEq struct {
	Trajectory    string                       `json:"trajectory"`     // "primary" or "alternative[i]"
	SnapshotIndex int                          `json:"snapshot_index"` // index within that trajectory's Snapshots
	Class         string                       `json:"class"`
	Level         string                       `json:"level"` // "base/job"
	Slots         map[string]resolvedSlotEntry `json:"slots,omitempty"`
}

type resolvedSlotEntry struct {
	Item  string   `json:"item"`            // "<name> [<id>]" or "#<id> NOT FOUND IN CATALOG"
	Cards []string `json:"cards,omitempty"` // same format, one per card slot
}

func (s *submitTrajectoryTool) resolveEquipment(in *SubmitTrajectoryInput) []resolvedSnapshotEq {
	var out []resolvedSnapshotEq
	out = append(out, s.resolveTrajectory("primary", in.Primary)...)
	for i, alt := range in.Alternatives {
		out = append(out, s.resolveTrajectory(fmt.Sprintf("alternative[%d]", i), alt)...)
	}
	return out
}

func (s *submitTrajectoryTool) resolveTrajectory(label string, t domain.Trajectory) []resolvedSnapshotEq {
	var out []resolvedSnapshotEq
	for i, snap := range t.Snapshots {
		if len(snap.Equipment) == 0 {
			continue
		}
		entry := resolvedSnapshotEq{
			Trajectory:    label,
			SnapshotIndex: i,
			Class:         snap.Class,
			Level:         levelStr(snap.Level),
			Slots:         make(map[string]resolvedSlotEntry, len(snap.Equipment)),
		}
		// Sort slot keys so the LLM sees a stable ordering.
		keys := make([]string, 0, len(snap.Equipment))
		for k := range snap.Equipment {
			keys = append(keys, string(k))
		}
		sort.Strings(keys)
		for _, k := range keys {
			spec := snap.Equipment[domain.SlotKey(k)]
			if spec.ID == 0 {
				continue
			}
			re := resolvedSlotEntry{Item: s.formatItem(spec.ID)}
			for _, cid := range spec.Cards {
				if cid == 0 {
					continue
				}
				re.Cards = append(re.Cards, s.formatItem(cid))
			}
			entry.Slots[k] = re
		}
		out = append(out, entry)
	}
	return out
}

func (s *submitTrajectoryTool) formatItem(id int) string {
	if item, ok := s.deps.Catalog.Item(id); ok {
		return fmt.Sprintf("%s [%d]", item.Name, id)
	}
	return fmt.Sprintf("#%d NOT FOUND IN CATALOG", id)
}

// ParseSubmitTrajectory decodes the LLM's tool_use input. Exposed so
// downstream callers (orchestrator trace inspection, tests) can re-
// parse the same args without re-invoking the tool.
func ParseSubmitTrajectory(raw json.RawMessage) (*SubmitTrajectoryInput, error) {
	var in SubmitTrajectoryInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("decode submit_trajectory input: %w", err)
	}
	return &in, nil
}
