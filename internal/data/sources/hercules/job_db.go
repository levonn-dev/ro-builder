package hercules

import (
	"fmt"
	"os"
	"sort"
)

// JobMaxLevels carries the base and job level caps for one class,
// derived by joining Hercules's job_db.conf against exp_group_db.conf:
//
//	job_db.conf:   Job_Name → (BaseExpGroup, JobExpGroup)
//	exp_group_db:  group name → MaxLevel
//
// The join is JobName.MaxBaseLevel = BaseExpGroups[BaseExpGroup].MaxLevel,
// JobName.MaxJobLevel = JobExpGroups[JobExpGroup].MaxLevel.
//
// Name preserves Hercules's original casing ("Assassin_Cross",
// "Swordsman_High"); cmd/build-catalog converts to shim-style keys at
// the boundary the same way it does for skill trees.
type JobMaxLevels struct {
	Name         string `json:"name"`
	MaxBaseLevel int    `json:"max_base_level"`
	MaxJobLevel  int    `json:"max_job_level"`
	BaseExpGroup string `json:"base_exp_group,omitempty"`
	JobExpGroup  string `json:"job_exp_group,omitempty"`
}

// JobExpRefs is one job's references into the exp-group tables. Returned
// by LoadJobDB so callers can join against exp_group_db.conf themselves
// (LoadJobMaxLevelsDB does the join for the common case).
type JobExpRefs struct {
	BaseExpGroup string
	JobExpGroup  string
}

// LoadJobDB parses Hercules's job_db.conf and returns each job's
// (BaseExpGroup, JobExpGroup) references. The file is a flat sequence
// of `JobName: { ... }` entries with no surrounding root identifier;
// same multi-root shape as skill_tree.conf, parsed the same way.
//
// Per-job HPTable, SPTable, BaseASPD, etc. are ignored; only the two
// exp-group reference strings flow through. Jobs missing either field
// keep an empty string for the corresponding ref.
func LoadJobDB(path string) (map[string]JobExpRefs, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	t := newTokenizer(src)
	out := make(map[string]JobExpRefs)
	for {
		tk, err := t.peek()
		if err != nil {
			return nil, err
		}
		if tk.typ == tokEOF {
			break
		}
		if tk.typ != tokIdent {
			return nil, fmt.Errorf("%s: expected job name, got %s", path, tk)
		}
		nameTok, _ := t.next()
		if _, err := t.expect(tokColon); err != nil {
			return nil, err
		}
		val, err := parseValue(t)
		if err != nil {
			return nil, err
		}
		obj, ok := val.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: %s value should be an object, got %T", path, nameTok.val, val)
		}
		refs := JobExpRefs{}
		if s, ok := obj["BaseExpGroup"].(string); ok {
			refs.BaseExpGroup = s
		}
		if s, ok := obj["JobExpGroup"].(string); ok {
			refs.JobExpGroup = s
		}
		out[nameTok.val] = refs
	}
	return out, nil
}

// ExpGroupMaxLevels carries the parsed MaxLevel for each entry in
// exp_group_db.conf, separated by which top-level db the group lives in.
type ExpGroupMaxLevels struct {
	Base map[string]int // base_exp_group_db: group name → MaxLevel
	Job  map[string]int // job_exp_group_db:  group name → MaxLevel
}

// LoadExpGroupDB parses Hercules's exp_group_db.conf and returns the
// MaxLevel for each entry in base_exp_group_db and job_exp_group_db.
//
// The file has two top-level entries (base_exp_group_db, job_exp_group_db),
// each containing { GroupName: { MaxLevel: N, Exp: [...] } } pairs. We
// only extract MaxLevel; the Exp tables are ignored.
func LoadExpGroupDB(path string) (ExpGroupMaxLevels, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return ExpGroupMaxLevels{}, fmt.Errorf("read %s: %w", path, err)
	}
	t := newTokenizer(src)
	out := ExpGroupMaxLevels{
		Base: make(map[string]int),
		Job:  make(map[string]int),
	}
	for {
		tk, err := t.peek()
		if err != nil {
			return ExpGroupMaxLevels{}, err
		}
		if tk.typ == tokEOF {
			break
		}
		if tk.typ != tokIdent {
			return ExpGroupMaxLevels{}, fmt.Errorf("%s: expected root identifier, got %s", path, tk)
		}
		rootTok, _ := t.next()
		if _, err := t.expect(tokColon); err != nil {
			return ExpGroupMaxLevels{}, err
		}
		val, err := parseValue(t)
		if err != nil {
			return ExpGroupMaxLevels{}, err
		}
		obj, ok := val.(map[string]any)
		if !ok {
			return ExpGroupMaxLevels{}, fmt.Errorf("%s: %s value should be an object, got %T", path, rootTok.val, val)
		}
		var target map[string]int
		switch rootTok.val {
		case "base_exp_group_db":
			target = out.Base
		case "job_exp_group_db":
			target = out.Job
		default:
			// Unknown top-level entry; skip. Renewer might add new dbs;
			// no reason to fail the parse just because we don't index it.
			continue
		}
		for groupName, groupVal := range obj {
			groupObj, ok := groupVal.(map[string]any)
			if !ok {
				continue
			}
			if maxLevel, ok := groupObj["MaxLevel"].(int); ok {
				target[groupName] = maxLevel
			}
		}
	}
	return out, nil
}

// LoadJobMaxLevelsDB is the common-case entry point: parse both files,
// join, return one JobMaxLevels per job. Jobs that reference an unknown
// exp group end up with the corresponding MaxLevel at zero; callers
// can detect by comparing against the empty string.
func LoadJobMaxLevelsDB(jobDBPath, expGroupDBPath string) ([]JobMaxLevels, error) {
	refs, err := LoadJobDB(jobDBPath)
	if err != nil {
		return nil, err
	}
	groups, err := LoadExpGroupDB(expGroupDBPath)
	if err != nil {
		return nil, err
	}
	out := make([]JobMaxLevels, 0, len(refs))
	for name, r := range refs {
		out = append(out, JobMaxLevels{
			Name:         name,
			BaseExpGroup: r.BaseExpGroup,
			JobExpGroup:  r.JobExpGroup,
			MaxBaseLevel: groups.Base[r.BaseExpGroup],
			MaxJobLevel:  groups.Job[r.JobExpGroup],
		})
	}
	// refs is a Go map, so ranging it yields a randomized order; sort by
	// name to keep the emitted job_max_levels list reproducible.
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
