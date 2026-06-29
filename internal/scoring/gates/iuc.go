package gates

import "fmt"

// Skill ids for detectable cast-safety strategies (positioning / blocking).
const (
	skillSafetyWall = 12 // MG_SAFETYWALL: blocks physical interrupts
	skillFireWall   = 18 // MG_FIREWALL: positioning / push
	skillIceWall    = 87 // WZ_ICEWALL: positioning / pathing block
)

// dodgeSafeCastPct is the dodge-vs-target chance above which a flee-based build
// is considered able to cast safely (rarely hit mid-cast). Tunable.
const dodgeSafeCastPct = 80.0

// evaluateUninterruptibleCast judges interruption risk on the build's PRIMARY
// attack skill, at its allocated level, using true effective DEX.
//
// Prerequisite: resolves the primary attack skill from the snapshot's
// ScoredSkills; returns nil immediately when no primary is designated or it
// cannot be resolved.
//
// Coverage pre-check: if the build equips uninterruptible-cast gear (Phen /
// Orleans's Gown), it casts through damage; emit a single pass and return.
//
// Fire condition: otherwise fire only when the primary is interruptible AND
// effective cast > CastInterruptMs. Severity is conditional on a detectable
// cast-safety strategy: Safety Wall / Ice Wall / Fire Wall allocated, or high
// dodge vs the target, demote to warn; otherwise fail. The fail reason notes
// that a tank or positioning party strategy also resolves it (we do not model
// party play).
//
// Fires only on scored snapshots (effective stats require a calc score).
func evaluateUninterruptibleCast(in Inputs) []Result {
	skill, level, ok := primaryAttackSkill(in)
	if !ok {
		return nil
	}

	if equipmentGrantsUninterruptibleCast(in) {
		return []Result{{
			Name:     "uninterruptible_cast",
			Severity: SeverityPass,
			Reason:   "Phen / Orleans Gown / equivalent equipped",
		}}
	}

	if !skill.Interruptible {
		return nil // non-interruptible primary: nothing to cancel
	}
	stats, ok := effectiveStats(in)
	if !ok {
		return nil
	}
	qg := resolveQualityGates(in.Profile)
	if qg.CastInterruptMs <= 0 {
		return nil
	}
	effMs := effectiveCastTimeMs(skill.CastAtLevelMs(level), skill.FixedCastMs, stats.Dex)
	if effMs <= qg.CastInterruptMs {
		return nil
	}

	if mitigation, ok := castSafetyMitigation(in); ok {
		return []Result{{
			Name:      "uninterruptible_cast",
			Threshold: qg.CastInterruptMs,
			Actual:    effMs,
			Severity:  SeverityWarn,
			Reason: fmt.Sprintf("%s effective cast %dms at %d effective DEX (incl. gear/cards/buffs, not base allocation) is interruptible with no Phen/Orleans, but %s mitigates interruption; playable with technique",
				skill.AegisName, effMs, stats.Dex, mitigation),
		}}
	}
	return []Result{{
		Name:      "uninterruptible_cast",
		Threshold: qg.CastInterruptMs,
		Actual:    effMs,
		Severity:  SeverityFail,
		Reason: fmt.Sprintf("%s effective cast %dms at %d effective DEX (incl. gear/cards/buffs, not base allocation) is interruptible and incoming damage will cancel it; add Phen/Orleans's Gown, Safety Wall, Ice/Fire Wall positioning, or high flee (a tank or positioning party strategy also resolves it)",
			skill.AegisName, effMs, stats.Dex),
	}}
}

// castSafetyMitigation reports the first detectable self-strategy that lets the
// build cast safely without uninterruptible-cast gear, and a label for it.
func castSafetyMitigation(in Inputs) (string, bool) {
	allocated := func(id int) bool {
		if in.Snapshot == nil {
			return false
		}
		for _, sk := range in.Snapshot.Skills {
			if sk.ID == id && sk.Level > 0 {
				return true
			}
		}
		return false
	}
	switch {
	case allocated(skillSafetyWall):
		return "Safety Wall", true
	case allocated(skillIceWall):
		return "Ice Wall positioning", true
	case allocated(skillFireWall):
		return "Fire Wall positioning", true
	}
	if in.Score != nil && in.Score.Combat != nil && in.Score.Combat.Dodge != nil &&
		*in.Score.Combat.Dodge >= dodgeSafeCastPct {
		return fmt.Sprintf("high flee (%.0f%% dodge vs target)", *in.Score.Combat.Dodge), true
	}
	return "", false
}
