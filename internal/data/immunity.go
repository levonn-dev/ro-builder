package data

// Status constants for the build / mob / item immunity-tag system. Used by:
//
//   - Item.GrantsImmunity (e.g. Marc Card → ImmunityFrozen)
//   - Mob.Threats.AppliesStatus (e.g. an MVP that applies ImmunityStun)
//   - The gates evaluator's per-scenario relevance lists: PvM/MVP must-immune
//     = [frozen, stun]; PvP must-immune = [frozen, stun, sleep]; plus
//     per-scenario nice-to-haves for warn-tier gates.
//
// These are the pre-renewal status set that has any practical relevance.
// Element resists are NOT modeled here; they flow through the calc shim's
// combat output instead.
const (
	ImmunityFrozen     = "frozen"
	ImmunityStun       = "stun"
	ImmunitySleep      = "sleep"
	ImmunityStoneCurse = "stone_curse"
	ImmunityCurse      = "curse"
	ImmunitySilence    = "silence"
	ImmunityBlind      = "blind"
	ImmunityPoison     = "poison"
	ImmunityConfusion  = "confusion"
)

// AllImmunities is the canonical set, in roughly "most build-relevant first"
// order. Useful for validation (an immunity tag outside this set is a typo)
// and for rendering tables.
var AllImmunities = []string{
	ImmunityFrozen,
	ImmunityStun,
	ImmunitySleep,
	ImmunityStoneCurse,
	ImmunityCurse,
	ImmunitySilence,
	ImmunityBlind,
	ImmunityPoison,
	ImmunityConfusion,
}

// IsValidImmunity reports whether s is one of the recognized status keys.
func IsValidImmunity(s string) bool {
	for _, v := range AllImmunities {
		if v == s {
			return true
		}
	}
	return false
}
