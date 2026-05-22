// Package data is the iRO-IDed item / monster / skill / etc. catalogue the
// orchestrator and downstream tools query. The public types are emulator-
// agnostic; multiple RO server emulators (Hercules, rAthena) all expose the
// same iRO ID space, so any of them can serve as the data source. Callers
// pick one source per request; the cross-check tool under cmd/verify-mapping
// loads both and flags discrepancies.
//
// The calc shim owns its own translation from these iRO IDs to whatever
// id space its backend uses; that translation lives next to the backend,
// not in this package.
package data

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/levonn-dev/ro-builder/internal/data/sources/hercules"
	"github.com/levonn-dev/ro-builder/internal/data/sources/rathena"
)

// Mode picks pre-renewal or renewal mechanics. Items, skills, mob stats, and
// status formulas all branch on this. Default is PreRenewal; the API request
// shape carries the choice through to here, but Renewal is currently
// rejected at the boundary.
type Mode int

const (
	PreRenewal Mode = iota
	Renewal
)

func (m Mode) String() string {
	switch m {
	case PreRenewal:
		return "pre-renewal"
	case Renewal:
		return "renewal"
	}
	return fmt.Sprintf("Mode(%d)", int(m))
}

// DirName returns the per-mode directory name both Hercules and rAthena use
// inside their db/ trees ("pre-re" or "re"). Exported so cross-check tools
// can build emulator-rooted paths without re-implementing the mapping.
func (m Mode) DirName() string {
	switch m {
	case PreRenewal:
		return "pre-re"
	case Renewal:
		return "re"
	}
	return ""
}

// Source identifies which RO server-emulator's data to load. iRO IDs are
// the same across both; they're Gravity's canonical identifiers; but the
// emulators occasionally disagree on details (item naming, comment-out
// state of niche items, renewal-only entries). Picking a source matters
// when a specific row's Name / Type / scriptable bonuses are load-bearing.
type Source int

const (
	SourceHercules Source = iota
	SourceRathena
)

func (s Source) String() string {
	switch s {
	case SourceHercules:
		return "hercules"
	case SourceRathena:
		return "rathena"
	}
	return fmt.Sprintf("Source(%d)", int(s))
}

// Item is the iRO-IDed item record. Re-exported from the hercules source;
// rathena.Item is a type alias to the same underlying struct so callers
// can pass items between sources without conversion.
type Item = hercules.Item

// LoadItems reads the item database from a Hercules clone (default source).
// Kept as the no-frills entry point so existing callers don't have to pick
// a Source explicitly. For multi-source workflows use LoadItemsFrom.
func LoadItems(herculesRoot string, mode Mode) ([]Item, error) {
	return LoadItemsFrom(herculesRoot, mode, SourceHercules)
}

// LoadItemsFrom reads the item database from the named source. emulatorRoot
// is the root of the emulator's clone (the dir containing db/, conf/, etc).
func LoadItemsFrom(emulatorRoot string, mode Mode, source Source) ([]Item, error) {
	dir := mode.DirName()
	if dir == "" {
		return nil, fmt.Errorf("unknown mode: %s", mode)
	}
	switch source {
	case SourceHercules:
		return hercules.LoadItemDB(filepath.Join(emulatorRoot, "db", dir, "item_db.conf"))
	case SourceRathena:
		return rathena.LoadItemDB(emulatorRoot, dir)
	}
	return nil, fmt.Errorf("unknown source: %s", source)
}

// Mob is the iRO-IDed mob record. Re-exported from the hercules source;
// rathena.Mob is a type alias to the same struct so callers can pass mobs
// between sources without conversion.
type Mob = hercules.Mob

// MobThreats is the gate-relevant threat profile of a mob (status the
// mob applies, skills it casts). Re-exported from hercules for the same
// reason as Mob.
type MobThreats = hercules.MobThreats

// ClassSkillTree and ClassSkillEntry are the per-class allocatable skill
// tree from Hercules's db/pre-re/skill_tree.conf. Used by the orchestrator
// to inject the class's skills into the LLM's user prompt and by the
// list_class_skills tool. cmd/build-catalog flattens inherits at build
// time so runtime callers see one fully-expanded list per class.
type ClassSkillTree = hercules.ClassSkillTree
type ClassSkillEntry = hercules.ClassSkillEntry

