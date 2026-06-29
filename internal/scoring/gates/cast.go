package gates

import "fmt"

// evaluateCastTime is the slowness advisory. It judges only the build's
// primary attack skill, at its allocated level, using the character's true
// effective DEX. A primary whose effective cast exceeds the warn threshold is
// flagged warn (no fail tier; the interruption gate owns the hard-fail, and
// the TTK gate owns "cannot kill in time"). Pre-re variable cast:
//
//	effective = fixed + base * max(0, 150 - DEX) / 150
//
// Fires only on scored snapshots (effective stats require a calc score).
func evaluateCastTime(in Inputs) []Result {
	skill, level, ok := primaryAttackSkill(in)
	if !ok {
		return nil
	}
	base := skill.CastAtLevelMs(level)
	if base <= 0 && skill.FixedCastMs <= 0 {
		return nil
	}
	stats, ok := effectiveStats(in)
	if !ok {
		return nil
	}
	qg := resolveQualityGates(in.Profile)
	if qg.CastTimeWarnMs <= 0 {
		return nil
	}
	effMs := effectiveCastTimeMs(base, skill.FixedCastMs, stats.Dex)
	if effMs <= qg.CastTimeWarnMs {
		return nil
	}
	return []Result{{
		Name:      "cast_time_" + skill.AegisName,
		Threshold: qg.CastTimeWarnMs,
		Actual:    effMs,
		Severity:  SeverityWarn,
		Reason: fmt.Sprintf("%s effective cast %dms at %d effective DEX (incl. gear/cards/buffs, not base allocation) exceeds %dms; primary nuke is slow (more DEX / instant-cast gear / Bragi helps)",
			skill.AegisName, effMs, stats.Dex, qg.CastTimeWarnMs),
	}}
}

// effectiveCastTimeMs computes pre-renewal cast time. At 150+ DEX the variable
// portion is 0; only the fixed portion remains.
func effectiveCastTimeMs(baseMs, fixedMs, dex int) int {
	if dex >= 150 {
		return fixedMs
	}
	if dex < 0 {
		dex = 0
	}
	return fixedMs + baseMs*(150-dex)/150
}
