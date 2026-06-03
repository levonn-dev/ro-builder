package tools

import (
	"errors"
	"fmt"

	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/data"
	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// errBuffResolve tags every error resolveBuffs returns so callers can
// classify a buff-resolution failure as caller-fixable input (the LLM
// declared a buff it can't have) rather than an infrastructure failure. The
// alternatives loop in submit_trajectory uses errors.Is to drop just the
// offending alternative instead of aborting the submission.
var errBuffResolve = errors.New("buff resolution")

// resolveBuffs turns a snapshot's declared ActiveBuffs into fully-specified
// scoring.Buffs: it fills each buff's level from the anchor skill's allocated
// level and validates that the buff is available to the class, its anchor is
// allocated, and (for endow buffs) the chosen element is within level. Returns
// a caller-fixable error wrapping errBuffResolve on any mismatch. nil
// activeBuffs -> nil, nil. A nil catalog with buffs present is an error (can't
// resolve). Takes the snapshot's fields rather than the snapshot so score_build
// (which scores a Build, not a Snapshot) can resolve buffs the same way.
func resolveBuffs(class string, skills []domain.SkillAlloc, activeBuffs []domain.ActiveBuff, cat *catalog.Catalog) ([]scoring.Buff, error) {
	if len(activeBuffs) == 0 {
		return nil, nil
	}
	if cat == nil {
		return nil, fmt.Errorf("%w: cannot resolve active_buffs without a catalog (scoring misconfigured)", errBuffResolve)
	}
	available, ok := cat.ClassBuffs(class)
	if !ok {
		return nil, fmt.Errorf("%w: class %q not found in catalog", errBuffResolve, class)
	}
	// Keyed by the buff's semantic name (e.g. "mild_wind"), the value the LLM
	// puts in ActiveBuff.Name, not the anchor skill's aegis name.
	byName := make(map[string]catalog.ClassSkill, len(available))
	for _, b := range available {
		byName[b.SelfBuff.Name] = b
	}
	allocated := make(map[int]int, len(skills)) // skill id -> level
	for _, sk := range skills {
		allocated[sk.ID] = sk.Level
	}

	out := make([]scoring.Buff, 0, len(activeBuffs))
	for _, ab := range activeBuffs {
		def, ok := byName[ab.Name]
		if !ok {
			return nil, fmt.Errorf("%w: buff %q is not available to class %q", errBuffResolve, ab.Name, class)
		}
		level, ok := allocated[def.ID]
		if !ok || level < 1 {
			return nil, fmt.Errorf("%w: buff %q requires skill %s allocated (>=1) on this snapshot", errBuffResolve, ab.Name, def.AegisName)
		}
		if def.SelfBuff.Kind == data.BuffKindWeaponEndow {
			if err := validateEndowElement(ab, def, level); err != nil {
				return nil, err
			}
		} else if ab.Element != "" {
			return nil, fmt.Errorf("%w: buff %q is %s and takes no element", errBuffResolve, ab.Name, def.SelfBuff.Kind)
		}
		out = append(out, scoring.Buff{Name: ab.Name, Level: level, Element: ab.Element})
	}
	return out, nil
}

// validateEndowElement checks the chosen element is one the buff offers and
// its 1-based position in endow.elements is <= the resolved level.
func validateEndowElement(ab domain.ActiveBuff, def catalog.ClassSkill, level int) error {
	if ab.Element == "" {
		return fmt.Errorf("%w: buff %q (weapon_endow) requires an element", errBuffResolve, ab.Name)
	}
	order := def.SelfBuff.Endow.Elements
	for i, el := range order {
		if el == ab.Element {
			if i+1 > level {
				return fmt.Errorf("%w: buff %q element %q needs level %d but skill is allocated at %d", errBuffResolve, ab.Name, ab.Element, i+1, level)
			}
			return nil
		}
	}
	return fmt.Errorf("%w: buff %q does not offer element %q (offers: %v)", errBuffResolve, ab.Name, ab.Element, order)
}
