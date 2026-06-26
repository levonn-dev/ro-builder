package tools

import (
	"errors"
	"fmt"

	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// errAttackSkillResolve tags every error resolveAttackSkills returns so the
// submit_trajectory alternatives loop can drop just the offending alternative
// (caller-fixable input) rather than aborting the submission.
var errAttackSkillResolve = errors.New("attack skill resolution")

// resolveAttackSkills turns a snapshot's declared ScoredSkills into
// fully-specified scoring.AttackSkills: it fills each skill's level from its
// own allocation and validates the skill is available to the class, allocated,
// and that exactly one declared skill is primary. nil declared -> nil, nil.
// Mirrors resolveBuffs.
func resolveAttackSkills(class string, skills []domain.SkillAlloc, declared []domain.ScoredSkill, cat *catalog.Catalog) ([]scoring.AttackSkill, error) {
	if len(declared) == 0 {
		return nil, nil
	}
	if cat == nil {
		return nil, fmt.Errorf("%w: cannot resolve scored_skills without a catalog (scoring misconfigured)", errAttackSkillResolve)
	}
	available, ok := cat.ClassAttackSkills(class)
	if !ok {
		return nil, fmt.Errorf("%w: class %q not found in catalog", errAttackSkillResolve, class)
	}
	byName := make(map[string]catalog.ClassSkill, len(available))
	for _, s := range available {
		byName[s.AttackSkill.Name] = s
	}
	allocated := make(map[int]int, len(skills))
	for _, sk := range skills {
		allocated[sk.ID] = sk.Level
	}

	primaries := 0
	out := make([]scoring.AttackSkill, 0, len(declared))
	seen := make(map[string]bool, len(declared))
	for _, d := range declared {
		if seen[d.Name] {
			return nil, fmt.Errorf("%w: skill %q declared more than once", errAttackSkillResolve, d.Name)
		}
		seen[d.Name] = true
		def, ok := byName[d.Name]
		if !ok {
			return nil, fmt.Errorf("%w: skill %q is not a scoreable attack skill for class %q", errAttackSkillResolve, d.Name, class)
		}
		level, ok := allocated[def.ID]
		if !ok || level < 1 {
			return nil, fmt.Errorf("%w: skill %q requires its anchor %s allocated (>=1) on this snapshot", errAttackSkillResolve, d.Name, def.AegisName)
		}
		if d.Primary {
			primaries++
		}
		out = append(out, scoring.AttackSkill{Name: d.Name, Level: level, Primary: d.Primary})
	}
	if primaries != 1 {
		return nil, fmt.Errorf("%w: exactly one scored skill must be primary, got %d", errAttackSkillResolve, primaries)
	}
	return out, nil
}
