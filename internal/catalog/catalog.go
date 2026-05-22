// Package catalog provides indexed read-only access to the iRO data
// catalogue (items, mobs) for the LLM-facing lookup tools. The data is
// baked into the binary at compile time via go:embed; the runtime API
// has no dependency on emulator clones, source paths, or environment
// variables. To refresh: re-run `task catalog` (or `go run ./cmd/build-catalog`)
// after a Hercules update.
//
// Construction is cheap (~50ms parse, ~7,300 items + ~1,000 mobs). The
// catalog is immutable after Load; concurrent reads are safe without
// locks.
package catalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/levonn-dev/ro-builder/internal/data"
)

//go:embed data/catalog.json
var catalogJSON []byte

// expectedVersion is the catalog schema version Load supports. Bump in
// lockstep with cmd/build-catalog/main.go's `Version: ...` constant when
// the schema changes incompatibly. Old catalogs failing the check is the
// failure mode; the operator regenerates with `task catalog`.
//
// v2: added JobMaxLevels (per-class base + job level caps parsed from
// Hercules job_db.conf + exp_group_db.conf), replacing the hand-rolled
// classMaxLevels table.
const expectedVersion = 2

// catalogFile mirrors the schema cmd/build-catalog writes.
type catalogFile struct {
	Version int          `json:"version"`
	Mode    string       `json:"mode"`
	Items   []data.Item  `json:"items"`
	Mobs    []data.Mob   `json:"mobs"`
	Skills  []data.Skill `json:"skills,omitempty"`
	// Classes maps the lowercase shim-style class name (e.g. "taekwon",
	// "knight", "high_wizard") to its allocatable skill tree, with
	// inherits already flattened by cmd/build-catalog. The runtime
	// catalog reads this directly via Class(name).
	Classes map[string]data.ClassSkillTree `json:"classes,omitempty"`
	// JobMaxLevels carries the per-class base/job level caps Hercules
	// derives via job_db.conf + exp_group_db.conf. Hercules's original
	// casing is preserved (e.g. "Assassin_Cross"); Load converts to
	// shim-style keys for runtime lookup.
	JobMaxLevels []data.JobMaxLevels `json:"job_max_levels,omitempty"`
}

// Catalog is the indexed view. Construct with Load; query via the methods.
type Catalog struct {
	version      int
	items        map[int]data.Item
	itemList     []data.Item // ordered for stable search results
	mobs         map[int]data.Mob
	mobList      []data.Mob
	skills       map[int]data.Skill
	skillList    []data.Skill
	skillsByName map[string]data.Skill
	classes      map[string]data.ClassSkillTree
	jobMaxLevels map[string][2]int // shim-key → [maxBase, maxJob]
}

// Load parses the embedded catalog.json and returns a ready-to-query
// Catalog. Errors here mean the embedded asset is malformed or has a
// version this binary doesn't support; both build-time invariant
// violations a production binary should never see.
func Load() (*Catalog, error) {
	var f catalogFile
	if err := json.Unmarshal(catalogJSON, &f); err != nil {
		return nil, fmt.Errorf("decode embedded catalog: %w", err)
	}
	if f.Version != expectedVersion {
		return nil, fmt.Errorf(
			"embedded catalog version %d not supported (this binary expects v%d); regenerate with `task catalog`",
			f.Version, expectedVersion)
	}

	c := &Catalog{
		version: f.Version,
		items:   make(map[int]data.Item, len(f.Items)),
		mobs:    make(map[int]data.Mob, len(f.Mobs)),
		skills:  make(map[int]data.Skill, len(f.Skills)),
	}
	// First-seen wins on id collision (matches build-rocalc-mapping's
	// policy and protects against stray duplicate-id rows in the source).
	for _, it := range f.Items {
		if _, exists := c.items[it.ID]; exists {
			continue
		}
		c.items[it.ID] = it
		c.itemList = append(c.itemList, it)
	}
	for _, m := range f.Mobs {
		if _, exists := c.mobs[m.ID]; exists {
			continue
		}
		c.mobs[m.ID] = m
		c.mobList = append(c.mobList, m)
	}
	for _, s := range f.Skills {
		if _, exists := c.skills[s.ID]; exists {
			continue
		}
		c.skills[s.ID] = s
		c.skillList = append(c.skillList, s)
	}
	c.skillsByName = make(map[string]data.Skill, len(c.skillList))
	for _, s := range c.skillList {
		c.skillsByName[s.AegisName] = s
	}
	c.classes = make(map[string]data.ClassSkillTree, len(f.Classes))
	for k, v := range f.Classes {
		c.classes[k] = v
	}
	c.jobMaxLevels = make(map[string][2]int, len(f.JobMaxLevels))
	for _, j := range f.JobMaxLevels {
		c.jobMaxLevels[normalizeClassKey(j.Name)] = [2]int{j.MaxBaseLevel, j.MaxJobLevel}
	}
	return c, nil
}

