package gates

import "fmt"

// evaluateEHP checks survival via two complementary layers:
//
//	Layer A (relative): maxHp / incoming.max >= multiplier
//	  normal mob: 1.5×, MVP melee: 2.0×, MVP magic burst: 2.5×.
//	  Captures "can the build survive one big hit." Hard floor:
//	  incoming.max <= maxHp (else one-shot).
//
//	Layer B (absolute, content-derived): maxHp >= incoming.ave * battleTimeSec
//	  The kill takes battleTimeSec seconds at incoming.ave damage per
//	  second; build needs at least that much HP to survive the fight.
//	  Subsumes archetype-specific "17k LK BB" / "10k Whitesmith" floors
//	  without authoring per-archetype tables; derives from the actual
//	  content the snapshot targets.
//
// Returns one or two Results: layer A always emits (when calc data is
// present); layer B emits only if it adds info beyond layer A.
func evaluateEHP(in Inputs) []Result {
	if in.Score == nil || in.Score.Combat == nil {
		return nil
	}
	c := in.Score.Combat
	maxHp := in.Score.Derived.MaxHP
	if maxHp == 0 {
		return nil
	}

	var out []Result

	// Layer A; relative to biggest single hit.
	if c.Incoming.Max != nil && *c.Incoming.Max > 0 {
		out = append(out, evaluateEHPLayerA(in, maxHp, *c.Incoming.Max))
	}
	// Layer B; absolute, content-derived.
	if c.BattleTimeSec != nil && c.Incoming.Ave != nil {
		if r := evaluateEHPLayerB(in, maxHp, *c.Incoming.Ave, *c.BattleTimeSec); r != nil {
			out = append(out, *r)
		}
	}
	return out
}

func evaluateEHPLayerA(in Inputs, maxHp int, incomingMax float64) Result {
	qg := resolveQualityGates(in.Profile)
	mult := qg.EHPMultiplier.Normal
	tag := "normal"
	if isMVP(in) {
		mult = qg.EHPMultiplier.MVPMelee
		tag = "mvp_melee"
		if isMagicBurstTarget(in) {
			mult = qg.EHPMultiplier.MVPMagicBurst
			tag = "mvp_magic_burst"
		}
	}
	required := incomingMax * mult
	ratio := float64(maxHp) / incomingMax

	switch {
	case float64(maxHp) < incomingMax:
		// Hard floor: one-shot.
		return Result{
			Name:      "ehp_one_shot_" + tag,
			Threshold: incomingMax,
			Actual:    maxHp,
			Severity:  SeverityFailHard,
			Reason: fmt.Sprintf("target's biggest hit (%.0f) exceeds maxHp (%d); build is one-shot",
				incomingMax, maxHp),
		}
	case float64(maxHp) < required:
		return Result{
			Name:      "ehp_burst_ratio_" + tag,
			Threshold: required,
			Actual:    maxHp,
			Severity:  SeverityFail,
			Reason: fmt.Sprintf("maxHp %d below %.1f× target's biggest hit (%.0f required)",
				maxHp, mult, required),
		}
	default:
		return Result{
			Name:      "ehp_burst_ratio_" + tag,
			Threshold: required,
			Actual:    maxHp,
			Severity:  SeverityPass,
			Reason: fmt.Sprintf("maxHp %d clears %.1f× big-hit floor (ratio %.2f)",
				maxHp, mult, ratio),
		}
	}
}

func evaluateEHPLayerB(in Inputs, maxHp int, incomingAve, battleTimeSec float64) *Result {
	required := incomingAve * battleTimeSec
	if required <= 0 {
		return nil
	}
	if float64(maxHp) >= required {
		// Skip emitting a duplicate pass result when layer A already
		// covers it.
		return nil
	}
	return &Result{
		Name:      "ehp_fight_duration",
		Threshold: required,
		Actual:    maxHp,
		Severity:  SeverityFail,
		Reason: fmt.Sprintf("maxHp %d below incoming.ave %.0f × battleTimeSec %.1f = %.0f required to survive the fight",
			maxHp, incomingAve, battleTimeSec, required),
	}
}

// isMagicBurstTarget reports whether the target's threats include
// frozen, sleep, or stone_curse; the proxy for "this MVP burst-casts
// magic at you" used to bump the EHP multiplier from 2.0× to 2.5×.
// Imperfect heuristic but better than nothing.
func isMagicBurstTarget(in Inputs) bool {
	threats := targetThreats(in)
	if threats == nil {
		return false
	}
	for _, s := range threats.AppliesStatus {
		if s == "frozen" || s == "sleep" || s == "stone_curse" {
			return true
		}
	}
	return false
}
