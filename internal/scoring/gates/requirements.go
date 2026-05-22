package gates

import "fmt"

// underspendWarnThreshold is the slack we allow before flagging unspent
// status points. At high stat values each raise costs 8-14 points, so
// anything above ~5 means the build could have bought one more stat-step
// it left on the table. Pure suboptimality, not invalid; surfaced as a
// warning so the LLM can iterate but the build isn't rejected.
const underspendWarnThreshold = 5

// evaluateRequirements: stat-cap respected (statPointsRemaining >= 0)
// is the only hard check we have direct access to from the calc.
// Under-spend (positive remaining) is a soft warning. Skill-tree
// validity and slot validity are enforced upstream by the calc shim
// (a bad allocation surfaces as a sidecar 4xx, not a gate failure).
func evaluateRequirements(in Inputs) []Result {
	if in.Score == nil {
		return nil
	}
	rem := in.Score.Derived.StatPointsRemaining
	if rem < 0 {
		return []Result{{
			Name:      "stat_points_overspent",
			Threshold: 0,
			Actual:    rem,
			Severity:  SeverityFailHard,
			Reason:    fmt.Sprintf("build allocates %d more stat points than the level provides", -rem),
		}}
	}
	if rem > underspendWarnThreshold {
		return []Result{{
			Name:      "stat_points_underspent",
			Threshold: underspendWarnThreshold,
			Actual:    rem,
			Severity:  SeverityWarn,
			Reason:    fmt.Sprintf("%d unspent stat points; raise stats to use the full budget", rem),
		}}
	}
	return []Result{{
		Name:     "requirements_met",
		Actual:   rem,
		Severity: SeverityPass,
		Reason:   "stat allocation within level budget",
	}}
}
