package gates

import "fmt"

// evaluateTTK gates time-to-kill against the MVP-only ceiling. Normal
// mobs are too scenario-dependent; covered loosely by the per-skill
// rotation gate (if your primary skill rotation is too slow, the TTK
// signals via that gate too).
func evaluateTTK(in Inputs) []Result {
	if !isMVP(in) {
		return nil
	}
	if in.Score == nil || in.Score.Combat == nil || in.Score.Combat.BattleTimeSec == nil {
		return nil
	}
	qg := resolveQualityGates(in.Profile)
	if qg.TTKMVPMaxSec <= 0 {
		return nil
	}
	actual := *in.Score.Combat.BattleTimeSec
	if actual <= float64(qg.TTKMVPMaxSec) {
		return []Result{{
			Name:      "ttk_mvp",
			Threshold: qg.TTKMVPMaxSec,
			Actual:    actual,
			Severity:  SeverityPass,
		}}
	}
	return []Result{{
		Name:      "ttk_mvp",
		Threshold: qg.TTKMVPMaxSec,
		Actual:    actual,
		Severity:  SeverityFail,
		Reason: fmt.Sprintf("MVP TTK %.0fs exceeds %d s ceiling; regen/status timers undermine the kill",
			actual, qg.TTKMVPMaxSec),
	}}
}
