package data

// Self-buff kinds, persistence, and the element set used by the
// skill_buffs.yaml overlay (merged onto skill records by cmd/build-catalog)
// and by the buff resolver. Mirrors immunity.go: constants + an "all" slice
// + an IsValid helper so a typo in the overlay fails the catalog build
// instead of silently dropping the tag.

const (
	// Semantic kinds. weapon_endow forces a weapon element (Mild Wind);
	// stat_buff is a level-select stat state (Spurt, and most class buffs);
	// status is a permanent character status (Taekwon Ranker). Kind drives
	// validation + LLM presentation, NOT calc-engine wiring (the sidecar binding
	// table does that).
	BuffKindWeaponEndow = "weapon_endow"
	BuffKindStatBuff    = "stat_buff"
	BuffKindStatus      = "status"
	// debuff is a player-cast enemy debuff (Lex Aeterna, Decrease AGI, Signum
	// Crucis): it modifies the combat-sim target, not the player, but the build
	// declares it like any self-buff. Driven via the enemy_debuf sidecar driver.
	BuffKindDebuff = "debuff"
	// land is a self-cast ground effect (Volcano, Deluge, Violent Gale) that
	// amplifies all same-element damage (auto-attack and spells) of those
	// standing on it. One land is active at a time. Driven via the land_buff
	// sidecar driver (the A6_Skill ground bank).
	BuffKindLand = "land"

	PersistencePermanent = "permanent"
	PersistenceTransient = "transient"
)

// AllBuffKinds is the canonical kind set (for overlay validation).
var AllBuffKinds = []string{BuffKindWeaponEndow, BuffKindStatBuff, BuffKindStatus, BuffKindDebuff, BuffKindLand}

// AllPersistence is the canonical persistence set (for overlay validation).
var AllPersistence = []string{PersistencePermanent, PersistenceTransient}

// AllElements is the weapon-element set a weapon_endow buff may name. Order
// is irrelevant here (the per-buff endow order, which encodes level gating,
// lives in the overlay's endow.elements list). "neutral" maps to the calc
// engine's "(unchanged)" control value; there is no force-neutral option.
var AllElements = []string{
	"neutral", "water", "earth", "fire", "wind",
	"poison", "holy", "shadow", "ghost", "undead",
}

// IsValidBuffKind reports whether s is a recognized self-buff kind.
func IsValidBuffKind(s string) bool {
	for _, v := range AllBuffKinds {
		if v == s {
			return true
		}
	}
	return false
}

// IsValidPersistence reports whether s is a recognized persistence value.
func IsValidPersistence(s string) bool {
	for _, v := range AllPersistence {
		if v == s {
			return true
		}
	}
	return false
}

// IsValidElement reports whether s is a recognized weapon element.
func IsValidElement(s string) bool {
	for _, v := range AllElements {
		if v == s {
			return true
		}
	}
	return false
}
