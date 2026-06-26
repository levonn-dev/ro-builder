// build-catalog is a run-only refresh tool, not a deployable binary. It
// reads emulator data and writes a single catalog.json that the runtime
// API embeds into its binary at compile time. Source is selectable:
// Hercules (default) or rAthena. Same run-only-refresh pattern as the other
// one-shot data tools.
//
// Invoke via `go run ./cmd/build-catalog` (or `task catalog`); this
// tool intentionally has no `task build:catalog` target. Running it
// produces only the JSON; any binary emitted by `go build` is a
// side-effect of using `go build` and is gitignored.
//
// Usage:
//
//	go run ./cmd/build-catalog                                # Hercules, ../Hercules
//	go run ./cmd/build-catalog -source rathena                # rAthena, ../rathena
//	go run ./cmd/build-catalog -source rathena -root /path/to/rathena
//	go run ./cmd/build-catalog -out path/to/catalog.json
//
// Or via Taskfile:
//
//	task catalog                                              # Hercules
//	task catalog -- -source rathena                           # rAthena
//
// The output file is committed; running this tool is a deliberate
// refresh step (typically after upstream emulator updates), not a
// build-every-time step. The runtime never executes this code;
// `internal/catalog` reads the embedded JSON directly.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/levonn-dev/ro-builder/internal/clitools"
	"github.com/levonn-dev/ro-builder/internal/data"
)

const (
	defaultHerculesRoot = "../Hercules"
	defaultRathenaRoot  = "../rathena"
	defaultOutPath      = "internal/catalog/data/catalog.json"
	immunityOverlayPath = "internal/catalog/data/item_immunities.yaml"
	mobThreatsPath      = "internal/catalog/data/mob_threats.yaml"
	skillBuffsPath      = "internal/catalog/data/skill_buffs.yaml"
	innateBuffsPath     = "internal/catalog/data/class_innate_buffs.yaml"
	attackSkillsPath    = "internal/catalog/data/attack_skills.yaml"
)

type catalogFile struct {
	Version     int                            `json:"version"`
	Mode        string                         `json:"mode"`
	Source      string                         `json:"source"`
	GeneratedAt time.Time                      `json:"generated_at"`
	Items       []data.Item                    `json:"items"`
	Mobs        []data.Mob                     `json:"mobs"`
	Skills      []data.Skill                   `json:"skills,omitempty"`
	Classes     map[string]data.ClassSkillTree `json:"classes,omitempty"`
	// JobMaxLevels carries per-class base and job level caps derived
	// from Hercules's job_db.conf + exp_group_db.conf. Keyed by
	// Hercules's original job name (e.g. "Assassin_Cross"); runtime
	// converts to shim-style keys at lookup time.
	JobMaxLevels []data.JobMaxLevels `json:"job_max_levels,omitempty"`
	// ClassInnateBuffs carries class-innate self-buffs (mechanics like the Super
	// Novice No-Death Bonus) that are not anchored to a learnable skill. Keyed by
	// the shim-style class name. See internal/catalog/data/class_innate_buffs.yaml.
	ClassInnateBuffs map[string][]data.SelfBuff `json:"class_innate_buffs,omitempty"`
}

