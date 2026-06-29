package hercules

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Skill is one row of Hercules's skill_db.conf. Field set is the
// LLM-relevant subset: identity (ID, AegisName, Name), max-level (so
// callers can validate skill levels in a build), AttackType / Element
// (the model uses these to reason about damage skills vs passives /
// supports), and cast/cooldown metadata (the gates evaluator uses these
// to derive uninterruptible-cast and primary-skill-rotation gates).
// Other skill_db fields (Range, HitCount, SkillType / SkillInfo flag
// groups, Requires, formula data) are intentionally omitted; add a
// field here and a line in skillFromMap when something downstream needs it.
//
// Note on field naming: Hercules's skill_db calls the human-readable label
// "Description" and the aegis identifier "Name". We follow Item's
// convention instead (AegisName for the script identifier, Name for the
// display label) so callers don't have to learn two naming schemes.
//
// Time fields are milliseconds at MAX skill level. Skill_db values can be
// either flat (`CastTime: 2000`) or per-level objects (`CastTime: { Lv1:
// 6000, Lv2: 7000, ... }`); we resolve to the max-level value and store
// a single int. Lower levels generally have shorter cast / cooldown, so
// max-level is the conservative bound the gate logic should reason against.
//
// Interruptible only carries meaning when CastTimeMs > 0. Hercules's
// InterruptCast field defaults to false (cast NOT interruptible by damage)
// when absent; rAthena's CastCancel defaults to true (cast IS interruptible).
// We store each source's own resolved truth; passive / instant skills may
// disagree across sources, but the gate evaluator only consults this field
// for cast-bearing skills, where both sources tend to specify explicitly.
type Skill struct {
	ID         int    `json:"id"`
	AegisName  string `json:"aegis_name"` // e.g. "SM_SWORD"
	Name       string `json:"name"`       // e.g. "Sword Mastery"
	MaxLevel   int    `json:"max_level"`
	AttackType string `json:"attack_type,omitempty"` // "Weapon", "Magic", "Misc", "" = passive/none
	Element    string `json:"element,omitempty"`     // "Ele_Neutral", "Ele_Fire", etc.; resolved to value at MaxLevel for grouped forms

	// Cast / cooldown metadata for the quality-gates evaluator. Milliseconds
	// at MaxLevel. 0 means "no cast / no cooldown".
	CastTimeMs  int `json:"cast_time_ms,omitempty"`  // variable cast (DEX-reducible in pre-re per cast = base * (150-DEX)/150)
	FixedCastMs int `json:"fixed_cast_ms,omitempty"` // fixed portion (uncommon in pre-re, present on a few skills)
	AfterCastMs int `json:"after_cast_ms,omitempty"` // post-cast delay (blocks all skills for X ms after the cast finishes)
	CooldownMs  int `json:"cooldown_ms,omitempty"`   // single-skill cooldown (blocks reuse of *this* skill)
	// CastTimeByLevelMs is the variable cast time at each level (index i =
	// level i+1), populated only when the source declares per-level cast
	// (the bolts, Storm Gust, Meteor, etc.). nil for flat-cast skills, which
	// use the scalar CastTimeMs at every level. Kept sparse to bound the
	// embedded catalog's size.
	CastTimeByLevelMs []int `json:"cast_time_by_level_ms,omitempty"`
	Interruptible     bool  `json:"interruptible,omitempty"` // cast cancellable by damage; only meaningful when CastTimeMs > 0

	// StatusChange is the SC_X identifier of the status the skill applies
	// when it lands ("SC_FREEZE" for WZ_STORMGUST, "SC_STUN" for
	// NPC_STUNATTACK, etc.). Empty when the skill applies no status.
	// Populated from Hercules's skill_db.conf (rAthena pre-re doesn't
	// expose this field cleanly so it's left blank when -source=rathena).
	// Used by cmd/build-catalog to compute MobThreats.AppliesStatus by
	// joining mob_skill_db × skill catalog. Surfaced via lookup_skill so
	// the LLM can see which status each skill applies.
	StatusChange string `json:"status_change,omitempty"`

	// SelfBuff is hand-authored self-buff metadata (Mild Wind / Spurt /
	// Ranker etc.) layered on at catalog build time from
	// internal/catalog/data/skill_buffs.yaml. nil for the vast majority of
	// skills, which are not drivable self-buffs. Not emulator-derived, so
	// not set in skillFromMap.
	SelfBuff *SelfBuff `json:"self_buff,omitempty"`

	// AttackSkill is hand-authored attack-skill metadata (Tornado Kick etc.)
	// layered on at catalog build time from
	// internal/catalog/data/attack_skills.yaml. nil for non-scoreable skills.
	// Not emulator-derived, so not set in skillFromMap.
	AttackSkill *AttackSkill `json:"attack_skill,omitempty"`
}

// LoadSkillDB reads a Hercules skill_db.conf and returns one Skill per
// entry. Mirrors LoadItemDB / LoadMobDB.
func LoadSkillDB(path string) ([]Skill, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	raw, err := ParseFile(src)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	skills := make([]Skill, 0, len(raw))
	for i, e := range raw {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s entry %d: expected object, got %T", path, i, e)
		}
		s, err := skillFromMap(m)
		if err != nil {
			return nil, fmt.Errorf("%s entry %d: %w", path, i, err)
		}
		skills = append(skills, s)
	}
	return skills, nil
}

