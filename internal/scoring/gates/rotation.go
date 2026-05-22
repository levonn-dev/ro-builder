package gates

// evaluateRotation: per-skill ASPD-vs-cooldown gate. Replaces the
// rejected per-archetype "ASPD floor" table with a derived check.
//
// The empirical breakpoints from ref_docs (SinX MA caps at 175 ASPD
// because its 0.5s cooldown locks rotation; SinX SD floor 175; LK BB
// floor ~190 with Berserk) all come from "primary skill cooldown vs
// player attack interval" math. The gate evaluates that math directly:
// if a skill's effective rotation is materially slower than its
// cooldown allows, the build is under-spamming.
//
// NOTE: implementing this well needs cooldown data the catalog now
// carries (CooldownMs) plus knowledge of which skill is the build's
// "primary damage skill"; and that's a derivation problem (highest
// damage * frequency among the allocated skills). Currently this ships
// a SKELETON gate that emits no results; the data substrate is in place
// (catalog.Skill().CooldownMs is populated for every skill) so the
// implementation can land in a follow-up without further data work.
//
// The TTK gate (ttk.go) catches the worst case for MVP content
// regardless. Normal-mob rotation tightness is more a quality concern
// than a hard gate, so deferring this is low-risk for now.
func evaluateRotation(in Inputs) []Result {
	return nil
}
