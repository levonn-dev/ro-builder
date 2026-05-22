// verify-mapping cross-checks calc-sidecar/src/backends/rocalc/mapping.json
// against multiple RO server-emulator sources (Hercules and rAthena), flagging
// any iRO id where the sources disagree or one is missing it entirely.
//
// Usage:
//
//	go run ./cmd/verify-mapping
//
// Inputs (paths relative to repo root):
//
//	calc-sidecar/src/backends/rocalc/mapping.json   (the rocalc → iRO map)
//	../Hercules                                       (cloned HerculesWS/Hercules)
//	../rathena                                        (cloned rathena/rathena)
//
// Per source we union pre-re and renewal db files: many iRO items rocalc
// references are commented out in pre-re's item_db (Leo Crown 5588, Noah Hat
// 5759, etc.) and only fully spelled out in re/. The mapping tool already
// uses the same union when building mappings; the cross-check mirrors that
// so a "missing" report means the item is genuinely absent from the source
// rather than being filtered out by the pre-re commenting convention.
//
// Reports:
//   - missing_hercules: iRO ids in the mapping that no Hercules row carries.
//   - missing_rathena:  iRO ids in the mapping that no rAthena row carries.
//   - aegis_mismatch:   iRO ids both sources have but with diverging
//     AegisNames; typically a localization/translation
//     drift between the two emulators.
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/levonn-dev/ro-builder/internal/clitools"
	"github.com/levonn-dev/ro-builder/internal/data"
)

const (
	herculesRoot = "../Hercules"
	rathenaRoot  = "../rathena"
	mappingPath  = "calc-sidecar/src/backends/rocalc/mapping.json"
)

type mappingEntry struct {
	IRoID    int    `json:"iRO_id"`
	RocalcID int    `json:"rocalc_id"`
	Name     string `json:"name"`
}

type mappingFile struct {
	Items []mappingEntry `json:"items"`
	Cards []mappingEntry `json:"cards"`
}

type mismatch struct {
	IRoID         int
	RocalcName    string
	HerculesAegis string
	RathenaAegis  string
}

func main() {
	repoRoot, err := clitools.FindRepoRoot()
	if err != nil {
		slog.Default().Error("find repo root", slog.String("error", err.Error()))
		os.Exit(1)
	}

	mapping, err := readMapping(filepath.Join(repoRoot, mappingPath))
	if err != nil {
		slog.Default().Error("read mapping", slog.String("error", err.Error()))
		os.Exit(1)
	}
	slog.Default().Info("mapping loaded", slog.Int("items", len(mapping.Items)), slog.Int("cards", len(mapping.Cards)))

	hercByID, err := loadIndexBoth(filepath.Join(repoRoot, herculesRoot), data.SourceHercules)
	if err != nil {
		slog.Default().Error("load Hercules", slog.String("error", err.Error()))
		os.Exit(1)
	}
	rathByID, err := loadIndexBoth(filepath.Join(repoRoot, rathenaRoot), data.SourceRathena)
	if err != nil {
		slog.Default().Error("load rAthena", slog.String("error", err.Error()))
		os.Exit(1)
	}
	slog.Default().Info("Hercules indexed", slog.Int("items", len(hercByID)))
	slog.Default().Info("rAthena indexed", slog.Int("items", len(rathByID)))

	report := func(label string, entries []mappingEntry) {
		var (
			missingH, missingR int
			mismatches         []mismatch
		)
		for _, e := range entries {
			h, hOK := hercByID[e.IRoID]
			r, rOK := rathByID[e.IRoID]
			if !hOK {
				missingH++
			}
			if !rOK {
				missingR++
			}
			if hOK && rOK && h.AegisName != "" && r.AegisName != "" && h.AegisName != r.AegisName {
				mismatches = append(mismatches, mismatch{
					IRoID: e.IRoID, RocalcName: e.Name,
					HerculesAegis: h.AegisName, RathenaAegis: r.AegisName,
				})
			}
		}
		slog.Default().Info("cross-check",
			slog.String("type", label),
			slog.Int("total", len(entries)),
			slog.Int("missing_hercules", missingH),
			slog.Int("missing_rathena", missingR),
			slog.Int("aegis_mismatch", len(mismatches)),
		)
		if len(mismatches) > 0 {
			sort.Slice(mismatches, func(i, j int) bool { return mismatches[i].IRoID < mismatches[j].IRoID })
			fmt.Printf("\n=== %s aegis-name divergences (Hercules vs rAthena) ===\n", label)
			for _, m := range mismatches {
				fmt.Printf("  iRO %5d   herc=%-30s  rath=%-30s   rocalc=%q\n",
					m.IRoID, m.HerculesAegis, m.RathenaAegis, m.RocalcName)
			}
		}
	}
	report("items", mapping.Items)
	report("cards", mapping.Cards)
}

// loadIndexBoth loads an emulator's pre-re and renewal item DBs and returns
// a single id-keyed index. Pre-re entries take precedence on collision so
// pre-re-correct names win when an item exists in both files; renewal-only
// rows fill the gaps for items pre-re emulators commonly comment out.
func loadIndexBoth(emulatorRoot string, source data.Source) (map[int]data.Item, error) {
	preRe, err := data.LoadItemsFrom(emulatorRoot, data.PreRenewal, source)
	if err != nil {
		return nil, fmt.Errorf("pre-re: %w", err)
	}
	re, err := data.LoadItemsFrom(emulatorRoot, data.Renewal, source)
	if err != nil {
		return nil, fmt.Errorf("renewal: %w", err)
	}
	idx := make(map[int]data.Item, len(preRe)+len(re))
	// Pre-re wins on overlap (matching the mapping tool's policy).
	for _, it := range re {
		idx[it.ID] = it
	}
	for _, it := range preRe {
		idx[it.ID] = it
	}
	return idx, nil
}

func readMapping(path string) (*mappingFile, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m mappingFile
	if err := json.Unmarshal(src, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
