package gates

import "fmt"

// minCastTimeForIUCGateMs is the minimum base cast time at which the
// uninterruptible-cast gate fires. Below ~1s the cast resolves before
// most damage instances can interrupt it; gating sub-second skills as
// needing Phen would over-fire on Heal / Sanctuary / etc.
const minCastTimeForIUCGateMs = 1000

// evaluateUninterruptibleCast fires when the snapshot allocates any
// skill whose DEX-effective cast time at the build's stats > 1s AND
// interruptible == true AND no equipped item carries
// grants_uninterruptible_cast. Phen card and Orleans Gown are the
// canonical sources.
//
// "Allocated" means present in Snapshot.Skills with level > 0. The
// snapshot doesn't tell us which skill is the build's primary, but if
// the build invested points in a long-cast skill, the gate assumes the
// player will use it in combat. The effective-time check (rather than
// raw base) prevents false positives on high-DEX builds where a 2s
// base cast resolves in <500ms.
func evaluateUninterruptibleCast(in Inputs) []Result {
	if in.Snapshot == nil || in.Catalog == nil {
		return nil
	}
	hasIUC := equipmentGrantsUninterruptibleCast(in)
	if hasIUC {
		// Build covers it; gate passes silently. Emit one pass result
		// so the breakdown shows the gate ran.
		return []Result{{
			Name:     "uninterruptible_cast",
			Severity: SeverityPass,
			Reason:   "Phen / Orleans Gown / equivalent equipped",
		}}
	}

	dex := in.Snapshot.Stats.Dex
	var offenders []string
	for _, sk := range in.Snapshot.Skills {
		if sk.Level <= 0 {
			continue
		}
		skill, ok := in.Catalog.Skill(sk.ID)
		if !ok {
			continue
		}
		if !skill.Interruptible {
			continue
		}
		if effectiveCastTimeMs(skill.CastTimeMs, skill.FixedCastMs, dex) < minCastTimeForIUCGateMs {
			continue
		}
		offenders = append(offenders, skill.AegisName)
	}
	if len(offenders) == 0 {
		return nil // no long-cast interruptible skills allocated → gate not relevant
	}
	return []Result{{
		Name:      "uninterruptible_cast",
		Threshold: "Phen / Orleans Gown / equivalent",
		Actual:    offenders,
		Severity:  SeverityFail,
		Reason: fmt.Sprintf("build allocates interruptible long-cast skills (%v) but no equipped item grants uninterruptible cast; incoming damage will cancel the cast",
			offenders),
	}}
}
