// build-rocalc-mapping joins rocalc's internal item/card/monster IDs with
// Hercules's iRO IDs by name, producing the lookup table the rocalc shim
// consumes at runtime so callers can speak iRO IDs end to end.
//
// Usage:
//
//	go run ./cmd/build-rocalc-mapping
//
// Inputs (paths relative to repo root):
//
//	../Hercules/db/pre-re/item_db.conf       (Hercules iRO catalog)
//	../Hercules/db/pre-re/mob_db.conf        (Hercules iRO mobs)
//	calc-sidecar/vendor/rocalc/item_*.js     (rocalc m_Item)
//	calc-sidecar/vendor/rocalc/card_*.js     (rocalc m_Card)
//	calc-sidecar/vendor/rocalc/monster_*.js  (rocalc m_Monster)
//	cmd/build-rocalc-mapping/manual_overrides.json   (optional, hand-resolved entries)
//
// Outputs:
//
//	calc-sidecar/src/backends/rocalc/mapping.json     (runtime lookup)
//	cmd/build-rocalc-mapping/unmatched-items.txt      (review)
//	cmd/build-rocalc-mapping/unmatched-cards.txt      (review)
//	cmd/build-rocalc-mapping/unmatched-mobs.txt       (review)
//
// For each rocalc entry we first check manual_overrides, then look up by
// exact name in Hercules. Cards get a " Card" suffix appended to the rocalc
// name before joining (rocalc names cards bare like "Andre"; Hercules calls
// them "Andre Card"). Unmatched entries land in the per-type review files
// and don't fail the run; the expectation is iterative refinement: review,
// add manual overrides, re-run.
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/dop251/goja"
	"github.com/levonn-dev/ro-builder/internal/clitools"
	"github.com/levonn-dev/ro-builder/internal/data"
	"github.com/levonn-dev/ro-builder/internal/data/sources/rathena"
)

const (
	herculesRoot      = "../Hercules"
	rathenaRoot       = "../rathena"
	rocalcVendorDir   = "calc-sidecar/vendor/rocalc"
	rocalcItemFile    = "item_2026-04-06.js"
	rocalcCardFile    = "card_2026-04-06.js"
	rocalcMonsterFile = "monster_2026-04-06.js"
	mappingOutPath    = "calc-sidecar/src/backends/rocalc/mapping.json"
	overridesPath     = "cmd/build-rocalc-mapping/manual_overrides.json"
	unmatchedItemsOut = "cmd/build-rocalc-mapping/unmatched-items.txt"
	unmatchedCardsOut = "cmd/build-rocalc-mapping/unmatched-cards.txt"
	unmatchedMobsOut  = "cmd/build-rocalc-mapping/unmatched-mobs.txt"
)

// rocalc preamble; minimum stubs/globals to evaluate the data files in goja
// without head.js. Same set the previous internal/data parser used.
const rocalcPreamble = `
var PvP = 1;
var SRV = 0;
function skillName(num) { return "[skill " + num + "]"; }
function skillNameInSelect(num) { return "[skill " + num + "]"; }
function Option(label, value) {}
var v_Race = ["<b style='color:#9F9E9B'>Formless</b>","<b style='color:purple'>Undead</b>","<b style='color:#7C7C7C'>Brute</b>","<b style='color:green'>Plant</b>","<b style='color:#0A8DEC'>Insect</b>","<b style='color:#A89682'>Fish</b>","<b style='color:#FF55E0'>Demon</b>","<b style='color:#0078E1'>Demi-Human</b>","<b style='color:#999999'>Angel</b>","<b style='color:red'>Dragon</b>"];
var v_Element = ["<b style='color:#A89682'>Neutral</b>","<b style='color:blue'>Water</b>","<b style='color:brown'>Earth</b>","<b style='color:red'>Fire</b>","<b style='color:green'>Wind</b>","<b style='color:purple'>Poison</b>","<b style='color:#999999'>Holy</b>","<b style='color:#FF55E0'>Shadow</b>","<b style='color:black'>Ghost</b>","<b style='color:purple'>Undead</b>"];
var v_Size = ["Small","Medium","Large"];
`

type mappingEntry struct {
	IRoID    int    `json:"iRO_id"`
	RocalcID int    `json:"rocalc_id"`
	Name     string `json:"name"`
}

type mapping struct {
	Version   int            `json:"version"`
	Comment   string         `json:"comment"`
	Items     []mappingEntry `json:"items"`
	Cards     []mappingEntry `json:"cards"`
	Mobs      []mappingEntry `json:"mobs"`
	Generated map[string]int `json:"counts"` // matched / unmatched per type
}

type overrides struct {
	Items         map[string]int `json:"items"` // rocalc_id (string) -> iRO_id
	Cards         map[string]int `json:"cards"`
	Mobs          map[string]int `json:"mobs"`
	ExcludedItems []int          `json:"excluded_items"` // rocalc IDs intentionally not mapped
	ExcludedCards []int          `json:"excluded_cards"` // (server-specific entries; see notes section)
	ExcludedMobs  []int          `json:"excluded_mobs"`
}

