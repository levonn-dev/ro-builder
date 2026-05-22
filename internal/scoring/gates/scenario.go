package gates

import "log/slog"

// deriveScenarioType returns "pvm" or "pvp" for the gate priority lookup.
// Primary signal: combat.enemy.type from the calc shim ("MVP" / "Boss" /
// "Normal" / "PlayerCharacter"). Fallback: request-level playstyle hint.
// Default: "pvm".
//
// WoE folds into pvp. The pvp branch is wired even though it isn't
// currently exercised (forward-compat).
func deriveScenarioType(in Inputs) string {
	if in.Score != nil && in.Score.Combat != nil {
		switch t := in.Score.Combat.Enemy.Type; t {
		case "MVP", "Boss", "Normal":
			return "pvm"
		case "PlayerCharacter":
			return "pvp"
		case "":
			// Empty type means the calc didn't run a combat sim; fall
			// through to the playstyle hint without logging; this is
			// the normal "no enemy" path, not an unknown value.
		default:
			slog.Default().Warn("scenario gate: unknown enemy type, treating as pvm",
				slog.String("type", t))
		}
	}
	switch in.Playstyle {
	case "pvp", "woe":
		return "pvp"
	}
	return "pvm"
}

// isMVP reports whether the calc target is MVP-tier. Used by gates that
// branch their thresholds (HIT, EHP, TTK).
func isMVP(in Inputs) bool {
	if in.Score == nil || in.Score.Combat == nil {
		return false
	}
	t := in.Score.Combat.Enemy.Type
	return t == "MVP" || t == "Boss"
}