func main() {
	sourceFlag := flag.String("source", "hercules", "data source: hercules | rathena")
	rootFlag := flag.String("root", "", "path to the emulator clone (default ../Hercules or ../rathena depending on -source)")
	outFlag := flag.String("out", defaultOutPath, "output catalog.json path (relative to repo root)")
	flag.Parse()

	source, err := data.ParseSource(*sourceFlag)
	if err != nil {
		slog.Default().Error("parse source flag", slog.String("error", err.Error()))
		os.Exit(1)
	}

	repoRoot, err := clitools.FindRepoRoot()
	if err != nil {
		slog.Default().Error("find repo root", slog.String("error", err.Error()))
		os.Exit(1)
	}

	root := *rootFlag
	if root == "" {
		switch source {
		case data.SourceHercules:
			root = defaultHerculesRoot
		case data.SourceRathena:
			root = defaultRathenaRoot
		}
	}
	rootAbs, err := filepath.Abs(resolvePath(repoRoot, root))
	if err != nil {
		slog.Default().Error("resolve root path", slog.String("error", err.Error()))
		os.Exit(1)
	}
	outAbs := resolvePath(repoRoot, *outFlag)

	slog.Default().Info("building catalog", slog.String("source", source.String()), slog.String("root", rootAbs))

	items, err := data.LoadItemsFrom(rootAbs, data.PreRenewal, source)
	if err != nil {
		slog.Default().Error("load items", slog.String("error", err.Error()))
		os.Exit(1)
	}
	mobs, err := data.LoadMobsFrom(rootAbs, data.PreRenewal, source)
	if err != nil {
		slog.Default().Error("load mobs", slog.String("error", err.Error()))
		os.Exit(1)
	}
	skills, err := data.LoadSkillsFrom(rootAbs, data.PreRenewal, source)
	if err != nil {
		slog.Default().Error("load skills", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Layer the hand-authored immunity overlay on top of the emulator-derived
	// item set. Lives in the repo at internal/catalog/data/item_immunities.yaml;
	// see that file's comment block for the rationale and editing rules.
	overlayAbs := resolvePath(repoRoot, immunityOverlayPath)
	overlay, err := loadImmunityOverlay(overlayAbs)
	if err != nil {
		slog.Default().Error("load immunity overlay", slog.String("path", overlayAbs), slog.String("error", err.Error()))
		os.Exit(1)
	}
	applied, err := applyImmunityOverlay(items, overlay)
	if err != nil {
		slog.Default().Error("apply immunity overlay", slog.String("error", err.Error()))
		os.Exit(1)
	}
	slog.Default().Info("merged immunity overlay", slog.Int("applied", applied))

	// Layer the hand-authored self-buff overlay onto skill records. Lives at
	// internal/catalog/data/skill_buffs.yaml; see that file's comment block for
	// the editing rules.
	skillBuffsAbs := resolvePath(repoRoot, skillBuffsPath)
	skillBuffs, err := loadSkillBuffs(skillBuffsAbs)
	if err != nil {
		slog.Default().Error("load skill buffs overlay", slog.String("path", skillBuffsAbs), slog.String("error", err.Error()))
		os.Exit(1)
	}
	appliedBuffs, err := applySkillBuffs(skills, skillBuffs)
	if err != nil {
		slog.Default().Error("apply skill buffs overlay", slog.String("error", err.Error()))
		os.Exit(1)
	}
	slog.Default().Info("merged skill buffs overlay", slog.Int("applied", appliedBuffs))

	attackSkillsAbs := resolvePath(repoRoot, attackSkillsPath)
	attackSkills, err := loadAttackSkills(attackSkillsAbs)
	if err != nil {
		slog.Default().Error("load attack skills overlay", slog.String("path", attackSkillsAbs), slog.String("error", err.Error()))
		os.Exit(1)
	}
	appliedAtk, err := applyAttackSkills(skills, attackSkills)
	if err != nil {
		slog.Default().Error("apply attack skills overlay", slog.String("error", err.Error()))
		os.Exit(1)
	}
	slog.Default().Info("merged attack skills overlay", slog.Int("applied", appliedAtk))

	innateBuffsAbs := resolvePath(repoRoot, innateBuffsPath)
	innateBuffs, err := loadInnateBuffs(innateBuffsAbs)
	if err != nil {
		slog.Default().Error("load class-innate buffs", slog.String("error", err.Error()))
		os.Exit(1)
	}
	slog.Default().Info("class-innate buffs loaded", slog.Int("classes", len(innateBuffs)))

	// Compute mob threats programmatically by joining rAthena's
	// mob_skill_db.txt against each skill's StatusChange field in our
	// catalog (already populated from Hercules's skill_db.conf above).
	// Result: hundreds of mob threat profiles derived from upstream data,
	// no hand-authoring required for the common case.
	//
	// We always read rAthena's text format because Hercules ships
	// mob_skill_db only as SQL; iRO ids agree across emulators so this
	// works regardless of which source produced the rest of the catalog.
	rathenaForMobs := resolvePath(repoRoot, defaultRathenaRoot)
	computedThreats, computedCount, err := computeMobThreats(rathenaForMobs, skills)
	if err != nil {
		slog.Default().Error("compute mob threats", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if computedCount > 0 {
		slog.Default().Info("computed mob threats", slog.String("source", rathenaForMobs+"/db/pre-re/mob_skill_db.txt"), slog.Int("mobs_annotated", computedCount))
	} else {
		slog.Default().Info("rAthena mob_skill_db not found; skipping computed threats (hand-authored overlay only)", slog.String("path", rathenaForMobs))
	}

	// Hand-authored overlay layers on top of the computed set. Replace
	// semantics: any mob present in the YAML overrides the computed
	// entry entirely. Used for (a) auto-attack status procs that aren't
	// in mob_skill_db (Eddga / Atroce stun proc, lore-known) and (b)
	// corrections where the computed data is wrong.
	threatsAbs := resolvePath(repoRoot, mobThreatsPath)
	threatOverlay, err := loadMobThreats(threatsAbs)
	if err != nil {
		slog.Default().Error("load mob threats", slog.String("path", threatsAbs), slog.String("error", err.Error()))
		os.Exit(1)
	}
	for id, t := range threatOverlay {
		computedThreats[id] = t
	}
	threatsApplied, err := applyMobThreats(mobs, computedThreats)
	if err != nil {
		slog.Default().Error("apply mob threats", slog.String("error", err.Error()))
		os.Exit(1)
	}
	slog.Default().Info("merged mob threats (computed + overlay)", slog.Int("applied", threatsApplied))

	// Class skill trees from Hercules's skill_tree.conf; flattened so
	// each class's Skills slice carries its own + inherited skills (Novice's
	// skills end up under every job that inherits from it). Always read
	// from a Hercules clone since rAthena's format isn't supported here
	// (Hercules is the canonical source for skill_tree.conf, even when
	// -source=rathena is selected for items/mobs/skills).
	//
	// When the caller explicitly passed `-root` AND the source is Hercules,
	// honor that path; they likely have a fork they want to use end-to-end
	// for items/mobs/skills/skill_tree consistently. For -source=rathena,
	// rootAbs points at a rAthena clone (no skill_tree.conf there), so we
	// fall back to the default Hercules path.
	skillTreeRoot := defaultHerculesRoot
	if source == data.SourceHercules {
		skillTreeRoot = rootAbs
	}
	classes, err := buildClassMap(filepath.Join(skillTreeRoot, "db", "pre-re", "skill_tree.conf"), repoRoot)
	if err != nil {
		slog.Default().Info("class skill trees not loaded (skipping)", slog.String("error", err.Error()))
	} else {
		slog.Default().Info("class skill trees loaded", slog.Int("classes", len(classes)))
	}

	// Job max-levels also come from a Hercules-only pair (job_db.conf +
	// exp_group_db.conf); rAthena's job-db format isn't supported here.
	// Use the same skillTreeRoot fallback as the skill-tree load so the
	// data stays consistent when -source=rathena.
	jobMaxLevels, err := loadJobMaxLevels(skillTreeRoot, repoRoot)
	if err != nil {
		slog.Default().Info("job max-levels not loaded (skipping)", slog.String("error", err.Error()))
	} else {
		slog.Default().Info("job max-levels loaded", slog.Int("classes", len(jobMaxLevels)))
	}

	out := catalogFile{
		Version:          2,
		Mode:             "pre-renewal",
		Source:           sourceLabel(source),
		GeneratedAt:      time.Now().UTC(),
		Items:            items,
		Mobs:             mobs,
		Skills:           skills,
		Classes:          classes,
		JobMaxLevels:     jobMaxLevels,
		ClassInnateBuffs: innateBuffs,
	}

	if err := os.MkdirAll(filepath.Dir(outAbs), 0o755); err != nil {
		slog.Default().Error("mkdir output dir", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Compact JSON keeps the embedded binary asset small (under 1 MB vs
	// ~1.6 MB indented). Hand-diffing isn't useful for a 7000-row generated
	// file; the build tool's stdout is the authoritative summary.
	body, err := json.Marshal(out)
	if err != nil {
		slog.Default().Error("marshal catalog", slog.String("error", err.Error()))
		os.Exit(1)
	}
	// catalog.json is a build-time artifact regenerated on every run; the
	// previous file is intentionally clobbered. Re-run this generator (e.g.
	// `task catalog`) after upstream emulator data changes. No atomic
	// temp+rename: a half-written file would only surface as a JSON parse
	// error in the next `go build`, and the regenerator can just be re-run.
	if err := os.WriteFile(outAbs, body, 0o644); err != nil {
		slog.Default().Error("write catalog", slog.String("error", err.Error()))
		os.Exit(1)
	}
	slog.Default().Info("wrote catalog",
		slog.String("path", outAbs),
		slog.Int("items", len(items)),
		slog.Int("mobs", len(mobs)),
		slog.Int("skills", len(skills)),
		slog.Int("bytes", len(body)),
	)
}

// sourceLabel renders a human-readable description of where the data
// came from, stored in catalog.json's metadata. Useful when debugging
// "why does my Knife have these stats"; the source labels which
// emulator's snapshot is in play.
func sourceLabel(s data.Source) string {
	switch s {
	case data.SourceHercules:
		return "Hercules db/pre-re/item_db.conf, mob_db.conf, skill_db.conf"
	case data.SourceRathena:
		return "rAthena db/pre-re/item_db_*.yml, mob_db.yml, skill_db.yml"
	}
	return s.String()
}

// resolvePath treats absolute paths as literal and relative paths as
// rooted at the repo root. Without this, `-out /tmp/foo.json` would write
// to <repo>/tmp/foo.json because filepath.Join silently re-roots.
func resolvePath(repoRoot, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(repoRoot, p)
}

// immunityOverlayFile mirrors internal/catalog/data/item_immunities.yaml:
// a top-level `items:` list of {id, grants_immunity[], grants_uninterruptible_cast}.
type immunityOverlayFile struct {
	Items []immunityEntry `yaml:"items"`
}

type immunityEntry struct {
	ID                        int      `yaml:"id"`
	GrantsImmunity            []string `yaml:"grants_immunity"`
	GrantsUninterruptibleCast bool     `yaml:"grants_uninterruptible_cast"`
}

// loadImmunityOverlay reads and validates the overlay file. Validation is
// strict so a typo in a status key (e.g. "frozeon") fails the build instead
// of silently dropping the tag.
func loadImmunityOverlay(path string) (map[int]immunityEntry, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var f immunityOverlayFile
	if err := yaml.Unmarshal(src, &f); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	out := make(map[int]immunityEntry, len(f.Items))
	for i, e := range f.Items {
		if e.ID == 0 {
			return nil, fmt.Errorf("entry %d: id is required", i)
		}
		if _, dup := out[e.ID]; dup {
			return nil, fmt.Errorf("entry %d: duplicate id %d", i, e.ID)
		}
		for _, s := range e.GrantsImmunity {
			if !data.IsValidImmunity(s) {
				return nil, fmt.Errorf("entry %d (id %d): unknown immunity %q (valid: %v)",
					i, e.ID, s, data.AllImmunities)
			}
		}
		out[e.ID] = e
	}
	return out, nil
}

// applyImmunityOverlay mutates items in place, setting GrantsImmunity /
// GrantsUninterruptibleCast on each item present in the overlay. Returns
// the number of entries applied; errors if any overlay id is missing
// from the catalog (= the overlay references an item the emulator
// snapshot doesn't have, which is almost always a typo).
func applyImmunityOverlay(items []data.Item, overlay map[int]immunityEntry) (int, error) {
	byID := make(map[int]int, len(items))
	for i := range items {
		byID[items[i].ID] = i
	}
	applied := 0
	for id, e := range overlay {
		idx, ok := byID[id]
		if !ok {
			return applied, fmt.Errorf("overlay id %d not found in catalog (typo or item not in this mode?)", id)
		}
		items[idx].GrantsImmunity = e.GrantsImmunity
		items[idx].GrantsUninterruptibleCast = e.GrantsUninterruptibleCast
		applied++
	}
	return applied, nil
}

// skillBuffsFile mirrors internal/catalog/data/skill_buffs.yaml: a top-level
// `skills:` list of {aegis_name, self_buff}. Matched onto skill records by
// aegis_name (skills have no stable position the way ids index items).
type skillBuffsFile struct {
	Skills []skillBuffEntry `yaml:"skills"`
}

type skillBuffEntry struct {
	AegisName string         `yaml:"aegis_name"`
	SelfBuff  *data.SelfBuff `yaml:"self_buff"`
}

// validateSkillBuffEntry enforces the overlay's invariants. Strict so a typo
// fails the build instead of silently dropping the tag.
func validateSkillBuffEntry(i int, e skillBuffEntry) error {
	if e.AegisName == "" {
		return fmt.Errorf("entry %d: aegis_name is required", i)
	}
	if e.SelfBuff == nil {
		return fmt.Errorf("entry %d (%s): self_buff is required", i, e.AegisName)
	}
	b := e.SelfBuff
	if b.Name == "" {
		return fmt.Errorf("entry %d (%s): self_buff.name is required", i, e.AegisName)
	}
	if !data.IsValidBuffKind(b.Kind) {
		return fmt.Errorf("entry %d (%s): unknown kind %q (valid: %v)", i, e.AegisName, b.Kind, data.AllBuffKinds)
	}
	if !data.IsValidPersistence(b.Persistence) {
		return fmt.Errorf("entry %d (%s): unknown persistence %q (valid: %v)", i, e.AegisName, b.Persistence, data.AllPersistence)
	}
	if b.Kind == data.BuffKindWeaponEndow {
		if b.Endow == nil || len(b.Endow.Elements) == 0 {
			return fmt.Errorf("entry %d (%s): weapon_endow requires endow.elements", i, e.AegisName)
		}
		seenEl := make(map[string]struct{}, len(b.Endow.Elements))
		for _, el := range b.Endow.Elements {
			if !data.IsValidElement(el) {
				return fmt.Errorf("entry %d (%s): unknown endow element %q (valid: %v)", i, e.AegisName, el, data.AllElements)
			}
			// A repeat would make the element's 1-based position (which encodes
			// the level unlock) ambiguous; resolver and sidecar both match on
			// the first occurrence, so a dup silently mis-gates the later one.
			if _, dup := seenEl[el]; dup {
				return fmt.Errorf("entry %d (%s): duplicate endow element %q", i, e.AegisName, el)
			}
			seenEl[el] = struct{}{}
		}
	} else if b.Endow != nil {
		return fmt.Errorf("entry %d (%s): endow set on non-weapon_endow kind %q", i, e.AegisName, b.Kind)
	}
	return nil
}

// loadSkillBuffs reads and validates the overlay, keyed by aegis_name.
func loadSkillBuffs(path string) (map[string]*data.SelfBuff, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var f skillBuffsFile
	if err := yaml.Unmarshal(src, &f); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	out := make(map[string]*data.SelfBuff, len(f.Skills))
	// Buff names must be unique across entries: the resolver and list_class_buffs
	// key on self_buff.name, so two skills sharing a name would silently collide
	// (last-write-wins) and resolve the wrong anchor skill / element table.
	seenName := make(map[string]string, len(f.Skills)) // buff name -> first aegis
	for i, e := range f.Skills {
		if err := validateSkillBuffEntry(i, e); err != nil {
			return nil, err
		}
		if _, dup := out[e.AegisName]; dup {
			return nil, fmt.Errorf("entry %d: duplicate aegis_name %q", i, e.AegisName)
		}
		if prev, dup := seenName[e.SelfBuff.Name]; dup {
			return nil, fmt.Errorf("entry %d (%s): duplicate self_buff.name %q (already used by %s)", i, e.AegisName, e.SelfBuff.Name, prev)
		}
		out[e.AegisName] = e.SelfBuff
		seenName[e.SelfBuff.Name] = e.AegisName
	}
	return out, nil
}

type attackSkillEntry struct {
	AegisName   string            `yaml:"aegis_name"`
	AttackSkill *data.AttackSkill `yaml:"attack_skill"`
}

type attackSkillsFile struct {
	Skills []attackSkillEntry `yaml:"skills"`
}

// loadAttackSkills reads and validates the overlay, keyed by aegis_name. Names
// must be unique (the resolver + sidecar binding key on name).
func loadAttackSkills(path string) (map[string]*data.AttackSkill, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var f attackSkillsFile
	if err := yaml.Unmarshal(src, &f); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	out := make(map[string]*data.AttackSkill, len(f.Skills))
	seenName := make(map[string]string, len(f.Skills))
	for i, e := range f.Skills {
		if e.AegisName == "" {
			return nil, fmt.Errorf("entry %d: aegis_name is required", i)
		}
		if e.AttackSkill == nil || e.AttackSkill.Name == "" {
			return nil, fmt.Errorf("entry %d (%s): attack_skill.name is required", i, e.AegisName)
		}
		if _, dup := out[e.AegisName]; dup {
			return nil, fmt.Errorf("entry %d: duplicate aegis_name %q", i, e.AegisName)
		}
		if prev, dup := seenName[e.AttackSkill.Name]; dup {
			return nil, fmt.Errorf("entry %d (%s): duplicate attack_skill.name %q (already used by %s)", i, e.AegisName, e.AttackSkill.Name, prev)
		}
		out[e.AegisName] = e.AttackSkill
		seenName[e.AttackSkill.Name] = e.AegisName
	}
	return out, nil
}

// applyAttackSkills mutates skills in place, setting AttackSkill on each record
// present in the overlay. Errors if any overlay aegis_name is missing.
func applyAttackSkills(skills []data.Skill, overlay map[string]*data.AttackSkill) (int, error) {
	byAegis := make(map[string]int, len(skills))
	for i := range skills {
		byAegis[skills[i].AegisName] = i
	}
	applied := 0
	for aegis, a := range overlay {
		idx, ok := byAegis[aegis]
		if !ok {
			return applied, fmt.Errorf("attack skill aegis %q not found in catalog (typo or skill not in this mode?)", aegis)
		}
		skills[idx].AttackSkill = a
		applied++
	}
	return applied, nil
}

// innateBuffsFile mirrors internal/catalog/data/class_innate_buffs.yaml: a
// top-level `classes:` map of class name -> list of self-buffs that are class
// mechanics, not skills. No aegis_name (no anchor skill).
type innateBuffsFile struct {
	Classes map[string][]data.SelfBuff `yaml:"classes"`
}

// loadInnateBuffs reads and validates the class-innate overlay.
func loadInnateBuffs(path string) (map[string][]data.SelfBuff, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var f innateBuffsFile
	if err := yaml.Unmarshal(src, &f); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	out := make(map[string][]data.SelfBuff, len(f.Classes))
	for class, buffs := range f.Classes {
		seen := make(map[string]bool, len(buffs))
		for i, b := range buffs {
			if b.Name == "" {
				return nil, fmt.Errorf("class %q innate buff %d: name is required", class, i)
			}
			if b.Kind != data.BuffKindStatBuff && b.Kind != data.BuffKindStatus {
				return nil, fmt.Errorf("class %q innate buff %q: kind must be stat_buff or status, got %q", class, b.Name, b.Kind)
			}
			if b.Persistence != data.PersistencePermanent && b.Persistence != data.PersistenceTransient {
				return nil, fmt.Errorf("class %q innate buff %q: persistence must be permanent or transient, got %q", class, b.Name, b.Persistence)
			}
			if b.Endow != nil {
				return nil, fmt.Errorf("class %q innate buff %q: innate buffs cannot be weapon endows", class, b.Name)
			}
			if seen[b.Name] {
				return nil, fmt.Errorf("class %q: duplicate innate buff name %q", class, b.Name)
			}
			seen[b.Name] = true
		}
		out[class] = buffs
	}
	return out, nil
}

// applySkillBuffs mutates skills in place, setting SelfBuff on each record
// present in the overlay. Errors if any overlay aegis_name is missing from
// the catalog (typo, or skill not in this mode).
func applySkillBuffs(skills []data.Skill, overlay map[string]*data.SelfBuff) (int, error) {
	byAegis := make(map[string]int, len(skills))
	for i := range skills {
		byAegis[skills[i].AegisName] = i
	}
	applied := 0
	for aegis, b := range overlay {
		idx, ok := byAegis[aegis]
		if !ok {
			return applied, fmt.Errorf("skill buff aegis %q not found in catalog (typo or skill not in this mode?)", aegis)
		}
		skills[idx].SelfBuff = b
		applied++
	}
	return applied, nil
}

// statusChangeToImmunity maps Hercules's SC_X identifiers to our
// data.Immunity* keys. Only includes statuses currently modeled; SC values
// outside this set (SC_BLEEDING, SC_DPOISON, status buffs, etc.) are
// dropped. This is intentionally narrow; the gates evaluator's
// status_immunity gate only fires on the statuses listed in
// data.AllImmunities, so anything outside is gate-irrelevant.
var statusChangeToImmunity = map[string]string{
	"SC_FREEZE":    data.ImmunityFrozen,
	"SC_STUN":      data.ImmunityStun,
	"SC_SLEEP":     data.ImmunitySleep,
	"SC_STONE":     data.ImmunityStoneCurse, // Hercules uses SC_STONE for stone curse status
	"SC_CURSE":     data.ImmunityCurse,
	"SC_SILENCE":   data.ImmunitySilence,
	"SC_BLIND":     data.ImmunityBlind,
	"SC_POISON":    data.ImmunityPoison,
	"SC_CONFUSION": data.ImmunityConfusion,
	"SC_ILLUSION":  data.ImmunityConfusion, // NPC_HALLUCINATION uses SC_ILLUSION; we treat as Confusion for gate purposes
}

// extraStatusBySkillAegis covers status-applying skills whose Hercules
// skill_db StatusChange field is empty or doesn't directly map (e.g.
// element-armored attacks that confer status only via the element). Use
// sparingly; the StatusChange field is the authoritative source.
var extraStatusBySkillAegis = map[string][]string{
	// WZ_STORMGUST has StatusChange: "SC_FREEZE"; already covered
	// by the SC mapping above; listed here for completeness only.
}

// computeMobThreats parses rAthena's pre-re mob_skill_db.txt and
// aggregates per-mob threat profiles by joining each row's skill aegis
// against the catalog's StatusChange field. Returns map[mobID]*MobThreats
// plus the number of mobs that ended up with non-trivial threat data.
//
// File format per row (CSV):
//
//	MobID,MobName@SkillAegis,State,SkillID,SkillLv,Rate,...
//
// Lines starting with "//" or empty are skipped. Returns empty map (not
// error) when the file is absent so dev environments without a rAthena
// clone still build a catalog (with hand-authored overlay only).
func computeMobThreats(rathenaRoot string, skills []data.Skill) (map[int]*data.MobThreats, int, error) {
	path := filepath.Join(rathenaRoot, "db", "pre-re", "mob_skill_db.txt")
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[int]*data.MobThreats{}, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	// Index skills by aegis name for the StatusChange lookup.
	byAegis := make(map[string]data.Skill, len(skills))
	for _, sk := range skills {
		byAegis[sk.AegisName] = sk
	}

	threats := make(map[int]*data.MobThreats)
	seenSkill := make(map[int]map[string]bool)
	seenStatus := make(map[int]map[string]bool)

	sc := bufio.NewScanner(f)
	// Default scanner buffer is 64KB; mob_skill_db.txt rows fit, but raise
	// it preemptively so future renewal-style longer rows don't choke.
	sc.Buffer(make([]byte, 0, 1<<16), 1<<20)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		cols := strings.Split(line, ",")
		if len(cols) < 2 {
			continue
		}
		mobID, err := strconv.Atoi(strings.TrimSpace(cols[0]))
		if err != nil {
			continue
		}
		// cols[1] is "MobName@SKILL_AEGIS" (after the @).
		skillCol := strings.TrimSpace(cols[1])
		idx := strings.LastIndex(skillCol, "@")
		if idx < 0 {
			continue
		}
		skillAegis := skillCol[idx+1:]

		if _, ok := threats[mobID]; !ok {
			threats[mobID] = &data.MobThreats{}
			seenSkill[mobID] = make(map[string]bool)
			seenStatus[mobID] = make(map[string]bool)
		}
		if !seenSkill[mobID][skillAegis] {
			threats[mobID].CastsSkills = append(threats[mobID].CastsSkills, skillAegis)
			seenSkill[mobID][skillAegis] = true
		}

		// Look up the skill's StatusChange field and map to our
		// immunity keys. Skills without a StatusChange or with an
		// SC value we don't model are silently skipped.
		statusKey := ""
		if sk, ok := byAegis[skillAegis]; ok && sk.StatusChange != "" {
			statusKey = statusChangeToImmunity[sk.StatusChange]
		}
		if statusKey == "" {
			// Fall back to the explicit aegis-keyed map for skills
			// not covered by skill_db's StatusChange.
			if extras, ok := extraStatusBySkillAegis[skillAegis]; ok {
				for _, k := range extras {
					if !seenStatus[mobID][k] {
						threats[mobID].AppliesStatus = append(threats[mobID].AppliesStatus, k)
						seenStatus[mobID][k] = true
					}
				}
			}
			continue
		}
		if !seenStatus[mobID][statusKey] {
			threats[mobID].AppliesStatus = append(threats[mobID].AppliesStatus, statusKey)
			seenStatus[mobID][statusKey] = true
		}
	}
	if err := sc.Err(); err != nil {
		return nil, 0, fmt.Errorf("scan %s line %d: %w", path, lineNo, err)
	}

	// Sort each mob's slices for stable JSON output (the map iteration
	// above produces non-deterministic order). Catalog regen should be
	// reproducible; flaky JSON ordering breaks `git diff`.
	count := 0
	for _, t := range threats {
		if len(t.AppliesStatus) > 0 || len(t.CastsSkills) > 0 {
			count++
		}
		sort.Strings(t.AppliesStatus)
		sort.Strings(t.CastsSkills)
	}
	return threats, count, nil
}

// mobThreatsFile mirrors internal/catalog/data/mob_threats.yaml: a
// top-level `mobs:` list of {id, threats: {applies_status, casts_skills}}.
type mobThreatsFile struct {
	Mobs []mobThreatEntry `yaml:"mobs"`
}

type mobThreatEntry struct {
	ID      int              `yaml:"id"`
	Threats *data.MobThreats `yaml:"threats"`
}

// loadMobThreats reads and validates the mob threat overlay.
func loadMobThreats(path string) (map[int]*data.MobThreats, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var f mobThreatsFile
	if err := yaml.Unmarshal(src, &f); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	out := make(map[int]*data.MobThreats, len(f.Mobs))
	for i, e := range f.Mobs {
		if e.ID == 0 {
			return nil, fmt.Errorf("entry %d: id is required", i)
		}
		if _, dup := out[e.ID]; dup {
			return nil, fmt.Errorf("entry %d: duplicate id %d", i, e.ID)
		}
		if e.Threats != nil {
			for _, s := range e.Threats.AppliesStatus {
				if !data.IsValidImmunity(s) {
					return nil, fmt.Errorf("entry %d (id %d): unknown status %q (valid: %v)",
						i, e.ID, s, data.AllImmunities)
				}
			}
		}
		out[e.ID] = e.Threats
	}
	return out, nil
}

// applyMobThreats mutates mobs in place, setting Threats on each entry
// present in the overlay. Errors if any overlay id is missing from the
// catalog.
func applyMobThreats(mobs []data.Mob, overlay map[int]*data.MobThreats) (int, error) {
	byID := make(map[int]int, len(mobs))
	for i := range mobs {
		byID[mobs[i].ID] = i
	}
	applied := 0
	for id, t := range overlay {
		idx, ok := byID[id]
		if !ok {
			return applied, fmt.Errorf("threat overlay id %d not found in catalog", id)
		}
		mobs[idx].Threats = t
		applied++
	}
	return applied, nil
}

// loadJobMaxLevels reads Hercules's job_db.conf + exp_group_db.conf for
// pre-renewal and returns one JobMaxLevels entry per class. herculesPath
// is relative to repoRoot when not absolute (mirrors the rest of
// cmd/build-catalog's path handling).
func loadJobMaxLevels(herculesPath, repoRoot string) ([]data.JobMaxLevels, error) {
	full := herculesPath
	if !filepath.IsAbs(full) {
		full = filepath.Join(repoRoot, full)
	}
	return data.LoadJobMaxLevels(full, data.PreRenewal)
}

// usabilityExclusions removes skills that Hercules's skill_tree.conf carries as
// allocated (inherited) for a class but which the class cannot actually USE. A
// Soul Linker / Star Gladiator was a TaeKwon Kid, so the flattened tree carries
// every TK skill, but only a usable subset remains. Taekwon Ranker (TK_MISSION)
// is TaeKwon-Kid-only (the calc engine applies it only for TaeKwon Kid); without this it
// would wrongly surface as a self-buff for both 2nd classes. Keyed by shim-style
// class name -> set of aegis names to drop.
var usabilityExclusions = map[string]map[string]bool{
	"soul_linker":    {"TK_MISSION": true},
	"star_gladiator": {"TK_MISSION": true},
}

// filterSkills returns skills with any entry whose AegisName is in drop removed.
// A nil/empty drop is a no-op (returns skills unchanged).
func filterSkills(skills []data.ClassSkillEntry, drop map[string]bool) []data.ClassSkillEntry {
	if len(drop) == 0 {
		return skills
	}
	out := make([]data.ClassSkillEntry, 0, len(skills))
	for _, e := range skills {
		if drop[e.AegisName] {
			continue
		}
		out = append(out, e)
	}
	return out
}

// buildClassMap reads Hercules's skill_tree.conf, flattens inherits so
// every class's Skills slice carries its full allocatable set, and
// keys the result by shim-style class names ("taekwon", "high_wizard",
// etc.); matching what GenerateRequest.Class carries.
//
// herculesPath is relative to repoRoot when not absolute (mirrors the
// rest of cmd/build-catalog's path handling).
func buildClassMap(herculesPath, repoRoot string) (map[string]data.ClassSkillTree, error) {
	full := herculesPath
	if !filepath.IsAbs(full) {
		full = filepath.Join(repoRoot, full)
	}
	raw, err := data.LoadSkillTreesFromFile(full)
	if err != nil {
		return nil, err
	}
	// Flatten inherits in topological order. Hercules's file lists
	// classes in dependency order (Novice before Swordsman before Knight),
	// so a single forward pass is sufficient; each class's inherits
	// have already been flattened by the time we reach it.
	out := make(map[string]data.ClassSkillTree, len(raw))
	for _, c := range raw {
		flat := flattenSkills(c, out)
		key := classKeyFromHerculesName(c.Name)
		flat = filterSkills(flat, usabilityExclusions[key])
		out[key] = data.ClassSkillTree{
			Name:    c.Name,
			Inherit: c.Inherit,
			Skills:  flat,
		}
	}
	return out, nil
}

// flattenSkills returns the union of c.Skills with the already-flattened
// skill sets of each inherit parent. Higher-tier classes overwrite lower
// max-levels (Swordsman's NV_BASIC stays at Novice's level, but that's
// fine since Hercules doesn't override max-levels in skill_tree.conf).
func flattenSkills(c data.ClassSkillTree, parents map[string]data.ClassSkillTree) []data.ClassSkillEntry {
	seen := make(map[string]int, len(c.Skills))
	out := make([]data.ClassSkillEntry, 0, len(c.Skills))
	add := func(e data.ClassSkillEntry) {
		if _, ok := seen[e.AegisName]; ok {
			return
		}
		seen[e.AegisName] = len(out)
		out = append(out, e)
	}
	for _, parent := range c.Inherit {
		key := classKeyFromHerculesName(parent)
		if pt, ok := parents[key]; ok {
			for _, e := range pt.Skills {
				add(e)
			}
		}
	}
	for _, e := range c.Skills {
		add(e)
	}
	return out
}

// classKeyFromHerculesName converts a Hercules skill_tree.conf job name
// ("Taekwon", "High_Wizard", "Assassin Cross") to the shim-style key
// the orchestrator's GenerateRequest carries ("taekwon", "high_wizard",
// "assassin_cross"). Stripped to lowercase + underscores.
func classKeyFromHerculesName(name string) string {
	out := strings.ToLower(name)
	out = strings.ReplaceAll(out, " ", "_")
	return out
}