type rocalcEntry struct {
	ID   int
	Name string
	// Disabled is true when rocalc has marked this row as not selectable in
	// its UI. Detected via the sentinel dispLoc=999 / jobMask=999 pattern in
	// m_Item; rocalc keeps the row in its data file but filters it out of
	// every option-list dropdown. We treat these the same as the explicit
	// excluded_items list: the row exists for rocalc-internal reasons but
	// has no iRO-id mapping target.
	Disabled bool
}

func main() {
	if err := run(); err != nil {
		slog.Default().Error("build-rocalc-mapping failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	repoRoot, err := clitools.FindRepoRoot()
	if err != nil {
		return err
	}

	hercItems, err := data.LoadItems(filepath.Join(repoRoot, herculesRoot), data.PreRenewal)
	if err != nil {
		return fmt.Errorf("load Hercules pre-re items: %w", err)
	}
	slog.Default().Info("loaded Hercules pre-re items", slog.Int("count", len(hercItems)))

	// Many iRO items pre-re-server-emulators choose not to ship are commented
	// out in Hercules pre-re's item_db.conf (Leo Crown 5588, Noah Hat 5759,
	// Frigga's/Fricca's Circlet 5124, etc.). The iRO IDs themselves are still
	// canonical; server emulator opinions don't change what id Gravity
	// assigned. We also load Hercules's renewal db/re/item_db.conf and append
	// its entries as additional name sources. Two important points:
	//   1. Items in BOTH dirs (same iRO ID) often have *different display
	//      Names* in pre-re vs renewal (e.g. id 5488 = "Twin Santa Hat" in
	//      pre-re, "Cute Santa Hat" in renewal). Both names should index to
	//      the same id, so we keep both entries; the herculesIndex's
	//      first-seen-wins dedupe across (name, id) pairs prevents collisions
	//      while letting alternate names from RE land for items that have
	//      them.
	//   2. Pre-re entries are appended first so on a true name collision
	//      between distinct items the pre-re item wins.
	hercItemsRE, err := data.LoadItems(filepath.Join(repoRoot, herculesRoot), data.Renewal)
	if err != nil {
		return fmt.Errorf("load Hercules re items: %w", err)
	}
	slog.Default().Info("loaded Hercules re items (additional name source)", slog.Int("count", len(hercItemsRE)))
	hercItems = append(hercItems, hercItemsRE...)
	slog.Default().Info("combined index size", slog.Int("entries", len(hercItems)))

	rocalcItems, err := loadRocalcArray(filepath.Join(repoRoot, rocalcVendorDir, rocalcItemFile), "m_Item", 8)
	if err != nil {
		return fmt.Errorf("load rocalc items: %w", err)
	}
	slog.Default().Info("loaded rocalc items", slog.Int("count", len(rocalcItems)))

	rocalcCards, err := loadRocalcArray(filepath.Join(repoRoot, rocalcVendorDir, rocalcCardFile), "m_Card", 2)
	if err != nil {
		return fmt.Errorf("load rocalc cards: %w", err)
	}
	slog.Default().Info("loaded rocalc cards", slog.Int("count", len(rocalcCards)))

	// Mobs use the same pre-re ∪ re union as items, plus rAthena as a
	// secondary name source. The two emulators occasionally disagree on
	// mob names; Hercules's mob_db has more legacy/kRO-leaning names with
	// the iRO name in JName, while rAthena tends to put the iRO name
	// directly in Name. Pulling from both means the rocalc-name join finds
	// matches whichever emulator put the iRO form where we look first.
	mobs, err := loadMobsAllSources(repoRoot)
	if err != nil {
		return fmt.Errorf("load mobs: %w", err)
	}

	rocalcMonsters, err := loadRocalcArray(filepath.Join(repoRoot, rocalcVendorDir, rocalcMonsterFile), "m_Monster", 1)
	if err != nil {
		return fmt.Errorf("load rocalc monsters: %w", err)
	}
	slog.Default().Info("loaded rocalc monsters", slog.Int("count", len(rocalcMonsters)))

	// Pull lvl/hp per rocalc mob id for the bracket-variant stat-disambig
	// pass below. m_Monster row layout: [id, name, race, element, size,
	// lvl(5), hp(6), atk1, atk2, def, mdef, ...].
	rocalcMobStats, err := loadRocalcMobStats(filepath.Join(repoRoot, rocalcVendorDir, rocalcMonsterFile))
	if err != nil {
		return fmt.Errorf("load rocalc mob stats: %w", err)
	}

	ov, err := loadOverrides(filepath.Join(repoRoot, overridesPath))
	if err != nil {
		return fmt.Errorf("load overrides: %w", err)
	}
	slog.Default().Info("loaded overrides",
		slog.Int("items", len(ov.Items)),
		slog.Int("cards", len(ov.Cards)),
		slog.Int("mobs", len(ov.Mobs)),
	)

	hercItemByName, hercCardByName := indexHerculesByName(hercItems)
	slog.Default().Info("Hercules indexed",
		slog.Int("non_card_items", len(hercItemByName.byName)),
		slog.Int("cards", len(hercCardByName.byName)),
	)

	mobByName := indexHerculesMobsByName(mobs)
	slog.Default().Info("mob index built", slog.Int("entries", len(mobByName.byName)))

	excludedItems := make(map[int]bool, len(ov.ExcludedItems))
	for _, id := range ov.ExcludedItems {
		excludedItems[id] = true
	}
	excludedCards := make(map[int]bool, len(ov.ExcludedCards))
	for _, id := range ov.ExcludedCards {
		excludedCards[id] = true
	}
	excludedMobs := make(map[int]bool, len(ov.ExcludedMobs))
	for _, id := range ov.ExcludedMobs {
		excludedMobs[id] = true
	}
	itemsMatched, itemsUnmatched, itemsSkipped, itemsExcluded := joinByName(rocalcItems, hercItemByName, ov.Items, excludedItems, "")
	cardsMatched, cardsUnmatched, cardsSkipped, cardsExcluded := joinByName(rocalcCards, hercCardByName, ov.Cards, excludedCards, " Card")
	mobsMatched, mobsUnmatched, mobsSkipped, mobsExcluded := joinByName(rocalcMonsters, mobByName, ov.Mobs, excludedMobs, "")

	// Regional-variant inheritance: rocalc lists "Leo Crown" and
	// "Leo Crown (aRO)" as separate rows but they're the same iRO item;
	// reuse the base's iRO id for any unmatched regional variant.
	itemsMatched, itemsUnmatched, inheritedItems := inheritRegional(itemsMatched, itemsUnmatched, rocalcItems)
	cardsMatched, cardsUnmatched, inheritedCards := inheritRegional(cardsMatched, cardsUnmatched, rocalcCards)
	mobsMatched, mobsUnmatched, inheritedMobs := inheritRegional(mobsMatched, mobsUnmatched, rocalcMonsters)

	// Stat-based disambiguation: for unmatched bracket-variant rocalc mobs
	// ("Goblin [Axe]", "Venatu [Water]"), look up same-base-name candidates
	// in the mob index and pick the unique one whose lvl+hp match rocalc's
	// m_Monster row. Resolves cases where the bracket encodes a sprite-
	// distinguished iRO mob (most of them) without needing manual review.
	mobsByName := indexMobsByDisplayName(mobs)
	statMatches, mobsUnmatched, statResolved := disambiguateMobsByStats(mobsUnmatched, rocalcMobStats, mobsByName)
	mobsMatched = append(mobsMatched, statMatches...)
	sort.Slice(mobsMatched, func(i, j int) bool { return mobsMatched[i].RocalcID < mobsMatched[j].RocalcID })

	slog.Default().Info("items matched",
		slog.Int("matched", len(itemsMatched)),
		slog.Int("via_regional_inherit", inheritedItems),
		slog.Int("unmatched", len(itemsUnmatched)),
		slog.Int("skipped_placeholder", itemsSkipped),
		slog.Int("excluded_server_specific", itemsExcluded),
	)
	slog.Default().Info("cards matched",
		slog.Int("matched", len(cardsMatched)),
		slog.Int("via_regional_inherit", inheritedCards),
		slog.Int("unmatched", len(cardsUnmatched)),
		slog.Int("skipped_placeholder", cardsSkipped),
		slog.Int("excluded_server_specific", cardsExcluded),
	)
	slog.Default().Info("mobs matched",
		slog.Int("matched", len(mobsMatched)),
		slog.Int("via_regional", inheritedMobs),
		slog.Int("via_stat_disambig", statResolved),
		slog.Int("unmatched", len(mobsUnmatched)),
		slog.Int("skipped_placeholder", mobsSkipped),
		slog.Int("excluded_server_specific", mobsExcluded),
	)

	out := mapping{
		Version: 1,
		Comment: "rocalc-id ↔ iRO-id; produced by cmd/build-rocalc-mapping. Do not hand-edit; see manual_overrides.json instead.",
		Items:   itemsMatched,
		Cards:   cardsMatched,
		Mobs:    mobsMatched,
		Generated: map[string]int{
			"items_matched":   len(itemsMatched),
			"items_unmatched": len(itemsUnmatched),
			"cards_matched":   len(cardsMatched),
			"cards_unmatched": len(cardsUnmatched),
			"mobs_matched":    len(mobsMatched),
			"mobs_unmatched":  len(mobsUnmatched),
		},
	}
	if err := writeMapping(filepath.Join(repoRoot, mappingOutPath), out); err != nil {
		return fmt.Errorf("write mapping: %w", err)
	}
	if err := writeUnmatched(filepath.Join(repoRoot, unmatchedItemsOut), itemsUnmatched); err != nil {
		return fmt.Errorf("write unmatched items: %w", err)
	}
	if err := writeUnmatched(filepath.Join(repoRoot, unmatchedCardsOut), cardsUnmatched); err != nil {
		return fmt.Errorf("write unmatched cards: %w", err)
	}
	if err := writeUnmatched(filepath.Join(repoRoot, unmatchedMobsOut), mobsUnmatched); err != nil {
		return fmt.Errorf("write unmatched mobs: %w", err)
	}
	slog.Default().Info("wrote outputs",
		slog.String("mapping", mappingOutPath),
		slog.String("unmatched_items", unmatchedItemsOut),
		slog.String("unmatched_cards", unmatchedCardsOut),
		slog.String("unmatched_mobs", unmatchedMobsOut),
	)
	return nil
}

// loadMobsAllSources returns the union of mobs from Hercules and rAthena,
// pre-re and renewal each. Pre-re and renewal entries from a given source
// are concatenated as separate rows; the herculesIndex.add() function
// de-dupes by (name, id), so the same mob appearing in both pre-re and re
// of the same source contributes its base names once. Across sources,
// duplicate (name, id) pairs are also collapsed; first-seen wins which is
// fine since the iRO id space is canonical regardless of source.
func loadMobsAllSources(repoRoot string) ([]data.Mob, error) {
	type src struct {
		root, label string
		source      data.Source
	}
	sources := []src{
		{root: herculesRoot, label: "Hercules", source: data.SourceHercules},
		{root: rathenaRoot, label: "rAthena", source: data.SourceRathena},
	}
	var combined []data.Mob
	for _, s := range sources {
		preRe, err := loadMobsFrom(filepath.Join(repoRoot, s.root), data.PreRenewal, s.source)
		if err != nil {
			return nil, fmt.Errorf("%s pre-re: %w", s.label, err)
		}
		slog.Default().Info("loaded pre-re mobs", slog.String("source", s.label), slog.Int("count", len(preRe)))
		re, err := loadMobsFrom(filepath.Join(repoRoot, s.root), data.Renewal, s.source)
		if err != nil {
			return nil, fmt.Errorf("%s renewal: %w", s.label, err)
		}
		slog.Default().Info("loaded renewal mobs (additional name source)", slog.String("source", s.label), slog.Int("count", len(re)))
		combined = append(combined, preRe...)
		combined = append(combined, re...)
	}
	return combined, nil
}

// loadMobsFrom is the source-aware mob loader. internal/data only exposes
// data.LoadMobs (Hercules-only) for now since the runtime path always uses
// Hercules; the mapping tool is the one place that benefits from also
// pulling rAthena's names, so the dispatch lives here rather than promoting
// it into data.LoadMobsFrom (premature abstraction).
func loadMobsFrom(emulatorRoot string, mode data.Mode, source data.Source) ([]data.Mob, error) {
	switch source {
	case data.SourceHercules:
		return data.LoadMobs(emulatorRoot, mode)
	case data.SourceRathena:
		return rathena.LoadMobDB(emulatorRoot, mode.DirName())
	}
	return nil, fmt.Errorf("unknown source: %s", source)
}

// rocalcMobStats holds the per-mob fields the stat-disambiguation pass
// uses (lvl, hp). Filled by loadRocalcMobStats from m_Monster row indices
// 5 and 6.
type rocalcMobStats struct {
	Lv int
	Hp int
}

// loadRocalcMobStats evaluates m_Monster in goja and pulls lvl + hp for
// each row, keyed by rocalc id. Used by disambiguateMobsByStats to resolve
// bracket-variant rocalc names against same-named Hercules/rAthena
// candidates. Skips rows with no name (the m_Monster tail has two non-
// monster summary rows that the joinByName pass already filters out).
func loadRocalcMobStats(path string) (map[int]rocalcMobStats, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	vm := goja.New()
	if _, err := vm.RunString(rocalcPreamble); err != nil {
		return nil, fmt.Errorf("preamble: %w", err)
	}
	if _, err := vm.RunString(string(src)); err != nil {
		return nil, fmt.Errorf("eval %s: %w", path, err)
	}
	val := vm.Get("m_Monster")
	if val == nil || goja.IsUndefined(val) {
		return nil, fmt.Errorf("m_Monster not defined")
	}
	rows, ok := val.Export().([]any)
	if !ok {
		return nil, fmt.Errorf("m_Monster is not an array")
	}
	out := make(map[int]rocalcMobStats, len(rows))
	for _, r := range rows {
		row, ok := r.([]any)
		if !ok || len(row) < 7 {
			continue
		}
		// Skip non-monster trailer rows (last two entries); they have no
		// string name at index 1.
		if _, isString := row[1].(string); !isString {
			continue
		}
		idVal, ok := row[0].(int64)
		if !ok {
			continue
		}
		var stats rocalcMobStats
		if v, ok := row[5].(int64); ok {
			stats.Lv = int(v)
		}
		if v, ok := row[6].(int64); ok {
			stats.Hp = int(v)
		}
		out[int(idVal)] = stats
	}
	return out, nil
}

// indexMobsByDisplayName builds a lower-cased name → []data.Mob multimap
// for stat-disambiguation. Both Hercules's display Name and JName are
// indexed since the iRO display name might live in either field
// (Wanderer/Wander Man, Zealotus/Zherlthsh; see indexHerculesMobsByName).
// Same iRO id appearing in multiple sources (Hercules pre-re ∪ re,
// rAthena pre-re ∪ re) lands as one entry per (name, source-row); the
// disambiguation pass de-dupes by id when picking a match.
func indexMobsByDisplayName(mobs []data.Mob) map[string][]data.Mob {
	out := make(map[string][]data.Mob)
	for _, m := range mobs {
		for _, n := range []string{m.Name, m.JName} {
			if n == "" {
				continue
			}
			key := strings.ToLower(n)
			out[key] = append(out[key], m)
		}
	}
	return out
}

// disambiguateMobsByStats resolves bracket-variant rocalc names ("Goblin
// [Axe]") by looking up same-base-name candidates and picking the one
// whose lvl + hp match rocalc's m_Monster row.
//
// Returns:
//   - newMatches:     entries the stat pass resolved
//   - stillUnmatched: entries with no bracket / no stats / ambiguous-by-stats
//   - resolved:       count for logging
func disambiguateMobsByStats(
	unmatched []rocalcEntry,
	rocalcStats map[int]rocalcMobStats,
	mobsByName map[string][]data.Mob,
) (newMatches []mappingEntry, stillUnmatched []rocalcEntry, resolved int) {
	for _, u := range unmatched {
		base, hasBracket := stripMobBracket(u.Name)
		if !hasBracket {
			stillUnmatched = append(stillUnmatched, u)
			continue
		}
		candidates := mobsByName[strings.ToLower(base)]
		if len(candidates) == 0 {
			stillUnmatched = append(stillUnmatched, u)
			continue
		}
		rs, hasStats := rocalcStats[u.ID]
		if !hasStats || rs.Lv == 0 || rs.Hp == 0 {
			stillUnmatched = append(stillUnmatched, u)
			continue
		}
		// Find candidates whose lvl+hp match rocalc's row exactly. De-dupe
		// by iRO id since the same mob lands multiple times across the four
		// (Hercules, rAthena) × (pre-re, re) combinations and might have
		// slightly different stats per row; first stats-match wins per id.
		idSet := make(map[int]bool)
		var statMatches []data.Mob
		for _, c := range candidates {
			if c.Lv == rs.Lv && c.Hp == rs.Hp && !idSet[c.ID] {
				idSet[c.ID] = true
				statMatches = append(statMatches, c)
			}
		}
		if len(statMatches) == 1 {
			newMatches = append(newMatches, mappingEntry{IRoID: statMatches[0].ID, RocalcID: u.ID, Name: u.Name})
			resolved++
			continue
		}
		stillUnmatched = append(stillUnmatched, u)
	}
	sort.Slice(stillUnmatched, func(i, j int) bool { return stillUnmatched[i].ID < stillUnmatched[j].ID })
	return newMatches, stillUnmatched, resolved
}

// stripMobBracket removes a trailing " [X]"; rocalc's mob variant marker.
// Distinct from stripRegionalSuffix which handles "(xRO)" item-region
// markers; both formats coexist (a few rocalc rows have both).
func stripMobBracket(name string) (base string, hadBracket bool) {
	if i := strings.LastIndex(name, " ["); i > 0 && strings.HasSuffix(name, "]") {
		return name[:i], true
	}
	return name, false
}

// indexHerculesMobsByName indexes mobs by display Name, SpriteName, and
// JName so joinByName can find rocalc monsters under any of the three.
//
// JName matters specifically for mobs: rocalc uses the iRO name as its row
// label, but Hercules's display Name field is sometimes the kRO name with
// the iRO name shoved into JName. Concrete cases:
//
//	rocalc "Wanderer"       → Hercules Name="Wander Man",   JName="Wanderer"
//	rocalc "Zealotus"       → Hercules Name="Zherlthsh",    JName="Zealotus"
//	rocalc "Abysmal Knight" → Hercules Name="Knight of Abyss", JName="Abysmal Knight"
//
// SpriteName ("PORING", "EDDGA") is parallel to Item.AegisName and helps in
// the rare case rocalc carries the eAthena ident form instead of display.
func indexHerculesMobsByName(mobs []data.Mob) herculesIndex {
	idx := newHerculesIndex()
	for _, m := range mobs {
		if m.Name == "" && m.SpriteName == "" && m.JName == "" {
			continue
		}
		idx.add(m.Name, m.SpriteName, m.ID)
		// JName is the iRO display name in many rows. Index it via the same
		// path the AegisName takes so the underscore→space normalization
		// catches it, and prefer the first-seen mapping (Hercules lists
		// iRO-canonical mobs in id order, so iRO 1002 = Poring wins over
		// any later "Poring" alias for variants).
		if m.JName != "" && m.JName != m.Name {
			idx.add("", m.JName, m.ID)
		}
	}
	return idx
}

// loadRocalcArray evaluates one rocalc data file in a goja vm and returns
// the named global (m_Item or m_Card) as rocalcEntry rows. nameIdx is the
// position of the display name in each row; 8 for m_Item, 2 for m_Card.
//
// For m_Item rows we also pull the dispLoc field at index 1; rocalc uses
// 999 as a "disabled / not selectable" sentinel (Breeze Whip 495, all the
// (aRO) renewal sets, etc). Card rows have no dispLoc; that field is
// inherent to weapon/armor placement and isn't meaningful for cards.
func loadRocalcArray(path, globalName string, nameIdx int) ([]rocalcEntry, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	vm := goja.New()
	if _, err := vm.RunString(rocalcPreamble); err != nil {
		return nil, fmt.Errorf("preamble: %w", err)
	}
	if _, err := vm.RunString(string(src)); err != nil {
		return nil, fmt.Errorf("eval %s: %w", path, err)
	}
	val := vm.Get(globalName)
	if val == nil || goja.IsUndefined(val) {
		return nil, fmt.Errorf("%s not defined after evaluating %s", globalName, path)
	}
	rows, ok := val.Export().([]any)
	if !ok {
		return nil, fmt.Errorf("%s is not an array (got %T)", globalName, val.Export())
	}
	var out []rocalcEntry
	for _, r := range rows {
		row, ok := r.([]any)
		if !ok || len(row) <= nameIdx {
			continue
		}
		idVal := row[0]
		var id int
		switch n := idVal.(type) {
		case int64:
			id = int(n)
		case float64:
			id = int(n)
		default:
			continue
		}
		name, _ := row[nameIdx].(string)
		if name == "" {
			continue
		}
		// For m_Item: row[1] is dispLoc; 999 = rocalc-disabled (filtered from
		// UI dropdowns). m_Card has no equivalent so this stays false there.
		disabled := false
		if globalName == "m_Item" && len(row) > 1 {
			if v, ok := row[1].(int64); ok && v == 999 {
				disabled = true
			}
		}
		out = append(out, rocalcEntry{ID: id, Name: name, Disabled: disabled})
	}
	return out, nil
}

// herculesIndex is two views of the same items: exact-Name and lowered-Name.
// We try exact first to avoid spurious matches between distinct items whose
// names differ only in case, and fall back to lowered when the exact lookup
// fails (rocalc capitalization is inconsistent: "Two-handed Sword" vs
// Hercules's "Two-Handed Sword").
type herculesIndex struct {
	byName  map[string]int
	byLower map[string]int
}

func newHerculesIndex() herculesIndex {
	return herculesIndex{
		byName:  make(map[string]int),
		byLower: make(map[string]int),
	}
}

// add records the first Hercules entry seen with a given name/aegisname pair.
// Hercules lists items in ID order, so the base variant (e.g. Knife = 1201)
// is encountered before slotted variants (Knife_ = 1202 = 4-slot,
// Knife__ = 1203). Both share the display Name "Knife"; rocalc only carries
// the base, so picking the first-seen ID gives us the right join.
//
// AegisName is also indexed (with underscores normalized to spaces) because
// rocalc's display names sometimes match Hercules's AegisName rather than
// its display Name. Concrete cases: rocalc "Cutlas" matches AegisName
// "Cutlas" exactly while Hercules's display Name for the same item is
// "Cutlus" (Hercules's typo). Same pattern with "Knuckle Duster" → AegisName
// "Knuckle_Duster" but display "Knuckle Dusters".
func (h *herculesIndex) add(name, aegisName string, id int) {
	if name != "" {
		if _, exists := h.byName[name]; !exists {
			h.byName[name] = id
		}
		lower := strings.ToLower(name)
		if _, exists := h.byLower[lower]; !exists {
			h.byLower[lower] = id
		}
	}
	if aegisName != "" {
		// Underscore-to-space lets us match against rocalc's space-separated
		// display names. Both forms indexed for safety.
		spaced := strings.ReplaceAll(aegisName, "_", " ")
		if _, exists := h.byName[spaced]; !exists {
			h.byName[spaced] = id
		}
		lower := strings.ToLower(spaced)
		if _, exists := h.byLower[lower]; !exists {
			h.byLower[lower] = id
		}
	}
}

func (h *herculesIndex) lookup(name string) (int, bool) {
	if id, ok := h.byName[name]; ok {
		return id, true
	}
	if id, ok := h.byLower[strings.ToLower(name)]; ok {
		return id, true
	}
	return 0, false
}

// fuzzyLookup finds a Hercules entry within edit distance 1 of name.
// Distance 1 catches the vast majority of single-char typos (rocalc's
// "Stilleto"→"Stiletto", "Cutlas"→"Cutlass", "Aniversary"→"Anniversary")
// while keeping false-positive risk low; most legitimately distinct items
// differ by more than one character. The first match wins; in the rare case
// two Hercules names are within distance 1 of the rocalc name, the
// dictionary iteration order makes results unstable, so log a warning when
// it happens to surface for manual review.
func (h *herculesIndex) fuzzyLookup(name string) (id int, ok bool, ambiguous bool) {
	target := strings.ToLower(name)
	// Sort candidate keys before iterating. Go's map iteration is
	// randomized; without sorting, two runs over the same data could
	// produce different `first` choices in edge cases where multiple
	// names within edit distance 1 map to different iRO ids. That would
	// flip mapping.json contents between runs and create phantom diffs.
	keys := make([]string, 0, len(h.byLower))
	for cand := range h.byLower {
		keys = append(keys, cand)
	}
	sort.Strings(keys)
	first := -1
	for _, cand := range keys {
		if clitools.Levenshtein1(target, cand) {
			if first == -1 {
				first = h.byLower[cand]
			} else if h.byLower[cand] != first {
				return first, true, true
			}
		}
	}
	if first == -1 {
		return 0, false, false
	}
	return first, true, false
}

// indexHerculesByName partitions Hercules items by Type. Cards are kept
// separately so the join can use the namespace the suffix-rule expects.
func indexHerculesByName(items []data.Item) (nonCard, card herculesIndex) {
	nonCard = newHerculesIndex()
	card = newHerculesIndex()
	for _, it := range items {
		if it.Name == "" && it.AegisName == "" {
			continue
		}
		idx := &nonCard
		if it.Type == "IT_CARD" {
			idx = &card
		}
		idx.add(it.Name, it.AegisName, it.ID)
	}
	return nonCard, card
}

// isPlaceholder is true for rocalc entries that are not real items but UI
// scaffolding or synthetic aggregates without iRO equivalents:
//   - "(no weapon)" / "(no armor)" empty-slot rows, "(unused)" reserved rows
//   - asterisk-prefixed synthetic card entries (Element Stones, % buff cards)
//   - reserved-slot rows literally named "Unused" or "0"
//   - "X Set" entries where rocalc emulates an in-game multi-item set bonus
//     by carrying the aggregate as a single row (Sprint Set, Black Cat Set)
//   - combo names containing " + " or " & "; rocalc lists multi-piece in-game
//     combos as synthetic single-row entries (e.g. "Captain's Hat + Captain's
//     Pipe", "Blush & Necktie"). Hercules pre-re item names never contain
//     these connectors so the filter is safe.
func isPlaceholder(name string) bool {
	if strings.HasPrefix(name, "(no ") || strings.HasPrefix(name, "*") {
		return true
	}
	if strings.HasPrefix(name, "(unused)") || name == "(unused)" {
		return true
	}
	if strings.HasSuffix(name, " Set") || strings.HasSuffix(name, " Set (old)") {
		return true
	}
	if strings.Contains(name, " + ") || strings.Contains(name, " & ") {
		return true
	}
	switch name {
	case "Unused", "0", "", "No Set":
		return true
	}
	return false
}

// regionalSuffix matches rocalc's "(xRO)" / "(xxRO)" tail used to disambiguate
// items that have regional gameplay differences (iRO/aRO/bRO/jRO/fRO etc.).
// rocalc keeps the regional version as a separate row even when the base
// "X" already exists in its catalog. Per project direction (2026-04-27),
// regional variants are currently folded back to the same iRO id as their
// base; server profiles handle the actual gameplay differences later.
var regionalSuffix = regexp.MustCompile(` \([a-zA-Z]+RO\)$`)

func stripRegionalSuffix(name string) (string, bool) {
	if regionalSuffix.MatchString(name) {
		return regionalSuffix.ReplaceAllString(name, ""), true
	}
	return name, false
}

// nameVariants returns the rocalc-name variants worth trying against
// Hercules. Beyond the literal name we accept:
//   - case-insensitive (handled inside herculesIndex.lookup)
//   - slash-alternative names ("Snake/Boa" → ["Snake", "Boa"], also handles
//     " / " spacing as in "Jing Guai / Li Me Mang Ryang")
//   - trailing-period stripped ("Baphomet Jr." → "Baphomet Jr")
//   - parenthetical-suffix stripped ("Survivor's Rod (dex)" → "Survivor's Rod")
//   - apostrophe stripped ("Bowman's Scarf" → "Bowmans Scarf")
//   - possessive 's stripped ("Bowman's Scarf" → "Bowman Scarf"; matches
//     AegisName "Bowman_Scarf" after the underscore→space normalization in
//     the Hercules index)
func nameVariants(name string) []string {
	out := []string{name}
	// Slash-alternatives: try each side individually.
	for _, sep := range []string{" / ", "/"} {
		if strings.Contains(name, sep) {
			for _, part := range strings.Split(name, sep) {
				p := strings.TrimSpace(part)
				if p != "" {
					out = append(out, p)
				}
			}
		}
	}
	// Trailing period strip: "X." → "X"
	if strings.HasSuffix(name, ".") {
		out = append(out, strings.TrimSuffix(name, "."))
	}
	// Parenthetical-suffix strip: "X (foo)" → "X"
	if i := strings.LastIndex(name, " ("); i > 0 && strings.HasSuffix(name, ")") {
		out = append(out, strings.TrimSpace(name[:i]))
	}
	// Possessive 's strip: "Bowman's Scarf" → "Bowman Scarf". Drop the 's
	// before stripping all apostrophes so we get both forms.
	if strings.Contains(name, "'s ") {
		out = append(out, strings.ReplaceAll(name, "'s ", " "))
	}
	if strings.HasSuffix(name, "'s") {
		out = append(out, strings.TrimSuffix(name, "'s"))
	}
	// Apostrophe-only strip: "Captain's Pipe" → "Captains Pipe".
	if strings.Contains(name, "'") {
		out = append(out, strings.ReplaceAll(name, "'", ""))
	}
	return out
}

// inheritRegional finds rocalc entries whose name carries a regional suffix
// (Leo Crown (aRO), Anubis Hat (iRO), etc.) and copies the iRO id from the
// non-suffixed sibling in rocalc's own catalog if that sibling has been
// matched. This handles the rocalc-internal pattern of listing the same
// item twice; once as the base, once as a regional variant; where the
// variant has no separate iRO id; project decision (2026-04-27) is to fold
// regional variants back to the base, leaving server-specific gameplay
// differences for the server-profile layer.
func inheritRegional(matched []mappingEntry, unmatched []rocalcEntry, all []rocalcEntry) ([]mappingEntry, []rocalcEntry, int) {
	matchedByRocalcID := make(map[int]int, len(matched))
	for _, m := range matched {
		matchedByRocalcID[m.RocalcID] = m.IRoID
	}
	rocalcByName := make(map[string]int, len(all))
	for _, r := range all {
		if _, exists := rocalcByName[r.Name]; !exists {
			rocalcByName[r.Name] = r.ID
		}
	}
	var stillUnmatched []rocalcEntry
	inherited := 0
	for _, u := range unmatched {
		base, isVariant := stripRegionalSuffix(u.Name)
		if !isVariant {
			stillUnmatched = append(stillUnmatched, u)
			continue
		}
		siblingID, hasSibling := rocalcByName[base]
		if !hasSibling {
			stillUnmatched = append(stillUnmatched, u)
			continue
		}
		iroID, siblingMatched := matchedByRocalcID[siblingID]
		if !siblingMatched {
			stillUnmatched = append(stillUnmatched, u)
			continue
		}
		matched = append(matched, mappingEntry{IRoID: iroID, RocalcID: u.ID, Name: u.Name})
		inherited++
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].RocalcID < matched[j].RocalcID })
	return matched, stillUnmatched, inherited
}