// LoadSkillTrees reads Hercules's per-mode skill_tree.conf and returns
// one ClassSkillTree per class. Inherits are NOT flattened here; the
// caller (cmd/build-catalog) handles that pass.
func LoadSkillTrees(herculesRoot string, mode Mode) ([]ClassSkillTree, error) {
	dir := mode.DirName()
	if dir == "" {
		return nil, fmt.Errorf("unknown mode: %s", mode)
	}
	return hercules.LoadSkillTreeDB(filepath.Join(herculesRoot, "db", dir, "skill_tree.conf"))
}

// LoadSkillTreesFromFile reads a Hercules skill_tree.conf at an explicit
// path. Used by cmd/build-catalog when the caller wants pre-re always
// regardless of -source flag (skill_tree.conf is Hercules-only; rAthena
// uses a different format we don't parse).
func LoadSkillTreesFromFile(path string) ([]ClassSkillTree, error) {
	return hercules.LoadSkillTreeDB(path)
}

// JobMaxLevels is the per-class base + job level cap derived from
// Hercules's job_db.conf + exp_group_db.conf. Re-exported from hercules
// for the same reason as Mob/Item.
type JobMaxLevels = hercules.JobMaxLevels

// LoadJobMaxLevels reads Hercules's job_db.conf + exp_group_db.conf for
// the given mode and joins them into one JobMaxLevels per class. rAthena
// is not supported here; its job-db format is different from Hercules's
// libconfig dialect and we don't parse it. cmd/build-catalog always
// resolves max-levels from the Hercules clone.
func LoadJobMaxLevels(herculesRoot string, mode Mode) ([]JobMaxLevels, error) {
	dir := mode.DirName()
	if dir == "" {
		return nil, fmt.Errorf("unknown mode: %s", mode)
	}
	jobPath := filepath.Join(herculesRoot, "db", dir, "job_db.conf")
	expPath := filepath.Join(herculesRoot, "db", dir, "exp_group_db.conf")
	return hercules.LoadJobMaxLevelsDB(jobPath, expPath)
}

// LoadMobs reads the mob database from a Hercules clone (default source).
// Kept as the no-frills entry point so existing callers don't need to
// pick a Source explicitly. For multi-source workflows use LoadMobsFrom.
func LoadMobs(herculesRoot string, mode Mode) ([]Mob, error) {
	return LoadMobsFrom(herculesRoot, mode, SourceHercules)
}

// LoadMobsFrom reads the mob database from the named source. emulatorRoot
// is the root of the emulator's clone (the dir containing db/, conf/, etc).
func LoadMobsFrom(emulatorRoot string, mode Mode, source Source) ([]Mob, error) {
	dir := mode.DirName()
	if dir == "" {
		return nil, fmt.Errorf("unknown mode: %s", mode)
	}
	switch source {
	case SourceHercules:
		return hercules.LoadMobDB(filepath.Join(emulatorRoot, "db", dir, "mob_db.conf"))
	case SourceRathena:
		return rathena.LoadMobDB(emulatorRoot, dir)
	}
	return nil, fmt.Errorf("unknown source: %s", source)
}

// Skill is the iRO-IDed skill record. Re-exported from hercules; rathena
// uses the same struct via type alias, so callers can pass skills between
// sources without conversion.
type Skill = hercules.Skill

// LoadSkills reads the skill database from a Hercules clone (default
// source). For multi-source workflows use LoadSkillsFrom.
func LoadSkills(herculesRoot string, mode Mode) ([]Skill, error) {
	return LoadSkillsFrom(herculesRoot, mode, SourceHercules)
}

// LoadSkillsFrom reads the skill database from the named source.
// emulatorRoot is the root of the emulator's clone.
func LoadSkillsFrom(emulatorRoot string, mode Mode, source Source) ([]Skill, error) {
	dir := mode.DirName()
	if dir == "" {
		return nil, fmt.Errorf("unknown mode: %s", mode)
	}
	switch source {
	case SourceHercules:
		return hercules.LoadSkillDB(filepath.Join(emulatorRoot, "db", dir, "skill_db.conf"))
	case SourceRathena:
		return rathena.LoadSkillDB(emulatorRoot, dir)
	}
	return nil, fmt.Errorf("unknown source: %s", source)
}

// ParseSource turns a CLI / config string into a Source. Accepts the
// canonical names ("hercules", "rathena", case-insensitive) and rejects
// anything else. Used by build-time tools that take a source flag.
func ParseSource(s string) (Source, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "hercules":
		return SourceHercules, nil
	case "rathena":
		return SourceRathena, nil
	}
	return 0, fmt.Errorf("unknown source %q (valid: hercules, rathena)", s)
}