// normalizeClassKey canonicalizes a class-name input to the catalog's
// internal map key form: lowercase, spaces folded to underscores. Mirrors
// the form Load uses when indexing JobMaxLevels and the shim-style keys
// the Classes map uses, so request-side inputs like "Taekwon_Kid" or
// "taekwon kid" hit the same row as "taekwon_kid".
func normalizeClassKey(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, " ", "_"))
}

// Item returns the item with the given iRO id, or zero value + false.
func (c *Catalog) Item(iroID int) (data.Item, bool) {
	it, ok := c.items[iroID]
	return it, ok
}

// Mob returns the mob with the given iRO id, or zero value + false.
func (c *Catalog) Mob(iroID int) (data.Mob, bool) {
	m, ok := c.mobs[iroID]
	return m, ok
}

// Skill returns the skill with the given iRO id, or zero value + false.
func (c *Catalog) Skill(iroID int) (data.Skill, bool) {
	s, ok := c.skills[iroID]
	return s, ok
}

// Class returns the allocatable skill tree for a class by its
// shim-style key (lowercase, underscore-separated: "taekwon",
// "knight", "high_wizard"). Inherits are flattened at build time so
// the returned Skills slice is the fully expanded set the class can
// allocate.
func (c *Catalog) Class(name string) (data.ClassSkillTree, bool) {
	t, ok := c.classes[name]
	return t, ok
}

// ClassWithAliases resolves a request-style class name to the catalog's
// flattened tree. Catalog keys come from Hercules's skill_tree.conf
// (lowercase + space→underscore); most API class names match directly,
// but a few aliases (taekwon_kid → taekwon, swordman → swordsman) need
// mapping. Add a row to classAliases when "class X not in catalog"
// shows up in /generate logs.
func (c *Catalog) ClassWithAliases(requestClass string) (data.ClassSkillTree, bool) {
	key := normalizeClassKey(requestClass)
	if t, ok := c.classes[key]; ok {
		return t, true
	}
	if alias, ok := classAliases[key]; ok {
		t, ok := c.classes[alias]
		return t, ok
	}
	return data.ClassSkillTree{}, false
}

// classAliases maps request-style class names to the catalog key Hercules
// records them under. Kept narrow; most names match the catalog key
// directly.
//
// Trans-class naming: Hercules records trans 1st-job classes as
// "<base>_high" (swordsman_high, acolyte_high, ...) but trans 2nd jobs
// as "high_<class>" (high_priest, high_wizard). Aliases here handle
// the common API form "high_<base>" for trans 1st jobs.
var classAliases = map[string]string{
	"taekwon_kid":    "taekwon",
	"swordman":       "swordsman",
	"high_swordman":  "swordsman_high",
	"high_swordsman": "swordsman_high",
}

// transSecondJobToTransFirst maps each trans 2nd-job catalog key to its
// post-rebirth trans 1st-job tier. The catalog's `Inherit` chain reflects
// skill-tree inheritance (which skills are allocatable, usually pulled
// from the pre-rebirth lineage), NOT job progression. After rebirth a
// player passes through novice_high → <trans 1st> → <trans 2nd>, so the
// skill-point budget chain must traverse those classes; not the
// pre-rebirth assassin → thief → novice path the Inherit chain walks.
//
// Trans 1st-job choice follows the base 1st-job: Crusader and Knight both
// share swordsman_high; Champion and High Priest both share acolyte_high;
// etc.
var transSecondJobToTransFirst = map[string]string{
	"lord_knight":    "swordsman_high",
	"paladin":        "swordsman_high",
	"high_priest":    "acolyte_high",
	"champion":       "acolyte_high",
	"high_wizard":    "magician_high",
	"professor":      "magician_high",
	"whitesmith":     "merchant_high",
	"creator":        "merchant_high",
	"assassin_cross": "thief_high",
	"stalker":        "thief_high",
	"sniper":         "archer_high",
	"clown":          "archer_high",
	"gypsy":          "archer_high",
}

// ClassMaxLevels returns the pre-renewal max base and job levels for the
// named class as parsed from Hercules's job_db.conf + exp_group_db.conf at
// catalog build time. requestClass accepts the same alias forms as
// ClassWithAliases. Returns (0, 0, false) when the class is empty, not
// in the catalog, or missing exp-group data; callers (orchestrator,
// ClassSkillPointBudget) skip the prompt-injection / chain contribution
// on false rather than guessing.
//
// Server-specific overrides (e.g. UARO raising Soul Linker max job) live
// on the ServerProfile and are merged on top of this value by the
// orchestrator's fetchClassMaxLevelsForPrompt; they don't belong in the
// catalog, which captures only the upstream emulator data.
func (c *Catalog) ClassMaxLevels(requestClass string) (baseLevel, jobLevel int, ok bool) {
	if requestClass == "" {
		return 0, 0, false
	}
	key := normalizeClassKey(requestClass)
	if alias, aliasOK := classAliases[key]; aliasOK {
		key = alias
	}
	pair, found := c.jobMaxLevels[key]
	if !found {
		return 0, 0, false
	}
	return pair[0], pair[1], true
}