// joinByName matches each rocalc entry to an iRO id. Lookup precedence:
//  0. Excluded list (rocalc id intentionally skipped; server-specific or
//     renewal-only entry that future server-profile configs will reintroduce)
//  1. Manual override on rocalc_id (always wins, including over a stale name)
//  2. Each variant from nameVariants tried against Hercules (exact then
//     case-insensitive; see herculesIndex.lookup)
//
// suffix is appended to each variant before lookup; empty for items,
// " Card" for cards (rocalc names cards bare; Hercules names them "X Card").
// Placeholder rocalc entries (see isPlaceholder) are filtered out before
// matching; they don't represent real items and shouldn't pollute the
// unmatched review files. The skipped count is returned for logging.
func joinByName(rocalcEntries []rocalcEntry, herc herculesIndex, ov map[string]int, excluded map[int]bool, suffix string) (matched []mappingEntry, unmatched []rocalcEntry, skipped, exclude int) {
	for _, r := range rocalcEntries {
		if isPlaceholder(r.Name) {
			skipped++
			continue
		}
		if r.Disabled {
			// rocalc-side disabled (dispLoc=999); bucket alongside explicit
			// exclusions since these aren't selectable items in rocalc and
			// have no iRO-id target the orchestrator could ask for.
			exclude++
			continue
		}
		if excluded[r.ID] {
			exclude++
			continue
		}
		if iroID, ok := ov[fmt.Sprintf("%d", r.ID)]; ok {
			matched = append(matched, mappingEntry{IRoID: iroID, RocalcID: r.ID, Name: r.Name})
			continue
		}
		var found bool
		for _, v := range nameVariants(r.Name) {
			if iroID, ok := herc.lookup(v + suffix); ok {
				matched = append(matched, mappingEntry{IRoID: iroID, RocalcID: r.ID, Name: r.Name})
				found = true
				break
			}
		}
		if found {
			continue
		}
		// Fuzzy fallback; edit distance 1 against any variant. Conservative:
		// we don't accept ambiguous fuzzy matches (where two distinct
		// Hercules entries are both within distance 1) since those need
		// human disambiguation.
		for _, v := range nameVariants(r.Name) {
			id, ok, ambiguous := herc.fuzzyLookup(v + suffix)
			if !ok || ambiguous {
				continue
			}
			matched = append(matched, mappingEntry{IRoID: id, RocalcID: r.ID, Name: r.Name})
			found = true
			break
		}
		if !found {
			unmatched = append(unmatched, r)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].RocalcID < matched[j].RocalcID })
	sort.Slice(unmatched, func(i, j int) bool { return unmatched[i].ID < unmatched[j].ID })
	return matched, unmatched, skipped, exclude
}

func loadOverrides(path string) (overrides, error) {
	var ov overrides
	src, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return overrides{Items: map[string]int{}, Cards: map[string]int{}, Mobs: map[string]int{}}, nil
		}
		return ov, err
	}
	if err := json.Unmarshal(src, &ov); err != nil {
		return ov, err
	}
	if ov.Items == nil {
		ov.Items = map[string]int{}
	}
	if ov.Cards == nil {
		ov.Cards = map[string]int{}
	}
	if ov.Mobs == nil {
		ov.Mobs = map[string]int{}
	}
	return ov, nil
}

func writeMapping(path string, m mapping) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	// mapping.json is a build-time artifact regenerated on every run; the
	// previous file is intentionally clobbered. Re-run this generator (e.g.
	// `task mapping`) after upstream rocalc / emulator data changes. No
	// atomic temp+rename: a half-written file would only break the next
	// sidecar startup, and the regenerator can just be re-run.
	return os.WriteFile(path, data, 0o644)
}

func writeUnmatched(path string, entries []rocalcEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, e := range entries {
		fmt.Fprintf(f, "%d\t%s\n", e.ID, e.Name)
	}
	return nil
}
