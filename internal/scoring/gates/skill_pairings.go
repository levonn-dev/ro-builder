package gates

import (
	"fmt"
	"sort"
	"strings"
)

// taekwonKickStancePairs maps each Taekwon kick skill (aegis name) to
// its corresponding READY-stance passive. Without the stance learned
// the kick produces no usable damage; the stance is what activates
// the kick's full damage table in pre-renewal. TK_JUMPKICK (Flying
// Kick) is the only kick without a stance and is intentionally absent.
//
// This pairing isn't expressible via the standard prerequisite graph
// (the Hercules/rAthena skill_tree.conf inverts it; the kick is
// listed as a prereq for the stance, not vice versa) so we enforce
// it as a dedicated gate rather than relying on prereq validation.
var taekwonKickStancePairs = map[string]string{
	"TK_STORMKICK": "TK_READYSTORM",   // Tornado Kick → Tornado Stance
	"TK_DOWNKICK":  "TK_READYDOWN",    // Heel Drop → Heel Drop Stance
	"TK_TURNKICK":  "TK_READYTURN",    // Roundhouse Kick → Roundhouse Stance
	"TK_COUNTER":   "TK_READYCOUNTER", // Counter Kick → Counter Kick Stance
}

// evaluateTaekwonKickStances emits a fail gate when a snapshot allocates
// a Taekwon kick without its stance partner. Kicks land but deal trivial
// damage without the stance, so the build functionally doesn't work for
// its intended offense. Severity is Fail (not FailHard); the rejection
// is recoverable: the LLM allocates the stance and resubmits.
//
// Skipped when the catalog isn't available (no way to resolve allocated
// skill ids to aegis names).
func evaluateTaekwonKickStances(in Inputs) []Result {
	if in.Snapshot == nil || in.Catalog == nil {
		return nil
	}

	allocated := map[string]bool{}
	for _, sk := range in.Snapshot.Skills {
		if sk.Level <= 0 {
			continue
		}
		s, ok := in.Catalog.Skill(sk.ID)
		if !ok {
			continue
		}
		allocated[s.AegisName] = true
	}

	var missing []string
	for kick, stance := range taekwonKickStancePairs {
		if allocated[kick] && !allocated[stance] {
			missing = append(missing, fmt.Sprintf("%s without %s", kick, stance))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)

	return []Result{{
		Name:     "taekwon_kick_missing_stance",
		Severity: SeverityFail,
		Reason: "Taekwon kick allocated without its READY-stance passive; the stance enables the kick's damage table; the build functionally doesn't deal damage. Allocate the stance at lv 1: " +
			strings.Join(missing, ", "),
	}}
}
