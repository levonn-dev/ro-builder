package hercules

// SelfBuff is hand-authored self-buff metadata layered onto a skill record
// at catalog build time from internal/catalog/data/skill_buffs.yaml. Co-located
// with the skill the buff comes from (Mild Wind -> TK_SEVENWIND, Spurt ->
// TK_RUN, Taekwon Ranker -> TK_MISSION). Lives in this package (not internal/
// data) because data.Skill = hercules.Skill aliases this struct's owner;
// defining SelfBuff here and aliasing it into data avoids an import cycle.
//
// Carries semantic facts only (no rocalc ids): the sidecar's rocalc binding
// table translates Name -> engine controls behind the shim boundary.
type SelfBuff struct {
	Name        string     `json:"name" yaml:"name"`                       // semantic key: "mild_wind", "spurt", "taekwon_ranker"
	Kind        string     `json:"kind" yaml:"kind"`                       // weapon_endow | stat_buff | status
	Persistence string     `json:"persistence" yaml:"persistence"`         // permanent | transient (reserved for SP/sustain; not gate logic)
	Endow       *EndowSpec `json:"endow,omitempty" yaml:"endow,omitempty"` // nil for stat_buff / status kinds
}

// EndowSpec is present only on weapon_endow buffs. Elements is ordered; the
// chosen element's 1-based index must be <= the buff's resolved level (Mild
// Wind: earth..holy by level). A fixed-element endow lists exactly one.
type EndowSpec struct {
	Elements []string `json:"elements" yaml:"elements"`
}
