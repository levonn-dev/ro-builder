package gates

import "fmt"

// evaluateFlee fires only when the build's mitigation is materially
// dodge-driven. We derive that condition from the calc itself: if
// aveWithDodge is meaningfully below ave, dodging is doing real work
// for survival → gate flee. Otherwise skip (a tank build with 5 FLEE
// shouldn't fail the FLEE gate just because its number is low).
//
// The "materially below" threshold is half; if dodge is reducing
// average incoming damage by 50%+, the build is at least partially
// dodge-tanking. Below that, mitigation is dominated by armor / HP /
// element resist, and FLEE is a dump stat outcome.
const fleeRelianceFactor = 0.5

func evaluateFlee(in Inputs) []Result {
	if in.Score == nil || in.Score.Combat == nil {
		return nil
	}
	c := in.Score.Combat
	// combat.Dodge is the per-encounter outcome (% chance to dodge), which
	// is what this gate cares about; the flat FLEE stat alone doesn't
	// determine survival without the target's HIT context.
	if c.Dodge == nil || c.Incoming.Ave == nil || c.Incoming.AveWithDodge == nil {
		return nil
	}
	// Dodge isn't doing material work; skip the gate.
	if *c.Incoming.AveWithDodge >= *c.Incoming.Ave*fleeRelianceFactor {
		return nil
	}

	qg := resolveQualityGates(in.Profile)
	actual := *c.Dodge / 100.0

	// In WoE / pvp the displayed FLEE is reduced by 20% pre-re. Compare
	// against the post-penalty value so a build that only just clears
	// 90% in PvM doesn't pass the same gate in WoE.
	if deriveScenarioType(in) == "pvp" && qg.FLEE.WoEPenalty > 0 {
		actual *= 1.0 - qg.FLEE.WoEPenalty
	}

	switch {
	case actual < qg.FLEE.Fail:
		return []Result{fleeResult(qg.FLEE.Fail, actual, SeverityFail,
			"FLEE %g; build relies on dodge but is below the 90%% floor; survives one mob, folds on AoE")}
	case actual < qg.FLEE.Warn:
		return []Result{fleeResult(qg.FLEE.Warn, actual, SeverityWarn,
			"FLEE %g; dodge-tank build below 95%%; +5 AGI / Whistle / Vali's Manteau closes the gap")}
	default:
		return []Result{fleeResult(qg.FLEE.Warn, actual, SeverityPass, "")}
	}
}

func fleeResult(thr, act float64, sev, reasonFmt string) Result {
	r := Result{Name: "flee_floor", Threshold: thr, Actual: act, Severity: sev}
	if reasonFmt != "" {
		r.Reason = fmt.Sprintf(reasonFmt, act*100)
	}
	return r
}
