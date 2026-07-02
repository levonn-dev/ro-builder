package api

import (
	"time"

	"github.com/levonn-dev/ro-builder/internal/buildlibrary"
	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// NamedRef is the resolved-id form: every numeric id reference in the
// snapshot body is rendered as {id, name} so consumers don't need a
// separate catalog lookup. Name is empty when the catalog has no record
// for id, or when no catalog is configured.
type NamedRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// EnrichedEquipSpec is the wire form of an equipment slot once item and
// card ids have been resolved.
type EnrichedEquipSpec struct {
	Item   NamedRef   `json:"item"`
	Refine int        `json:"refine,omitempty"`
	Cards  []NamedRef `json:"cards,omitempty"`
}

// EnrichedSkillAlloc is flat (id, name, level); the alloc *is* the
// skill, not a reference to one.
type EnrichedSkillAlloc struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Level int    `json:"level"`
}

// EnrichedLevelingTarget mirrors domain.Scenario but with the mob id
// resolved.
type EnrichedLevelingTarget struct {
	Target *NamedRef `json:"target,omitempty"`
}

// EnrichedSnapshot is the wire shape of a snapshot in GetBuild responses.
// Differs from domain.Snapshot only in the three id-bearing fields
// (Skills, Equipment, LevelingTarget).
type EnrichedSnapshot struct {
	Class          string                               `json:"class"`
	Level          domain.Level                         `json:"level"`
	PostRebirth    bool                                 `json:"post_rebirth,omitempty"`
	Stats          domain.Stats                         `json:"stats"`
	Skills         []EnrichedSkillAlloc                 `json:"skills,omitempty"`
	Equipment      map[domain.SlotKey]EnrichedEquipSpec `json:"equipment,omitempty"`
	LevelingTarget *EnrichedLevelingTarget              `json:"leveling_target,omitempty"`
	Score          *scoring.ScoreResponse               `json:"score,omitempty"`
	Gates          []domain.GateResult                  `json:"gates,omitempty"`
	Notes          string                               `json:"notes,omitempty"`
}

// EnrichedTrajectory is the wire shape of a trajectory in GetBuild
// responses.
type EnrichedTrajectory struct {
	Class     string             `json:"class"`
	Snapshots []EnrichedSnapshot `json:"snapshots"`
}

// EnrichedBuild is the GET /builds/{id} response body.
type EnrichedBuild struct {
	ID             string                   `json:"id"`
	CreatedAt      time.Time                `json:"created_at"`
	Class          string                   `json:"class"`
	Server         string                   `json:"server"`
	Playstyle      string                   `json:"playstyle"`
	Mode           string                   `json:"mode"`
	Description    string                   `json:"description,omitempty"`
	Primary        EnrichedTrajectory       `json:"primary"`
	Alternatives   []EnrichedTrajectory     `json:"alternatives,omitempty"`
	FinalText      string                   `json:"final_text,omitempty"`
	GateSummary    buildlibrary.GateSummary `json:"gate_summary"`
	CalcVersion    string                   `json:"calc_version,omitempty"`
	CatalogVersion int                      `json:"catalog_version,omitempty"`
	AcceptedAt     *time.Time               `json:"accepted_at,omitempty"`
}

// enrichSavedTrajectory transforms a SavedTrajectory into its wire form
// for GET /builds/{id}: numeric ids in each snapshot's equipment, skills,
// and leveling_target are resolved against the catalog and rendered as
// {id, name} pairs inline. A nil catalog yields the same structure with
// every name field set to the empty string; the schema does not change
// on the cold path.
func enrichSavedTrajectory(st *buildlibrary.SavedTrajectory, cat *catalog.Catalog) *EnrichedBuild {
	out := &EnrichedBuild{
		ID:             st.ID,
		CreatedAt:      st.CreatedAt,
		Class:          st.Class,
		Server:         st.Server,
		Playstyle:      st.Playstyle,
		Mode:           st.Mode,
		Description:    st.Description,
		FinalText:      st.FinalText,
		GateSummary:    st.GateSummary,
		CalcVersion:    st.CalcVersion,
		CatalogVersion: st.CatalogVersion,
		AcceptedAt:     st.AcceptedAt,
		Primary:        enrichTrajectory(st.Primary, cat),
	}
	if len(st.Alternatives) > 0 {
		out.Alternatives = make([]EnrichedTrajectory, len(st.Alternatives))
		for i := range st.Alternatives {
			out.Alternatives[i] = enrichTrajectory(st.Alternatives[i], cat)
		}
	}
	return out
}

func enrichTrajectory(t domain.Trajectory, cat *catalog.Catalog) EnrichedTrajectory {
	out := EnrichedTrajectory{
		Class:     t.Class,
		Snapshots: make([]EnrichedSnapshot, len(t.Snapshots)),
	}
	for i := range t.Snapshots {
		out.Snapshots[i] = enrichSnapshot(t.Snapshots[i], cat)
	}
	return out
}

func enrichSnapshot(s domain.Snapshot, cat *catalog.Catalog) EnrichedSnapshot {
	out := EnrichedSnapshot{
		Class:       s.Class,
		Level:       s.Level,
		PostRebirth: s.PostRebirth,
		Stats:       s.Stats,
		Score:       s.Score,
		Gates:       s.Gates,
		Notes:       s.Notes,
	}
	if len(s.Skills) > 0 {
		out.Skills = make([]EnrichedSkillAlloc, len(s.Skills))
		for i, sk := range s.Skills {
			out.Skills[i] = EnrichedSkillAlloc{
				ID:    sk.ID,
				Name:  resolveSkillName(cat, sk.ID),
				Level: sk.Level,
			}
		}
	}
	if len(s.Equipment) > 0 {
		out.Equipment = make(map[domain.SlotKey]EnrichedEquipSpec, len(s.Equipment))
		for slot, spec := range s.Equipment {
			eq := EnrichedEquipSpec{
				Item:   NamedRef{ID: spec.ID, Name: resolveItemName(cat, spec.ID)},
				Refine: spec.Refine,
			}
			if len(spec.Cards) > 0 {
				eq.Cards = make([]NamedRef, len(spec.Cards))
				for i, cid := range spec.Cards {
					eq.Cards[i] = NamedRef{ID: cid, Name: resolveItemName(cat, cid)}
				}
			}
			out.Equipment[slot] = eq
		}
	}
	if s.LevelingTarget != nil {
		lt := &EnrichedLevelingTarget{}
		if s.LevelingTarget.Target != 0 {
			lt.Target = &NamedRef{
				ID:   s.LevelingTarget.Target,
				Name: resolveMobName(cat, s.LevelingTarget.Target),
			}
		}
		out.LevelingTarget = lt
	}
	return out
}

func resolveItemName(cat *catalog.Catalog, id int) string {
	if cat == nil || id == 0 {
		return ""
	}
	if item, ok := cat.Item(id); ok {
		return item.Name
	}
	return ""
}

func resolveSkillName(cat *catalog.Catalog, id int) string {
	if cat == nil || id == 0 {
		return ""
	}
	if s, ok := cat.Skill(id); ok {
		return s.Name
	}
	return ""
}

func resolveMobName(cat *catalog.Catalog, id int) string {
	if cat == nil || id == 0 {
		return ""
	}
	if m, ok := cat.Mob(id); ok {
		return m.Name
	}
	return ""
}
