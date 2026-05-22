package rathena

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"

	"github.com/levonn-dev/ro-builder/internal/data/sources/hercules"
)

// Skill is the same shape hercules.Skill exposes; re-exported here so
// callers who only know the rathena package can use Skill without
// touching hercules. rAthena calls the script identifier "Name" and the
// human label "Description"; we normalize to hercules.Skill's
// AegisName / Name pair.
type Skill = hercules.Skill

type rawSkill struct {
	ID          int    `yaml:"Id"`
	Name        string `yaml:"Name"`        // aegis identifier, e.g. "SM_SWORD"
	Description string `yaml:"Description"` // human label, e.g. "Sword Mastery"
	MaxLevel    int    `yaml:"MaxLevel"`
	// Type maps to Hercules's AttackType. rAthena values: "None", "Weapon",
	// "Magic", "Misc". Hercules wraps with "Weapon"/"Magic"/"Misc"/"None";
	// same set, no normalization needed.
	Type string `yaml:"Type"`
	// Element may be a flat string ("Fire") OR a sequence of {Level: N,
	// Element: X} objects. We currently capture only the flat form (matches
	// hercules's parser; level-grouped variants stay empty). yaml.v2's
	// untyped any catches both shapes without panicking.
	Element any `yaml:"Element"`

	// Cast / cooldown metadata. Each is either a flat int (`CastTime: 2000`)
	// or a sequence of {Level: N, Time: M} objects; yaml.v2's untyped any
	// handles both. Resolved to MaxLevel value during conversion.
	CastTime          any `yaml:"CastTime"`
	FixedCastTime     any `yaml:"FixedCastTime"`
	AfterCastActDelay any `yaml:"AfterCastActDelay"`
	Cooldown          any `yaml:"Cooldown"`

	// CastCancel maps to Hercules's InterruptCast inverse-default-wise:
	// rAthena's docs state "CastCancel; Cancel cast when hit. (Default:
	// true)". So absent means cast IS interruptible by damage.
	// *bool distinguishes nil (absent → true) from explicit false (false).
	CastCancel *bool `yaml:"CastCancel"`
}

type rawSkillFile struct {
	Header struct {
		Type    string `yaml:"Type"`
		Version int    `yaml:"Version"`
	} `yaml:"Header"`
	Body []rawSkill `yaml:"Body"`
}

// LoadSkillDB reads rAthena's per-mode skill_db.yml and returns one Skill
// per row. modeDir is "pre-re" or "re", same convention as Hercules.
func LoadSkillDB(rathenaRoot, modeDir string) ([]Skill, error) {
	path := filepath.Join(rathenaRoot, "db", modeDir, "skill_db.yml")
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var file rawSkillFile
	if err := yaml.Unmarshal(src, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := make([]Skill, 0, len(file.Body))
	for _, raw := range file.Body {
		element, _ := raw.Element.(string) // ignore non-string (level-grouped) Element values for now

		// CastCancel default: absent → true (interruptible) per rAthena docs.
		interruptible := true
		if raw.CastCancel != nil {
			interruptible = *raw.CastCancel
		}

		out = append(out, Skill{
			ID:         raw.ID,
			AegisName:  raw.Name,
			Name:       raw.Description,
			MaxLevel:   raw.MaxLevel,
			AttackType: raw.Type,
			// rAthena Element values like "Fire" are bare; Hercules uses
			// "Ele_Fire". Normalize to Hercules's prefix so cross-source
			// comparison is direct.
			Element: normalizeElement(element),

			CastTimeMs:    intAtMaxLevel(raw.CastTime, raw.MaxLevel),
			FixedCastMs:   intAtMaxLevel(raw.FixedCastTime, raw.MaxLevel),
			AfterCastMs:   intAtMaxLevel(raw.AfterCastActDelay, raw.MaxLevel),
			CooldownMs:    intAtMaxLevel(raw.Cooldown, raw.MaxLevel),
			Interruptible: interruptible,
		})
	}
	return out, nil
}

// intAtMaxLevel resolves a rAthena skill_db numeric field to its MaxLevel
// value. rAthena uses two shapes: a flat int (`CastTime: 2000`) or a
// sequence of objects (`CastTime: [{Level: 1, Time: 6000}, ...]`).
//
// Resolution order for sequence form:
//  1. Find an entry with Level == maxLevel; return its Time.
//  2. Fall back to the entry with the highest Level seen.
//  3. Return 0 if neither found.
//
// Flat int returns as-is. yaml.v2 unmarshals untyped sequences as
// []interface{} and untyped maps as map[interface{}]interface{}; we
// handle both that and the typed map[string]interface{} shape some
// callers may produce.
func intAtMaxLevel(v any, maxLevel int) int {
	switch t := v.(type) {
	case nil:
		return 0
	case int:
		return t
	case []any:
		bestLv := 0
		bestTime := 0
		for _, raw := range t {
			lv, time, ok := levelTimeEntry(raw)
			if !ok {
				continue
			}
			if maxLevel > 0 && lv == maxLevel {
				return time
			}
			if lv > bestLv {
				bestLv = lv
				bestTime = time
			}
		}
		return bestTime
	}
	return 0
}

// levelTimeEntry pulls Level / Time ints out of one element of a rAthena
// per-level sequence. yaml.v2 may unmarshal the entry as either
// map[interface{}]interface{} (untyped) or map[string]interface{}
// (typed); we accept both. Returns (0, 0, false) if either field is
// missing or non-int.
func levelTimeEntry(raw any) (lv, time int, ok bool) {
	switch m := raw.(type) {
	case map[any]any:
		l, lOk := m["Level"].(int)
		t, tOk := m["Time"].(int)
		return l, t, lOk && tOk
	case map[string]any:
		l, lOk := m["Level"].(int)
		t, tOk := m["Time"].(int)
		return l, t, lOk && tOk
	}
	return 0, 0, false
}

// normalizeElement prepends "Ele_" to bare rAthena element names so they
// match Hercules's "Ele_Neutral" / "Ele_Fire" / etc. convention. Empty
// stays empty; already-prefixed values pass through.
func normalizeElement(e string) string {
	if e == "" {
		return ""
	}
	if len(e) >= 4 && e[:4] == "Ele_" {
		return e
	}
	return "Ele_" + e
}
