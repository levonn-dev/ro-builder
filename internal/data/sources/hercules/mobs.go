package hercules

import (
	"fmt"
	"os"
)

// Mob is one row of Hercules's mob_db.conf, decoded into a typed struct.
// Field set covers identity + the combat stats rocalc's calc needs to
// score a build against this enemy: HP, Attack range, Def / Mdef, Race,
// Element (with level), Size. These also back the inline-stats path;
// when a server profile carries a custom mob the rocalc snapshot doesn't
// know about, the orchestrator passes the same fields directly to the
// shim, bypassing the m_Monster lookup.
//
// Hercules's mob_db has many more fields (Stats sub-object, Drops, MvP
// drops, AI mode flags, attack delay). They're parsed by ParseFile but
// we leave them as raw map values; promote into struct fields only when
// downstream needs them.
type Mob struct {
	ID         int    `json:"id" yaml:"id"`
	SpriteName string `json:"sprite_name" yaml:"sprite_name"`         // "PORING"; eAthena identifier, parallel to Item.AegisName
	Name       string `json:"name" yaml:"name"`                       // "Poring"; iRO display name
	JName      string `json:"jname,omitempty" yaml:"jname,omitempty"` // sometimes equals Name; sometimes a kRO name
	Lv         int    `json:"lv,omitempty" yaml:"lv,omitempty"`
	Hp         int    `json:"hp,omitempty" yaml:"hp,omitempty"`

	// Combat stats. AtkMin / AtkMax mirror Hercules's `Attack: [min, max]`
	// pair; rocalc treats mob attack as a range; both values matter.
	AtkMin    int    `json:"atk_min,omitempty" yaml:"atk_min,omitempty"`
	AtkMax    int    `json:"atk_max,omitempty" yaml:"atk_max,omitempty"`
	Def       int    `json:"def,omitempty" yaml:"def,omitempty"`
	MDef      int    `json:"mdef,omitempty" yaml:"mdef,omitempty"`
	Race      string `json:"race,omitempty" yaml:"race,omitempty"`             // "RC_Plant", "RC_Demon", "RC_Insect", etc.
	Element   string `json:"element,omitempty" yaml:"element,omitempty"`       // "Ele_Water", "Ele_Fire", etc.
	ElementLv int    `json:"element_lv,omitempty" yaml:"element_lv,omitempty"` // 1-4; used together with Element for resistance calc
	Size      string `json:"size,omitempty" yaml:"size,omitempty"`             // "Size_Small", "Size_Medium", "Size_Large"

	// Hand-authored threat tags layered on at catalog build time from
	// internal/catalog/data/mob_threats.yaml. Server profiles can override
	// or supplement inline on custom_mobs entries.
	//
	// AppliesStatus: status keys (data.Immunity*) the mob can inflict;
	// either via cast skills (cross-ref of mob_skill_db × skill_db's
	// StatusChange field) or via auto-attack proc rates (engine-baked,
	// known empirically for high-tier MVPs). Read by the gates evaluator's
	// status_immunity gate.
	//
	// CastsSkills: aegis-named skills the mob casts. Prompt context for
	// the LLM ("this MVP casts Storm Gust + Jupitel"); not consulted by
	// the gate logic directly.
	//
	// Pointer so unannotated mobs don't emit `{}` in catalog JSON; absent
	// == "no known threats" rather than "no threats."
	Threats *MobThreats `json:"threats,omitempty" yaml:"threats,omitempty"`
}

// MobThreats is the hand-authored gate-relevant threat profile of a mob.
type MobThreats struct {
	AppliesStatus []string `json:"applies_status,omitempty" yaml:"applies_status,omitempty"`
	CastsSkills   []string `json:"casts_skills,omitempty" yaml:"casts_skills,omitempty"`
}

// LoadMobDB reads a Hercules mob_db.conf file and returns one Mob per entry.
// Caller chooses the path; typically db/pre-re/mob_db.conf or db/re/mob_db.conf
// depending on the active mode.
func LoadMobDB(path string) ([]Mob, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	raw, err := ParseFile(src)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	mobs := make([]Mob, 0, len(raw))
	for i, e := range raw {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s entry %d: expected object, got %T", path, i, e)
		}
		mob, err := mobFromMap(m)
		if err != nil {
			return nil, fmt.Errorf("%s entry %d: %w", path, i, err)
		}
		mobs = append(mobs, mob)
	}
	return mobs, nil
}

func mobFromMap(m map[string]any) (Mob, error) {
	id, ok := m["Id"].(int)
	if !ok {
		return Mob{}, fmt.Errorf("missing or non-int Id (got %T)", m["Id"])
	}
	// Lv and Hp default to 1 per mob_db.conf's header doc ("Lv: level (int,
	// defaults to 1)" / "Hp: health (int, defaults to 1)"). Initialize
	// before reading so absent fields land at 1 instead of 0; the gates
	// evaluator and inline-stats path both treat 0 HP as "instantly dead",
	// which is wrong for mobs that simply omit the field.
	mob := Mob{ID: id, Lv: 1, Hp: 1}
	mob.SpriteName, _ = m["SpriteName"].(string)
	mob.Name, _ = m["Name"].(string)
	mob.JName, _ = m["JName"].(string)
	if v, ok := m["Lv"].(int); ok {
		mob.Lv = v
	}
	if v, ok := m["Hp"].(int); ok {
		mob.Hp = v
	}
	if v, ok := m["Def"].(int); ok {
		mob.Def = v
	}
	if v, ok := m["Mdef"].(int); ok {
		mob.MDef = v
	}
	mob.Race, _ = m["Race"].(string)
	mob.Size, _ = m["Size"].(string)
	// Hercules's `Attack: [min, max]` parses as []any{int, int}. Some
	// entries omit Attack entirely; we leave AtkMin/AtkMax at 0 in that
	// case.
	if arr, ok := m["Attack"].([]any); ok && len(arr) >= 2 {
		if v, ok := arr[0].(int); ok {
			mob.AtkMin = v
		}
		if v, ok := arr[1].(int); ok {
			mob.AtkMax = v
		}
	}
	// Hercules's `Element: ("Ele_X", N)` uses libconfig parens; parseList
	// returns []any{string, int}. Some entries omit Element entirely;
	// leave Element="" / ElementLv=0 there.
	if list, ok := m["Element"].([]any); ok && len(list) >= 2 {
		if s, ok := list[0].(string); ok {
			mob.Element = s
		}
		if v, ok := list[1].(int); ok {
			mob.ElementLv = v
		}
	}
	return mob, nil
}
