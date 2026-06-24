// Package scoring is the Go side of the calc-sidecar contract: typed mirrors
// of calc-sidecar/src/types.ts plus an HTTP client (client.go) that drives a
// running sidecar over POST /score.
//
// The sidecar's types.ts is the source of truth; every field here
// corresponds to one in the TS file, every JSON tag matches the TS property
// name. When the TS contract changes, this file must change in lock-step;
// the integration test in client_test.go is the canary.
//
// Numeric fields the shim can render as null (the contract for "could not
// compute"; e.g. infinite hit rate, time-to-kill exceeding the engine's
// iteration cap) are *float64 in Go. JSON null unmarshals to nil; callers
// branch on nil rather than parsing free-form text.
package scoring

// SlotKey identifies one of the ten equipment slots the shim accepts. The
// strings match the TS SlotKey union exactly; the sidecar rejects anything
// else with a 400.
type SlotKey string

const (
	SlotWeapon     SlotKey = "weapon"
	SlotHeadTop    SlotKey = "headTop"
	SlotHeadMid    SlotKey = "headMid"
	SlotHeadBot    SlotKey = "headBot"
	SlotShield     SlotKey = "shield"
	SlotArmor      SlotKey = "armor"
	SlotGarment    SlotKey = "garment"
	SlotFootgear   SlotKey = "footgear"
	SlotAccessory1 SlotKey = "accessory1"
	SlotAccessory2 SlotKey = "accessory2"
)

// Stats is the six-stat status-point allocation. Values are absolute (1-99
// in pre-renewal), not deltas.
type Stats struct {
	Str int `json:"str"`
	Agi int `json:"agi"`
	Vit int `json:"vit"`
	Int int `json:"int"`
	Dex int `json:"dex"`
	Luk int `json:"luk"`
}

// Level pairs base level (HP/SP scaling, status point pool) with job level
// (job-bonus stat allocation). Both 1-indexed.
type Level struct {
	Base int `json:"base"`
	Job  int `json:"job"`
}

// EquipSpec describes one equipment slot's content. ID and Cards are iRO IDs
// (the canonical space every RO server emulator exposes); the sidecar's
// active backend translates to its internal id space at the boundary. Refine
// is 0–10 and only meaningful for slots that accept refinement (weapons,
// armor, headgear, shield, garment, footgear).
type EquipSpec struct {
	ID     int   `json:"id"`
	Refine int   `json:"refine,omitempty"`
	Cards  []int `json:"cards,omitempty"`
}

// BasePlus splits a derived stat into the base value the calc shim returns
// plus the flat bonus from gear/cards (ATK = base + plus on the contract).
type BasePlus struct {
	Base int `json:"base"`
	Plus int `json:"plus"`
}

// MinMax holds the two-bound output for variable-range stats (pre-renewal
// MATK is min..max).
type MinMax struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// HardSoft splits pre-renewal defense into hard (% reduction) and soft (flat
// reduction). Both DEF and MDEF use this shape.
type HardSoft struct {
	Hard int `json:"hard"`
	Soft int `json:"soft"`
}

// DerivedStats is the post-stats post-equipment computed block; what the
// orchestrator and LLM tools reason over for "is this build viable?".
// StatPointsRemaining goes negative when the build over-allocates; treat
// negatives as invalid for the given level.
type DerivedStats struct {
	Hit                 int      `json:"hit"`
	Flee                int      `json:"flee"`
	Cri                 int      `json:"cri"`
	Atk                 BasePlus `json:"atk"`
	Matk                MinMax   `json:"matk"`
	Def                 HardSoft `json:"def"`
	Mdef                HardSoft `json:"mdef"`
	Aspd                float64  `json:"aspd"`
	MaxHP               int      `json:"maxHp"`
	MaxSP               int      `json:"maxSp"`
	StatPointsRemaining int      `json:"statPointsRemaining"`
}

// CombatDamage is the player's outgoing damage to the target (one direction
// of the combat sim). Pointer fields go nil when the shim couldn't compute
// the value (per the contract); the orchestrator should branch on nil.
type CombatDamage struct {
	Min       *float64 `json:"min"`
	Ave       *float64 `json:"ave"`
	Max       *float64 `json:"max"`
	SecondAve *float64 `json:"secondAve"`
}

// CombatCrit is per-crit damage and the % chance per swing.
type CombatCrit struct {
	Damage *float64 `json:"damage"`
	Rate   *float64 `json:"rate"`
}

// CombatIncoming is the target's outgoing damage to the player.
// AveWithDodge applies the player's dodge chance to the average, giving an
// effective DPS-taken figure.
type CombatIncoming struct {
	Min          *float64 `json:"min"`
	Ave          *float64 `json:"ave"`
	Max          *float64 `json:"max"`
	AveWithDodge *float64 `json:"aveWithDodge"`
}

// CombatEnemy is the resolved enemy profile the shim used for the sim.
// HP is nullable because the shim may report it as null for encounter
// types it can't model numerically; the rest are display strings.
type CombatEnemy struct {
	HP      *float64 `json:"hp"`
	Race    string   `json:"race"`
	Element string   `json:"element"`
	Size    string   `json:"size"`
	Type    string   `json:"type"`
}