// ClassSkillRequirement is one prerequisite for a class skill; the
// prereq skill's aegis name (e.g. "TK_STORMKICK") and the minimum
// invested level the player must have allocated before this skill can
// be learned.
type ClassSkillRequirement struct {
	AegisName string
	Level     int
}

// ClassSkill is one entry from a class's skill tree joined against the
// per-skill metadata in m_Skill. Catalog-side projection used by both
// the orchestrator's user-prompt block and the list_class_skills tool;
// each caller projects further to its own consumer-specific shape.
type ClassSkill struct {
	ID            int
	AegisName     string
	Name          string
	MaxLevel      int
	AttackType    string
	Element       string
	CastTimeMs    int
	CooldownMs    int
	Interruptible bool
	// Requires lists skills that must be allocated at the given level
	// before this skill can be learned. Sorted by AegisName for stable
	// output. Empty for skills with no prerequisites.
	Requires []ClassSkillRequirement `json:",omitempty"`
}

// ClassSkills returns the named class's allocatable skills, each joined
// against per-skill metadata (display name, cast/cooldown, etc.). Looks
// up the tree via ClassWithAliases so the caller can pass the request-
// style name directly. Returns (nil, false) when the class isn't in
// the catalog.
func (c *Catalog) ClassSkills(requestClass string) ([]ClassSkill, bool) {
	tree, ok := c.ClassWithAliases(requestClass)
	if !ok {
		return nil, false
	}
	byName := c.SkillsByName()
	out := make([]ClassSkill, 0, len(tree.Skills))
	for _, s := range tree.Skills {
		entry := ClassSkill{AegisName: s.AegisName, MaxLevel: s.MaxLevel}
		if sk, ok := byName[s.AegisName]; ok {
			entry.ID = sk.ID
			entry.Name = sk.Name
			entry.AttackType = sk.AttackType
			entry.Element = sk.Element
			entry.CastTimeMs = sk.CastTimeMs
			entry.CooldownMs = sk.CooldownMs
			entry.Interruptible = sk.Interruptible
			if entry.MaxLevel == 0 {
				entry.MaxLevel = sk.MaxLevel
			}
		}
		if len(s.Requires) > 0 {
			reqs := make([]ClassSkillRequirement, 0, len(s.Requires))
			for name, lv := range s.Requires {
				reqs = append(reqs, ClassSkillRequirement{AegisName: name, Level: lv})
			}
			sort.Slice(reqs, func(i, j int) bool { return reqs[i].AegisName < reqs[j].AegisName })
			entry.Requires = reqs
		}
		out = append(out, entry)
	}
	return out, true
}

// SkillsByName returns the cached aegis-name index of every skill.
// Built once at Load time; callers must treat the returned map as
// read-only.
func (c *Catalog) SkillsByName() map[string]data.Skill {
	return c.skillsByName
}

// ItemFilter narrows a SearchItems call. Empty / zero-value fields mean
// "ignore that filter"; non-empty fields are AND-combined.
type ItemFilter struct {
	Type         string // "IT_WEAPON", "IT_ARMOR", "IT_CARD", etc.
	Subtype      string // "W_DAGGER", "A_SHIELD", etc.
	NameContains string // case-insensitive substring of display Name
	MinSlots     int    // 0 = ignore (treat as "no minimum")
	MaxSlots     int    // 0 = ignore
	MaxEquipLv   int    // 0 = ignore
	Limit        int    // max results; 0 → DefaultSearchLimit; capped at MaxSearchLimit
}

const (
	DefaultSearchLimit = 20
	MaxSearchLimit     = 100
)

