package gates

import "fmt"

// hitInsensitiveClasses lists classes whose primary offense doesn't roll
// auto-attack HIT vs target FLEE. The calc still reports a HIT number
// (it's the auto-attack hit-rate, which is always defined), but that
// number isn't the right quality lens for these builds; gating on it
// rejects viable trajectories. We demote fail/fail_hard to warn so the
// data surfaces to the LLM but doesn't block the build.
//
//   - Taekwon family (taekwon/taekwon_kid/taekwon_master/star_gladiator):
//     heat skills have separate hit mechanics; the ranker counter kick is
//     unconditional.
//   - Soul Linker: offense is spells.
//   - Magic-primary classes (mage/wizard/sage/high_mage/high_wizard/
//     professor): magic damage doesn't roll HIT vs FLEE.
//
// Keys mirror Snapshot.Class values (canonical lowercase-with-underscore
// names from the shim's class-id table). Emulator-flavored aliases (magician,
// high_magician, scholar) collapse to the canonical key via
// normalizeClassForGates before lookup, so the map carries one row per
// concept.
var hitInsensitiveClasses = map[string]bool{
	"taekwon":        true,
	"taekwon_kid":    true,
	"taekwon_master": true,
	"star_gladiator": true,
	"soul_linker":    true,

	"mage":        true,
	"wizard":      true,
	"sage":        true,
	"high_mage":   true,
	"high_wizard": true,
	"professor":   true,
}

// isHitInsensitive returns true when the snapshot's offense doesn't
// rely on the auto-attack hit roll. Two signals: (a) class is in the
// always-skill-offense allowlist above; (b) MATK exceeds total ATK,
// indicating a magic-primary build regardless of class (e.g. a hybrid
// Sage running pure-magic gear, or any class wearing magic-heavy
// equipment that pivots the offense to spells).
func isHitInsensitive(in Inputs) bool {
	if in.Snapshot != nil && hitInsensitiveClasses[normalizeClassForGates(in.Snapshot.Class)] {
		return true
	}
	if in.Score != nil {
		d := in.Score.Derived
		totalAtk := d.Atk.Base + d.Atk.Plus
		if d.Matk.Max > totalAtk {
			return true
		}
	}
	return false
}

// evaluateHit checks combat.hit against tiered thresholds. combat.hit
// is a percent value (e.g. 95.0 for 95%); we normalize to a fraction
// before comparing to the QualityGates.HIT block (which stores
// fractions, e.g. 0.95).
//
// MVP targets get strict thresholds (fail-hard at 85%, fail at 95%);
// normal targets get tiered (fail-hard at 80%, fail at 95%, warn below
// 100%; every guide implicitly aims for 100% on the active farm
// target since arrow/SP cost per kill scales linearly below that).
//
// Demotion: for builds whose offense doesn't roll auto-attack HIT
// (see isHitInsensitive), fail/fail_hard demotes to warn so the data
// surfaces to the LLM without rejecting the trajectory for a non-issue.
func evaluateHit(in Inputs) []Result {
	if in.Score == nil || in.Score.Combat == nil || in.Score.Combat.Hit == nil {
		return nil
	}
	qg := resolveQualityGates(in.Profile)
	actual := *in.Score.Combat.Hit / 100.0

	var r Result
	if isMVP(in) {
		switch {
		case actual < qg.HIT.MVPFailHard:
			r = hitResult("hit_floor_mvp", qg.HIT.MVPFailHard, actual, SeverityFailHard,
				"HIT %g vs MVP target; half your swings miss; fight is unwinnable")
		case actual < qg.HIT.MVPFail:
			r = hitResult("hit_floor_mvp", qg.HIT.MVPFail, actual, SeverityFail,
				"HIT %g vs MVP target; below the 95%% MVP floor; arrows/SP scale per miss")
		default:
			r = hitResult("hit_floor_mvp", qg.HIT.MVPFail, actual, SeverityPass, "")
		}
	} else {
		switch {
		case actual < qg.HIT.NormalFailHard:
			r = hitResult("hit_floor_normal", qg.HIT.NormalFailHard, actual, SeverityFailHard,
				"HIT %g; below 80%%, build can't farm this target")
		case actual < qg.HIT.NormalFail:
			r = hitResult("hit_floor_normal", qg.HIT.NormalFail, actual, SeverityFail,
				"HIT %g; below 95%%, farming economy breaks (arrow/SP cost per kill scales)")
		case actual < qg.HIT.NormalWarn:
			r = hitResult("hit_floor_normal", qg.HIT.NormalWarn, actual, SeverityWarn,
				"HIT %g; every guide implicitly aims for 100%%; below this scales per-kill cost")
		default:
			r = hitResult("hit_floor_normal", qg.HIT.NormalWarn, actual, SeverityPass, "")
		}
	}

	// Demote when the build's primary offense doesn't roll auto-attack
	// HIT (Taekwon kicks, magic spells, etc.); the hit-rate the calc
	// reports isn't the right quality lens. Reason gets a leading
	// sentence so the LLM understands why severity moved.
	if (r.Severity == SeverityFail || r.Severity == SeverityFailHard) &&
		isHitInsensitive(in) {
		original := r.Severity
		r.Severity = SeverityWarn
		demote := fmt.Sprintf("auto-attack hit-rate isn't the right quality lens for this build (primary offense is skill/magic-based, not HIT-rolled); %s demoted to warn. ", original)
		r.Reason = demote + r.Reason
	}
	return []Result{r}
}

func hitResult(name string, thr, act float64, sev, reasonFmt string) Result {
	r := Result{Name: name, Threshold: thr, Actual: act, Severity: sev}
	if reasonFmt != "" {
		r.Reason = fmt.Sprintf(reasonFmt, act*100)
	}
	return r
}
