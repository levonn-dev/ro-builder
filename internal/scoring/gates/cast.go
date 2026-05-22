package gates

import "fmt"

// evaluateCastTime emits per-allocated-skill results when the
// effective cast time at the snapshot's total DEX exceeds a threshold.
// Pre-re formula: cast = base * (150 - DEX) / 150 (capped at 0). At
// 150 DEX every cast is instant; the gate self-tunes against the
// build's actual DEX.
//
// Threshold tiers from QualityGates: warn at > 2s, fail at > 4s. Builds
// that allocate a long-cast skill they can't actually cast in combat
// (e.g. a Wizard with Storm Gust at 30 DEX → 12s cast) get flagged
// here; independent of the IUC gate which checks Phen coverage.
func evaluateCastTime(in Inputs) []Result {
	if in.Snapshot == nil || in.Catalog == nil {
		return nil
	}
	qg := resolveQualityGates(in.Profile)
	dex := in.Snapshot.Stats.Dex

	var results []Result
	for _, sk := range in.Snapshot.Skills {
		if sk.Level <= 0 {
			continue
		}
		skill, ok := in.Catalog.Skill(sk.ID)
		if !ok || skill.CastTimeMs <= 0 {
			continue
		}
		effMs := effectiveCastTimeMs(skill.CastTimeMs, skill.FixedCastMs, dex)
		switch {
		case qg.CastTimeFailMs > 0 && effMs > qg.CastTimeFailMs:
			results = append(results, Result{
				Name:      "cast_time_" + skill.AegisName,
				Threshold: qg.CastTimeFailMs,
				Actual:    effMs,
				Severity:  SeverityFail,
				Reason: fmt.Sprintf("%s effective cast %dms at %d DEX exceeds %dms; unusable in active combat",
					skill.AegisName, effMs, dex, qg.CastTimeFailMs),
			})
		case qg.CastTimeWarnMs > 0 && effMs > qg.CastTimeWarnMs:
			results = append(results, Result{
				Name:      "cast_time_" + skill.AegisName,
				Threshold: qg.CastTimeWarnMs,
				Actual:    effMs,
				Severity:  SeverityWarn,
				Reason: fmt.Sprintf("%s effective cast %dms at %d DEX exceeds %dms warn threshold",
					skill.AegisName, effMs, dex, qg.CastTimeWarnMs),
			})
		}
	}
	return results
}

// effectiveCastTimeMs computes pre-renewal cast time given a skill's
// base + fixed components and the player's total DEX. Pre-re formula:
//
//	variable_cast = base * max(0, (150 - DEX)) / 150
//	effective    = fixed + variable_cast
//
// At 150+ DEX the variable portion is 0; only the fixed portion remains
// (and most pre-re skills have fixed = 0, so they're truly instant).
func effectiveCastTimeMs(baseMs, fixedMs, dex int) int {
	if dex >= 150 {
		return fixedMs
	}
	if dex < 0 {
		dex = 0
	}
	variable := baseMs * (150 - dex) / 150
	return fixedMs + variable
}
