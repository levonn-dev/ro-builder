package hercules

// AttackSkill is hand-authored attack-skill metadata layered onto a skill
// record at catalog build time from internal/catalog/data/attack_skills.yaml.
// Marks a skill as a scoreable damage skill. Co-located with the skill it
// describes (Tornado Kick -> TK_STORMKICK). Lives in this package for the same
// import-cycle reason as SelfBuff (data.Skill = hercules.Skill aliases this
// struct's owner). Carries semantic facts only (no engine ids); the sidecar's
// binding table translates Name -> engine controls behind the shim boundary.
type AttackSkill struct {
	Name string `json:"name" yaml:"name"` // semantic key: "tornado_kick", "sonic_blow"
}