func skillFromMap(m map[string]any) (Skill, error) {
	id, ok := m["Id"].(int)
	if !ok {
		return Skill{}, fmt.Errorf("missing or non-int Id (got %T)", m["Id"])
	}
	s := Skill{ID: id}
	s.AegisName, _ = m["Name"].(string)
	s.Name, _ = m["Description"].(string)
	if v, ok := m["MaxLevel"].(int); ok {
		s.MaxLevel = v
	}
	s.AttackType, _ = m["AttackType"].(string)
	// Element may be a flat string ("Ele_Fire") or a per-level grouped
	// object ({Lv1: "Ele_Fire", Lv2: "Ele_Wind", ...}); resolve grouped
	// forms to the MaxLevel value so the LLM sees a meaningful element on
	// skills like Jupitel Thunder whose element shifts per level.
	s.Element = stringAtMaxLevel(m["Element"], s.MaxLevel)

	// Cast / cooldown numerics; flat int OR { Lv1: N, Lv2: N, ... }.
	// Pick the value at MaxLevel; intAtMaxLevel handles both shapes.
	s.CastTimeMs = intAtMaxLevel(m["CastTime"], s.MaxLevel)
	s.CastTimeByLevelMs = intSliceByLevel(m["CastTime"], s.MaxLevel)
	s.FixedCastMs = intAtMaxLevel(m["FixedCastTime"], s.MaxLevel)
	s.AfterCastMs = intAtMaxLevel(m["AfterCastActDelay"], s.MaxLevel)
	s.CooldownMs = intAtMaxLevel(m["CoolDown"], s.MaxLevel)

	// Hercules's InterruptCast: default false when absent (= cast NOT
	// interrupted by damage). Explicit `true` means interruptible. Per
	// the field doc in skill_db.conf header.
	if v, ok := m["InterruptCast"].(bool); ok {
		s.Interruptible = v
	}

	// StatusChange: "SC_FREEZE" / "SC_STUN" / etc. Some status-applying
	// skills (e.g. NPC_POISONATTACK) don't have this field; they apply
	// element-based status via different machinery; leave blank in that
	// case.
	s.StatusChange, _ = m["StatusChange"].(string)

	return s, nil
}

// CastAtLevelMs returns the variable cast time at the given level. Uses the
// per-level table when present; otherwise falls back to the scalar
// CastTimeMs (flat-cast skills). Level is clamped to [1, len].
func (s Skill) CastAtLevelMs(level int) int {
	if len(s.CastTimeByLevelMs) == 0 {
		return s.CastTimeMs
	}
	if level < 1 {
		level = 1
	}
	if level > len(s.CastTimeByLevelMs) {
		level = len(s.CastTimeByLevelMs)
	}
	return s.CastTimeByLevelMs[level-1]
}

// intSliceByLevel resolves a Hercules skill_db numeric field to a per-level
// slice (index i = level i+1) when it is a grouped object
// (`{ Lv1: 700, Lv2: 1400, ... }`). Returns nil for a flat int or absent
// field, signaling "no per-level variation; use the scalar".
func intSliceByLevel(v any, maxLevel int) []int {
	t, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	n := maxLevel
	// Defensive: grow to the highest LvN present if it exceeds maxLevel.
	for k := range t {
		if !strings.HasPrefix(k, "Lv") {
			continue
		}
		if lv, err := strconv.Atoi(k[2:]); err == nil && lv > n {
			n = lv
		}
	}
	if n <= 0 {
		return nil
	}
	out := make([]int, n)
	populated := false
	for i := 0; i < n; i++ {
		if iv, ok := t[fmt.Sprintf("Lv%d", i+1)].(int); ok {
			out[i] = iv
			populated = true
		} else if i > 0 {
			out[i] = out[i-1] // carry forward sparse gaps
		}
	}
	if !populated {
		return nil
	}
	return out
}

// stringAtMaxLevel mirrors intAtMaxLevel for string-typed fields
// (specifically Element, which can be flat "Ele_Fire" or grouped
// {Lv1: "Ele_Fire", Lv2: "Ele_Wind", ...}). Returns "" when absent or
// resolvable to no value at any level.
func stringAtMaxLevel(v any, maxLevel int) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case map[string]any:
		if maxLevel > 0 {
			if sv, ok := t[fmt.Sprintf("Lv%d", maxLevel)].(string); ok {
				return sv
			}
		}
		bestLv := 0
		bestVal := ""
		for k, raw := range t {
			if !strings.HasPrefix(k, "Lv") {
				continue
			}
			lv, err := strconv.Atoi(k[2:])
			if err != nil {
				continue
			}
			sv, ok := raw.(string)
			if !ok {
				continue
			}
			if lv > bestLv {
				bestLv = lv
				bestVal = sv
			}
		}
		return bestVal
	}
	return ""
}

// intAtMaxLevel resolves a Hercules skill_db numeric field that can be
// either a flat int (`CastTime: 2000`) or a per-level grouped object
// (`CastTime: { Lv1: 6000, Lv2: 7000, ... }`) to the value at maxLevel.
//
// Resolution order for grouped form:
//  1. Look up `Lv{maxLevel}` directly.
//  2. Fall back to the highest LvN key present.
//  3. Return 0 if neither found.
//
// Flat int returns as-is. Anything else (nil / wrong type) returns 0.
func intAtMaxLevel(v any, maxLevel int) int {
	switch t := v.(type) {
	case nil:
		return 0
	case int:
		return t
	case map[string]any:
		// Direct hit at the requested level.
		if maxLevel > 0 {
			if iv, ok := t[fmt.Sprintf("Lv%d", maxLevel)].(int); ok {
				return iv
			}
		}
		// Fallback: scan for the highest LvN entry actually present.
		bestLv := 0
		bestVal := 0
		for k, raw := range t {
			if !strings.HasPrefix(k, "Lv") {
				continue
			}
			lv, err := strconv.Atoi(k[2:])
			if err != nil {
				continue
			}
			iv, ok := raw.(int)
			if !ok {
				continue
			}
			if lv > bestLv {
				bestLv = lv
				bestVal = iv
			}
		}
		return bestVal
	}
	return 0
}