// CombatResults is the full combat-sim output for one (build, target) pair.
// Hit/Dodge are percent values (0-100, e.g. 95 = 95%); BattleTimeSec is
// float seconds. The flat FLEE stat is on DerivedStats.Flee; Dodge here
// is the per-encounter outcome that uses FLEE plus AGI/luck against the
// target's HIT.
type CombatResults struct {
	Damage        CombatDamage   `json:"damage"`
	Crit          CombatCrit     `json:"crit"`
	Hit           *float64       `json:"hit"`
	Dodge         *float64       `json:"dodge"`
	BattleTimeSec *float64       `json:"battleTimeSec"`
	Incoming      CombatIncoming `json:"incoming"`
	Enemy         CombatEnemy    `json:"enemy"`
}

// ScoreRequest is the POST /score body. All fields are optional; absent
// fields keep the shim's reset baseline (Novice / lvl 1 / all stats 1 / no
// equipment / no combat sim). Set Enemy to drive the combat sim; without
// it the response omits Combat.
//
// Class is a class name per the shim contract ("novice", "taekwon_kid",
// "knight", etc.); the shim translates internally and validates. See
// calc-sidecar/src/types.ts for the canonical names list.
type ScoreRequest struct {
	Class     *string               `json:"class,omitempty"`
	Level     *Level                `json:"level,omitempty"`
	Stats     *Stats                `json:"stats,omitempty"`
	Skills    []SkillAlloc          `json:"skills,omitempty"`
	Buffs     []Buff                `json:"buffs,omitempty"`
	Equipment map[SlotKey]EquipSpec `json:"equipment,omitempty"`
	// Enemy and EnemyInline are mutually exclusive; set one or neither.
	// Enemy is an iRO mob id translated through the calc's id mapping; the
	// shim looks the mob up in its mob table. EnemyInline supplies the stats
	// directly, used for server-overlay custom mobs that aren't in the
	// calc snapshot. The sidecar rejects requests with both set.
	Enemy       *int        `json:"enemy,omitempty"`
	EnemyInline *EnemyStats `json:"enemy_inline,omitempty"`
}

// EnemyStats is the inline mob-stat blob the calc shim accepts in lieu
// of an iRO mob id. Mirrors the calc-relevant subset of data.Mob; same
// field semantics, same prefixed string conventions ("RC_Plant",
// "Ele_Water", "Size_Medium"). Required when the orchestrator scores
// against a server-overlay custom mob.
//
// Level feeds pre-renewal hit/flee scaling in the calc shim's combat sim.
// Zero or negative defaults to 1 on the shim side; pass the real mob
// level for accurate per-target rates against high-level overlay mobs
// (UARO's OGH overlay, lvl 95-100, was previously hard-coded to 1
// here and overestimated player hit by 10-20pp).
type EnemyStats struct {
	Hp        int    `json:"hp"`
	AtkMin    int    `json:"atk_min"`
	AtkMax    int    `json:"atk_max"`
	Def       int    `json:"def"`
	MDef      int    `json:"mdef"`
	Race      string `json:"race"`
	Element   string `json:"element"`
	ElementLv int    `json:"element_lv"`
	Size      string `json:"size"`
	Level     int    `json:"level,omitempty"`
}

// SkillAlloc is one entry in a build's skill allocation: the iRO skill id
// and the level the player has invested. Skill IDs are Gravity-canonical,
// so iRO and the calc engine agree without translation. Per-skill max-level lives
// in the skill catalog and isn't enforced at this layer; the shim
// surfaces "skill not in this class's tree" as a 4xx-classifiable error.
type SkillAlloc struct {
	ID    int `json:"id"`
	Level int `json:"level"`
}

// Buff is one resolved active self-buff sent to the calc shim. Mirrors the TS
// Buff in calc-sidecar/src/types.ts (keep in lock-step). Name is the semantic
// buff key; Level is the resolved buff level (filled from the anchor skill's
// allocation, not LLM-authored); Element is the chosen weapon element for
// endow buffs.
type Buff struct {
	Name    string `json:"name"`
	Level   int    `json:"level,omitempty"`
	Element string `json:"element,omitempty"`
}

// ScoreResponse is the POST /score success body. Combat is non-nil iff the
// request set Enemy. CalcVersion is the engine snapshot stamp (backend name
// + data snapshot date); the build library persists it so stale saved
// trajectories are detectable after the calc snapshot changes.
//
// Uncertainty carries caveats the LLM and the build library should weigh
// when reasoning over these numbers; for the active calc backend it surfaces
// that backend's documented known issues (left-hand crit damage, Soul Breaker
// approximations, inline-mob level scaling) so approximate values aren't
// treated as ground truth. TraceID is a backend-defined opaque id, used
// to tie a saved trajectory back to the exact calc invocation in logs.
type ScoreResponse struct {
	Derived     DerivedStats   `json:"derived"`
	Combat      *CombatResults `json:"combat,omitempty"`
	CalcVersion string         `json:"calc_version"`
	Uncertainty []string       `json:"uncertainty,omitempty"`
	TraceID     string         `json:"trace_id,omitempty"`
}
