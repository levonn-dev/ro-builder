package gates

import (
	"github.com/levonn-dev/ro-builder/internal/data"
	"github.com/levonn-dev/ro-builder/internal/domain"
)

// effectiveStats returns the character's true post-gear/card/self-buff stats
// from the calc score. Reports false when no score is present (unscored
// snapshot); stat-reading gates skip in that case.
func effectiveStats(in Inputs) (domain.Stats, bool) {
	if in.Score == nil {
		return domain.Stats{}, false
	}
	return in.Score.Derived.TotalStats, true
}

// primaryAttackSkill resolves the snapshot's primary scored skill to its
// catalog record and allocated level. The primary is named semantically
// (e.g. "cold_bolt") in ScoredSkills; we match it to an allocated skill via
// the catalog's attack_skill.name. Reports false when no primary is
// designated or it cannot be resolved (e.g. unscored snapshot).
func primaryAttackSkill(in Inputs) (data.Skill, int, bool) {
	if in.Snapshot == nil || in.Catalog == nil {
		return data.Skill{}, 0, false
	}
	var name string
	for _, ss := range in.Snapshot.ScoredSkills {
		if ss.Primary {
			name = ss.Name
			break
		}
	}
	if name == "" {
		return data.Skill{}, 0, false
	}
	for _, sk := range in.Snapshot.Skills {
		if sk.Level <= 0 {
			continue
		}
		skill, ok := in.Catalog.Skill(sk.ID)
		if !ok {
			continue
		}
		if skill.AttackSkill != nil && skill.AttackSkill.Name == name {
			return skill, sk.Level, true
		}
	}
	return data.Skill{}, 0, false
}