// SearchItems applies the filter and returns up to Limit matches in
// catalogue order. The order is stable for deterministic LLM behavior;
// scoring identical queries should produce identical candidate lists.
func (c *Catalog) SearchItems(f ItemFilter) []data.Item {
	limit := f.Limit
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}
	nameLower := strings.ToLower(f.NameContains)
	out := make([]data.Item, 0, limit)
	for _, it := range c.itemList {
		if f.Type != "" && it.Type != f.Type {
			continue
		}
		if f.Subtype != "" && it.Subtype != f.Subtype {
			continue
		}
		if nameLower != "" && !strings.Contains(strings.ToLower(it.Name), nameLower) {
			continue
		}
		if f.MinSlots > 0 && it.Slots < f.MinSlots {
			continue
		}
		if f.MaxSlots > 0 && it.Slots > f.MaxSlots {
			continue
		}
		if f.MaxEquipLv > 0 && it.EquipLv > f.MaxEquipLv {
			continue
		}
		out = append(out, it)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// ClassSkillPointBudget returns the cumulative skill-point budget at the
// endgame of requestClass: the sum of (max_job_level − 1) across every
// class in the job-progression chain (NOT the catalog's Inherit chain;
// see progressionChain for the distinction).
//
// Returns (0, false) if requestClass is not in the catalog.
// Returns (budget, true) for any recognized class.
//
// Examples (pre-renewal):
//
//   - taekwon_kid → novice (maxJob 10) + taekwon (maxJob 50) = 9 + 49 = 58
//   - assassin → novice + thief + assassin = 9 + 49 + 49 = 107
//   - assassin_cross (trans) → novice_high + thief_high + assassin_cross
//     = 9 + 49 + 69 = 127
//
// The trans path explicitly does NOT traverse the pre-rebirth assassin/thief
// classes; a trans 2nd-job's points come from the post-rebirth progression
// (high_novice → trans 1st → trans 2nd) per transSecondJobToTransFirst.
func (c *Catalog) ClassSkillPointBudget(requestClass string) (int, bool) {
	// Resolve alias so we use the catalog key consistently.
	key := normalizeClassKey(requestClass)
	if alias, ok := classAliases[key]; ok {
		key = alias
	}
	if _, exists := c.classes[key]; !exists {
		return 0, false
	}
	total := 0
	for _, cls := range c.progressionChain(key) {
		_, maxJob, ok := c.ClassMaxLevels(cls)
		if !ok {
			continue
		}
		if maxJob > 1 {
			total += maxJob - 1
		}
	}
	return total, true
}

// progressionChain returns the job-progression sequence for a catalog
// class key; the classes the player levels through to reach key's
// endgame. Distinct from the catalog's Inherit chain, which captures
// skill-tree inheritance (what skills are allocatable). For a trans
// 2nd-job, Inherit walks the pre-rebirth lineage (assassin_cross →
// assassin → thief → novice) but the actual points the player earned
// after rebirth come from a different chain (high_novice → thief_high
// → assassin_cross).
//
// Returned slice is ordered low → high. Returns nil for classes not in
// the catalog. Currently covers the pre-renewal job graph; renewal-only
// classes (genetic, royal_guard, baby_*, etc.) fall through to the Inherit
// walk and produce best-effort numbers that aren't gate-verified.
func (c *Catalog) progressionChain(key string) []string {
	// Trans 2nd-job: post-rebirth path is the source of truth, not
	// catalog Inherit which walks the pre-rebirth lineage.
	if transFirst, ok := transSecondJobToTransFirst[key]; ok {
		return []string{"novice_high", transFirst, key}
	}
	// Trans 1st-job; every key suffixed with "_high" except novice_high
	// (which IS the trans novice tier); and itself.
	if key != "novice_high" && strings.HasSuffix(key, "_high") {
		return []string{"novice_high", key}
	}
	if key == "novice_high" {
		return []string{"novice_high"}
	}
	// Non-trans path: walk Inherit recursively back to the root, then
	// emit each visited class once in low → high order. For non-trans
	// classes, the catalog's Inherit chain happens to match job
	// progression (assassin → thief → novice).
	seen := make(map[string]bool)
	out := []string{}
	var visit func(string)
	visit = func(k string) {
		if seen[k] {
			return
		}
		seen[k] = true
		tree, ok := c.classes[k]
		if !ok {
			// Not in the catalog; emit the key anyway so ClassMaxLevels
			// can fall back to its default; an unknown class won't drive
			// the budget but also doesn't silently zero out the chain.
			out = append(out, k)
			return
		}
		for _, inh := range tree.Inherit {
			visit(strings.ToLower(strings.ReplaceAll(inh, " ", "_")))
		}
		out = append(out, k)
	}
	visit(key)
	return out
}

// Version returns the schema version of the loaded catalog (matches
// cmd/build-catalog's Version field at generation time). Stamped into
// saved build-library records so stale entries are detectable after
// the catalog snapshot changes.
func (c *Catalog) Version() int { return c.version }

// ItemCount returns the number of indexed items.
func (c *Catalog) ItemCount() int { return len(c.items) }

// MobCount returns the number of indexed mobs.
func (c *Catalog) MobCount() int { return len(c.mobs) }

// SkillCount returns the number of indexed skills.
func (c *Catalog) SkillCount() int { return len(c.skills) }
