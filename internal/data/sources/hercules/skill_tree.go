package hercules

import (
	"fmt"
	"os"
	"sort"
)

// ClassSkillTree is one job's allocatable skill list. Inherits captures
// the chain of base classes whose skills also become allocatable
// (Novice → Swordsman → Knight, etc.); cmd/build-catalog flattens the
// tree at build time so the Catalog stores a fully-expanded set per
// class and runtime callers don't redo the recursion.
//
// Skills here are the player's allocatable tree; different from the
// calc engine's job-buff bank (the calc's runtime buff lookup, not
// the skill tree).
type ClassSkillTree struct {
	Name    string            `json:"name"`
	Inherit []string          `json:"inherit,omitempty"`
	Skills  []ClassSkillEntry `json:"skills"`
}

// ClassSkillEntry is one row of the tree: skill aegis name, max level
// the class can invest, and the prerequisite skills that must be
// allocated before this skill can be learned. Requires maps aegis name
// → required level; empty for skills with no prerequisites.
//
// the calc engine silently skips skills whose prerequisites are not met rather
// than returning an error, so the LLM must verify prereqs itself.
// Carrying them here lets the orchestrator surface them in the user
// prompt so the LLM doesn't have to rediscover the dependency.
type ClassSkillEntry struct {
	AegisName string         `json:"aegis_name"`
	MaxLevel  int            `json:"max_level"`
	Requires  map[string]int `json:"requires,omitempty"` // aegis_name → required level
	// Unusable marks a skill the class carries in its tree (inherited, with
	// points spent in an earlier job) but cannot actually USE in this job -- e.g.
	// the Taekwon kicks a Soul Linker keeps but can't cast. NOT from Hercules;
	// set by cmd/build-catalog. Kept in the tree so the skill still counts toward
	// the skill-point budget and skill monotonicity (the points were really
	// spent), but excluded from scoreable attacks and usable buffs and annotated
	// for the LLM. Default false (usable).
	Unusable bool `json:"unusable,omitempty"`
}

// LoadSkillTreeDB reads a Hercules skill_tree.conf and returns one
// ClassSkillTree per Job entry. Path is typically
// db/pre-re/skill_tree.conf.
//
// Each entry's skills map can have two value shapes:
//
//	SKILL: 10                 → simple max-level int; no prerequisites
//	SKILL: { MaxLevel: N, ... } → object form; every non-MaxLevel integer
//	                             field is a prerequisite: { PREREQ: minLevel }
//
// We extract MaxLevel and all prerequisites from the object form.
func LoadSkillTreeDB(path string) ([]ClassSkillTree, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	// skill_tree.conf is libconfig but with one quirk: the top level is
	// a series of named entries (Job_Name: { ... }), not a list. ParseFile
	// expects a list root, so wrap-and-fall-through doesn't apply here.
	// We use the lower-level tokenizer / parser primitives directly.
	t := newTokenizer(src)
	classes := []ClassSkillTree{}
	for {
		tk, err := t.peek()
		if err != nil {
			return nil, err
		}
		if tk.typ == tokEOF {
			break
		}
		if tk.typ != tokIdent {
			return nil, fmt.Errorf("%s: expected job name, got %s", path, tk)
		}
		nameTok, _ := t.next()
		if _, err := t.expect(tokColon); err != nil {
			return nil, err
		}
		val, err := parseValue(t)
		if err != nil {
			return nil, err
		}
		obj, ok := val.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: %s value should be an object, got %T", path, nameTok.val, val)
		}
		entry, err := classFromMap(nameTok.val, obj)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", path, nameTok.val, err)
		}
		classes = append(classes, entry)
	}
	return classes, nil
}

func classFromMap(name string, m map[string]any) (ClassSkillTree, error) {
	out := ClassSkillTree{Name: name}
	if raw, ok := m["inherit"]; ok {
		// inherit is a libconfig parenthesized list of strings →
		// parseList returns []any{string, string, ...}.
		if list, ok := raw.([]any); ok {
			for _, v := range list {
				if s, ok := v.(string); ok {
					out.Inherit = append(out.Inherit, s)
				}
			}
		}
	}
	if raw, ok := m["skills"]; ok {
		skillsMap, ok := raw.(map[string]any)
		if !ok {
			return out, fmt.Errorf("skills should be an object, got %T", raw)
		}
		for skillName, v := range skillsMap {
			maxLevel, requires, ok := extractSkillEntry(v)
			if !ok {
				return out, fmt.Errorf("skill %s: cannot extract MaxLevel from %T", skillName, v)
			}
			entry := ClassSkillEntry{
				AegisName: skillName,
				MaxLevel:  maxLevel,
			}
			if len(requires) > 0 {
				entry.Requires = requires
			}
			out.Skills = append(out.Skills, entry)
		}
	}
	// skillsMap is a Go map, whose range order is randomized; sort by aegis
	// name so cmd/build-catalog emits a reproducible skill list every run.
	sort.Slice(out.Skills, func(i, j int) bool {
		return out.Skills[i].AegisName < out.Skills[j].AegisName
	})
	return out, nil
}

// extractSkillEntry pulls the max-level and prerequisite map out of a
// skill_tree.conf value, which is either:
//
//	int   → simple form (SKILL: 10); no prerequisites
//	map   → object form (SKILL: { MaxLevel: 10, PREREQ_A: 1, PREREQ_B: 3 })
//
// In the map form, every non-MaxLevel integer field is a prerequisite:
// the player must have allocated that skill at ≥ the given level before
// this skill can be learned.
func extractSkillEntry(v any) (maxLevel int, requires map[string]int, ok bool) {
	switch t := v.(type) {
	case int:
		return t, nil, true
	case map[string]any:
		iv, hasMax := t["MaxLevel"].(int)
		if !hasMax {
			return 0, nil, false
		}
		reqs := make(map[string]int)
		for k, rv := range t {
			if k == "MaxLevel" {
				continue
			}
			if lv, isInt := rv.(int); isInt {
				reqs[k] = lv
			}
		}
		if len(reqs) == 0 {
			reqs = nil
		}
		return iv, reqs, true
	}
	return 0, nil, false
}
