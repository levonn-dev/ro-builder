package domain

import (
	"errors"
	"fmt"

	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// Trajectory is the output of /generate: an ordered list of character
// states ending at the max-level endgame. Earlier snapshots are
// forward-compatible states along the path; stat / skill points
// monotonically grow within a rebirth cycle. Snapshot.PostRebirth marks
// the cycle boundary; monotonicity is checked per-cycle, not across.
//
// Class is the endgame class key (e.g., "assassin_cross"). Mid-trajectory
// snapshots may carry a different Snapshot.Class (e.g., "high_thief")
// reflecting the player's class at that level. Only the last snapshot is
// required to match Trajectory.Class.
//
// Score density is set by the orchestrator, not by this type: only major
// checkpoints (job changes, lvl 85, endgame) carry a non-nil Snapshot.Score.
type Trajectory struct {
	Class     string     `json:"class"`
	Snapshots []Snapshot `json:"snapshots"`
}

// Clean reports whether every gate on every snapshot passed: no warnings
// and no failures. A trajectory with no gates evaluated (scoring disabled)
// counts as clean. Drives the orchestrator's first-clean-wins lock: a clean
// submission is final, a warned one stays provisional and replaceable.
func (t Trajectory) Clean() bool {
	for i := range t.Snapshots {
		for _, g := range t.Snapshots[i].Gates {
			if !g.IsPass() {
				return false
			}
		}
	}
	return true
}

// Snapshot is one character state along a Trajectory.
//
// Class is the player's class at this point; may differ from
// Trajectory.Class mid-trajectory (e.g., "high_thief" on the way to
// "assassin_cross").
//
// PostRebirth marks the post-transcend cycle. Pre-renewal trans resets
// stats and skills with a stat-point bonus baked into the new allocation.
// Validation only compares snapshots within the same PostRebirth flag;
// the rebirth boundary is the only place stats / skills are allowed to
// step backwards (or rather, restart from a fresh allocation).
//
// LevelingTarget names the map / mob the player levels at for this
// checkpoint; populated by the orchestrator from the server profile's
// LevelingContent section. Score is populated only at scored
// checkpoints (job changes, lvl 85, endgame); other snapshots leave it nil.
type Snapshot struct {
	Class          string                 `json:"class"`
	Level          Level                  `json:"level"`
	PostRebirth    bool                   `json:"post_rebirth,omitempty"`
	Stats          Stats                  `json:"stats"`
	Skills         []SkillAlloc           `json:"skills,omitempty"`
	ActiveBuffs    []ActiveBuff           `json:"active_buffs,omitempty"`
	ScoredSkills   []ScoredSkill          `json:"scored_skills,omitempty"`
	Equipment      map[SlotKey]EquipSpec  `json:"equipment,omitempty"`
	LevelingTarget *Scenario              `json:"leveling_target,omitempty"`
	Score          *scoring.ScoreResponse `json:"score,omitempty"`
	// Gates carries the quality-gate evaluator's per-gate results for
	// this scored snapshot. Populated by the orchestrator after canonical
	// scoring lands. Unscored snapshots leave it nil. Library save logic
	// rejects trajectories whose primary snapshots have any fail /
	// fail_hard severity result; warns flow through as breakdown context.
	Gates []GateResult `json:"gates,omitempty"`
	Notes string       `json:"notes,omitempty"`
}

// SkillAlloc is aliased to scoring.SkillAlloc; same wire shape, no
// double-definition. The alias mirrors how Stats/Level/EquipSpec are
// shared between scoring and domain via build.go.
type SkillAlloc = scoring.SkillAlloc

// ActiveBuff is the LLM's declared intent: which self-buff is active on this
// snapshot, and (for endow buffs) which element. It carries NO level: the
// level is resolved from the anchor skill's allocation during canonical
// scoring (the LLM can't claim a higher buff level than it allocated). The
// resolver turns this into a scoring.Buff. Distinct shape from scoring.Buff,
// so not a type alias the way SkillAlloc is.
type ActiveBuff struct {
	Name    string `json:"name"`
	Element string `json:"element,omitempty"`
}

// ScoredSkill is the LLM's declared intent: an attack skill to score for this
// snapshot, and whether it is the primary (the one whose pass drives the combat
// gates). It carries NO level: the level is resolved from the skill's own
// allocation during canonical scoring. The resolver turns this into a
// scoring.AttackSkill.
type ScoredSkill struct {
	Name    string `json:"name"`
	Primary bool   `json:"primary,omitempty"`
}

func validateActiveBuff(b ActiveBuff) error {
	if b.Name == "" {
		return fmt.Errorf("buff name must be non-empty")
	}
	return nil
}

// Validate runs boundary-level invariants on the trajectory: non-empty,
// ordered, monotonic within rebirth cycle, last snapshot matches the
// declared endgame class. Per-snapshot field sanity (refines, levels) is
// delegated to Snapshot.Validate.
func (t *Trajectory) Validate() error {
	if t.Class == "" {
		return fmt.Errorf("trajectory has empty class")
	}
	if len(t.Snapshots) == 0 {
		return fmt.Errorf("trajectory has no snapshots")
	}

	for i := range t.Snapshots {
		if err := t.Snapshots[i].Validate(); err != nil {
			return fmt.Errorf("snapshot[%d]: %w", i, err)
		}
	}

	// Ordering and monotonicity are checked per rebirth cycle. Within the
	// same PostRebirth flag, base level must non-decrease and stat / skill
	// totals must not regress. Crossing the rebirth boundary resets both.
	for i := 1; i < len(t.Snapshots); i++ {
		prev, cur := t.Snapshots[i-1], t.Snapshots[i]
		if prev.PostRebirth && !cur.PostRebirth {
			return fmt.Errorf("snapshot[%d] regresses from post-rebirth to pre-rebirth; only one rebirth boundary allowed", i)
		}
		if prev.PostRebirth != cur.PostRebirth {
			// rebirth boundary; monotonicity does not apply here
			continue
		}
		if cur.Level.Base < prev.Level.Base {
			return fmt.Errorf("snapshot[%d] base level %d < previous %d (within same rebirth cycle)", i, cur.Level.Base, prev.Level.Base)
		}
		if err := checkStatsMonotonic(prev.Stats, cur.Stats); err != nil {
			return fmt.Errorf("snapshot[%d]: %w", i, err)
		}
		if err := checkSkillsMonotonic(prev.Skills, cur.Skills); err != nil {
			return fmt.Errorf("snapshot[%d]: %w", i, err)
		}
	}

	last := t.Snapshots[len(t.Snapshots)-1]
	if last.Class != t.Class {
		return fmt.Errorf("last snapshot class %q does not match trajectory class %q", last.Class, t.Class)
	}

	return nil
}

// Validate runs per-snapshot field sanity. Refine bounds match Build.Validate
// for consistency at the calc boundary.
//
// Level floor: every snapshot in a trajectory represents a real point on
// the player's progression, so both base and job level must be >= 1. The
// /score endpoint's Build.Validate allows zero (which the shim interprets
// as "use Novice/lvl-1 defaults") for ad-hoc calculator use, but inside a
// Trajectory a zero-level snapshot is junk; pre-renewal characters start
// at base 1, job 1.
func (s *Snapshot) Validate() error {
	if s.Class == "" {
		return fmt.Errorf("class empty")
	}
	if s.Level.Base < 1 || s.Level.Job < 1 {
		return fmt.Errorf("level base=%d job=%d: both must be >= 1 (pre-renewal characters start at 1/1)", s.Level.Base, s.Level.Job)
	}
	for slot, spec := range s.Equipment {
		if spec.Refine < 0 || spec.Refine > 10 {
			return fmt.Errorf("equipment[%s] refine %d out of range (0..10)", slot, spec.Refine)
		}
	}
	seenSkill := make(map[int]struct{}, len(s.Skills))
	for i, sk := range s.Skills {
		if err := validateSkillAlloc(sk); err != nil {
			return fmt.Errorf("skills[%d]: %w", i, err)
		}
		if _, dup := seenSkill[sk.ID]; dup {
			return fmt.Errorf("skills[%d]: duplicate skill id %d (each skill may appear at most once)", i, sk.ID)
		}
		seenSkill[sk.ID] = struct{}{}
	}
	seenBuff := make(map[string]struct{}, len(s.ActiveBuffs))
	for i, b := range s.ActiveBuffs {
		if err := validateActiveBuff(b); err != nil {
			return fmt.Errorf("active_buffs[%d]: %w", i, err)
		}
		if _, dup := seenBuff[b.Name]; dup {
			return fmt.Errorf("active_buffs[%d]: duplicate buff %q (each buff may appear at most once)", i, b.Name)
		}
		seenBuff[b.Name] = struct{}{}
	}
	return nil
}

// validateSkillAlloc enforces minimum field sanity on a SkillAlloc.
// Standalone function rather than a method; SkillAlloc is now a type
// alias to scoring.SkillAlloc, and methods can't be defined on aliases
// from another package. Per-skill max-level lives in the catalog and
// isn't checked here.
func validateSkillAlloc(a SkillAlloc) error {
	if a.ID <= 0 {
		return fmt.Errorf("skill id %d must be > 0", a.ID)
	}
	if a.Level < 1 {
		return fmt.Errorf("skill id %d level %d must be >= 1", a.ID, a.Level)
	}
	return nil
}

// ToScoreRequest packages the snapshot's calc-relevant fields as the body
// the calc shim expects. LevelingTarget; when set and non-zero; supplies
// the combat-sim target; profile, when non-nil, swaps custom-mob targets
// into the inline-stats path so the shim doesn't try to look them up in
// the calc's mob table. Mirrors Build.ToScoreRequest's elision
// pattern so zero-valued fields don't clobber the shim's reset baseline.
func (s *Snapshot) ToScoreRequest(profile *ServerProfile) *scoring.ScoreRequest {
	req := &scoring.ScoreRequest{}
	if s.Class != "" {
		req.Class = new(s.Class)
	}
	if s.Level != (Level{}) {
		req.Level = new(s.Level)
	}
	if s.Stats != (Stats{}) {
		req.Stats = new(s.Stats)
	}
	if len(s.Skills) > 0 {
		req.Skills = s.Skills
	}
	if len(s.Equipment) > 0 {
		req.Equipment = s.Equipment
	}
	if s.LevelingTarget != nil && s.LevelingTarget.Target != 0 {
		req.Enemy, req.EnemyInline = ResolveEnemy(profile, s.LevelingTarget.Target)
	}
	return req
}

// checkStatsMonotonic collects every regressing stat and joins them via
// errors.Join so callers see the full picture (e.g. "STR and DEX both
// regressed") instead of having to fix one and re-run to find the next.
func checkStatsMonotonic(prev, cur Stats) error {
	var errs []error
	if cur.Str < prev.Str {
		errs = append(errs, fmt.Errorf("STR regressed %d → %d within rebirth cycle", prev.Str, cur.Str))
	}
	if cur.Agi < prev.Agi {
		errs = append(errs, fmt.Errorf("AGI regressed %d → %d within rebirth cycle", prev.Agi, cur.Agi))
	}
	if cur.Vit < prev.Vit {
		errs = append(errs, fmt.Errorf("VIT regressed %d → %d within rebirth cycle", prev.Vit, cur.Vit))
	}
	if cur.Int < prev.Int {
		errs = append(errs, fmt.Errorf("INT regressed %d → %d within rebirth cycle", prev.Int, cur.Int))
	}
	if cur.Dex < prev.Dex {
		errs = append(errs, fmt.Errorf("DEX regressed %d → %d within rebirth cycle", prev.Dex, cur.Dex))
	}
	if cur.Luk < prev.Luk {
		errs = append(errs, fmt.Errorf("LUK regressed %d → %d within rebirth cycle", prev.Luk, cur.Luk))
	}
	return errors.Join(errs...)
}

// checkSkillsMonotonic verifies every skill the previous snapshot had at
// some level still appears at >= that level in the current snapshot. New
// skills appearing in cur are fine; that's how class-change skill trees
// open up. All violations are collected and joined so a snapshot that
// regresses multiple skills surfaces them in one pass.
func checkSkillsMonotonic(prev, cur []SkillAlloc) error {
	curLevels := make(map[int]int, len(cur))
	for _, a := range cur {
		curLevels[a.ID] = a.Level
	}
	var errs []error
	for _, p := range prev {
		c, ok := curLevels[p.ID]
		if !ok {
			errs = append(errs, fmt.Errorf("skill id %d (level %d) disappeared within rebirth cycle", p.ID, p.Level))
			continue
		}
		if c < p.Level {
			errs = append(errs, fmt.Errorf("skill id %d regressed lvl %d → %d within rebirth cycle", p.ID, p.Level, c))
		}
	}
	return errors.Join(errs...)
}
