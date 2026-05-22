// enrich-mapping-rms turns the unmatched-items.txt / unmatched-cards.txt /
// unmatched-mobs.txt files produced by cmd/build-rocalc-mapping into a list
// of suggested overrides by querying ratemyserver.net.
//
// Usage:
//
//	go run ./cmd/enrich-mapping-rms
//	go run ./cmd/enrich-mapping-rms --apply
//
// Without --apply, writes cmd/build-rocalc-mapping/rms_suggestions.json
// for review. With --apply, additionally merges high-confidence suggestions
// (single result with AegisName matching the rocalc name verbatim) into
// cmd/build-rocalc-mapping/manual_overrides.json. Re-run build-rocalc-mapping
// after to regenerate calc-sidecar/.../mapping.json.
//
// Each rocalc name is URL-encoded and passed as `iname=...` to rms's item_db
// search. The response HTML carries entries shaped like
//
//	Item ID# 1135 (Cutlas)
//
// where 1135 is the iRO ID and "Cutlas" is the Hercules AegisName. We match
// rocalc names to AegisName because rocalc's display strings sometimes
// align with AegisName rather than Hercules's display Name (rocalc "Cutlas"
// == AegisName "Cutlas" while Hercules display Name is "Cutlus").
//
// Rate-limited at 200 ms between requests. Total walltime ~2 minutes for
// ~500 unmatched rows. The output captures all parsed candidates so a human
// can verify ambiguous cases.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dop251/goja"

	"github.com/levonn-dev/ro-builder/internal/clitools"
)

const (
	unmatchedItemsPath   = "cmd/build-rocalc-mapping/unmatched-items.txt"
	unmatchedCardsPath   = "cmd/build-rocalc-mapping/unmatched-cards.txt"
	unmatchedMobsPath    = "cmd/build-rocalc-mapping/unmatched-mobs.txt"
	suggestionsPath      = "cmd/build-rocalc-mapping/rms_suggestions.json"
	overridesPath        = "cmd/build-rocalc-mapping/manual_overrides.json"
	rocalcItemFile       = "calc-sidecar/vendor/rocalc/item_2026-04-06.js"
	rmsNameURLTemplate   = "https://ratemyserver.net/index.php?iname=%s&page=item_db&quick=1&isearch=Search"
	rmsScriptURLTemplate = "https://ratemyserver.net/index.php?page=item_db&iscript=%s&isearch=Search"
	rmsMobURLTemplate    = "https://ratemyserver.net/index.php?mob_name=%s&page=mob_db&quick=1&f=1&mob_search=Search"
	requestDelay         = 200 * time.Millisecond
	userAgent            = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// httpClient bounds every rms request; without a timeout, one stuck
// connection would hang the ~500-row run indefinitely. 30s is generous
// enough for slow rms responses, tight enough that a dead request gives
// up before the whole batch stalls.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// rocalcPreamble is the minimal stub set the rocalc data files need to
// evaluate cleanly in goja without head.js / DOM. Same set the build-rocalc-
// mapping tool uses.
const rocalcPreamble = `
var PvP = 1;
var SRV = 0;
function skillName(num) { return "[skill " + num + "]"; }
function skillNameInSelect(num) { return "[skill " + num + "]"; }
function Option(label, value) {}
var v_Race = ["<b>Formless</b>","<b>Undead</b>","<b>Brute</b>","<b>Plant</b>","<b>Insect</b>","<b>Fish</b>","<b>Demon</b>","<b>Demi-Human</b>","<b>Angel</b>","<b>Dragon</b>"];
var v_Element = ["<b>Neutral</b>","<b>Water</b>","<b>Earth</b>","<b>Fire</b>","<b>Wind</b>","<b>Poison</b>","<b>Holy</b>","<b>Shadow</b>","<b>Ghost</b>","<b>Undead</b>"];
var v_Size = ["Small","Medium","Large"];
`

// rocalcEffectCode maps a rocalc effect-code to the script-bonus string that
// Hercules / rms uses for the same effect. Codes derived empirically by
// cross-referencing rocalc effects against confirmed-match Hercules scripts;
// see manual_overrides.json's "effect_match_*" notes for provenance.
//
// The function takes the rocalc value and returns the rendered script line
// (no trailing semicolon; joinScript adds those). Returns empty string for
// unknown codes; callers use that as a "skip this bonus" signal.
//
// Important: status-effect codes (151-159 family, also bResEff branch)
// multiply the rocalc value by 100 to match Hercules's per-mille format
// (rocalc 5 = 5% = bResEff,500). Stat-bonus codes pass through directly.
type bonusFn func(value int) string

var rocalcEffectCodes = map[int]bonusFn{
	1:  func(v int) string { return fmt.Sprintf("bonus bStr,%d", v) },
	2:  func(v int) string { return fmt.Sprintf("bonus bAgi,%d", v) },
	3:  func(v int) string { return fmt.Sprintf("bonus bVit,%d", v) },
	4:  func(v int) string { return fmt.Sprintf("bonus bInt,%d", v) },
	5:  func(v int) string { return fmt.Sprintf("bonus bDex,%d", v) },
	6:  func(v int) string { return fmt.Sprintf("bonus bLuk,%d", v) },
	7:  func(v int) string { return fmt.Sprintf("bonus bAllStats,%d", v) },
	8:  func(v int) string { return fmt.Sprintf("bonus bHit,%d", v) },
	9:  func(v int) string { return fmt.Sprintf("bonus bFlee,%d", v) },
	10: func(v int) string { return fmt.Sprintf("bonus bCritical,%d", v) },
	12: func(v int) string { return fmt.Sprintf("bonus bAspdRate,%d", v) },
	13: func(v int) string { return fmt.Sprintf("bonus bMaxHP,%d", v) },
	14: func(v int) string { return fmt.Sprintf("bonus bMaxSP,%d", v) },
	19: func(v int) string { return fmt.Sprintf("bonus bMdef,%d", v) },
	51: func(v int) string { return fmt.Sprintf("bonus2 bAddRaceTolerance,RC_Demon,%d", v) },
	56: func(v int) string { return fmt.Sprintf("bonus2 bAddRaceTolerance,RC_Undead,%d", v) },
	73: func(v int) string { return fmt.Sprintf("bonus bVariableCastrate,%d", v) },
	75: func(v int) string { return fmt.Sprintf("bonus bHPrecovRate,%d", v) },
	76: func(v int) string { return fmt.Sprintf("bonus bSPrecovRate,%d", v) },
	// Status-effect resistance: rocalc value × 100 = script value (5 → 500 = 5% per-mille)
	151: func(v int) string { return fmt.Sprintf("bonus2 bResEff,Eff_Stun,%d", v*100) },
	153: func(v int) string { return fmt.Sprintf("bonus2 bResEff,Eff_Curse,%d", v*100) },
	155: func(v int) string { return fmt.Sprintf("bonus2 bResEff,Eff_Stone,%d", v*100) },
	156: func(v int) string { return fmt.Sprintf("bonus2 bResEff,Eff_Silence,%d", v*100) },
	159: func(v int) string { return fmt.Sprintf("bonus2 bResEff,Eff_Sleep,%d", v*100) },
}

// itemPattern captures rms's "Item ID# N (AegisName)" inline, the most
// reliable structural element across both item and card result rows.
var itemPattern = regexp.MustCompile(`Item ID# (\d+) \(([^)]+)\)`)

// mobPattern captures rms's mob_db result entries shaped like
// ">Display Name &nbsp; (SPRITE_NAME) &nbsp; Mob-ID#1234<". The sprite
// distinguishes variants (PORING vs E_PORING vs GOBLIN_1 vs GOBLIN_2);
// rocalc's "Goblin [Axe]" / "Goblin [Flail]" name pattern can't be
// disambiguated from sprite alone, so bracket variants land in "ambiguous"
// for human review. Standalone names with a single rms hit auto-match.
var mobPattern = regexp.MustCompile(`>([^&<]+) &nbsp; \(([A-Z_][A-Z0-9_]*)\) &nbsp; Mob-ID#(\d+)<`)

type rmsCandidate struct {
	IRoID     int    `json:"iRO_id"`
	AegisName string `json:"aegisName"`      // for mobs this carries the SpriteName (PORING, GOBLIN_1); the eAthena-ident parallel
	Name      string `json:"name,omitempty"` // populated for mob results to support display-name disambiguation
}

type suggestion struct {
	RocalcID   int            `json:"rocalc_id"`
	Name       string         `json:"name"`
	Status     string         `json:"status"` // "auto", "ambiguous", "rms_empty", "rms_no_aegis_match"
	Auto       int            `json:"auto,omitempty"`
	Candidates []rmsCandidate `json:"candidates,omitempty"`
}

type suggestionsFile struct {
	Comment string                  `json:"comment"`
	Items   map[string][]suggestion `json:"items"` // status -> []suggestion
	Cards   map[string][]suggestion `json:"cards"`
	Mobs    map[string][]suggestion `json:"mobs"`
	Counts  map[string]int          `json:"counts"`
}

type unmatchedEntry struct {
	ID      int
	Name    string
	Effects []int // (code, value, code, value, ...) flat pairs from rocalc m_Item
}

func main() {
	apply := flag.Bool("apply", false, "merge high-confidence (status=auto) suggestions into manual_overrides.json")
	flag.Parse()

	if err := run(*apply); err != nil {
		slog.Default().Error("enrich-mapping-rms failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(apply bool) error {
	repoRoot, err := clitools.FindRepoRoot()
	if err != nil {
		return err
	}

	items, err := readUnmatched(filepath.Join(repoRoot, unmatchedItemsPath))
	if err != nil {
		return fmt.Errorf("read items: %w", err)
	}
	cards, err := readUnmatched(filepath.Join(repoRoot, unmatchedCardsPath))
	if err != nil {
		return fmt.Errorf("read cards: %w", err)
	}
	// Mobs file is optional; older trees of build-rocalc-mapping didn't
	// emit it. Treat missing as empty rather than failing the run.
	mobs, err := readUnmatched(filepath.Join(repoRoot, unmatchedMobsPath))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read mobs: %w", err)
	}

	// Pull effects per rocalc m_Item id so iscript searches can use them.
	// Unmatched cards are an empty pipe today (every card classified); we
	// only need item effects. Mobs have no scripts so no effect lookup.
	effects, err := loadRocalcItemEffects(filepath.Join(repoRoot, rocalcItemFile))
	if err != nil {
		return fmt.Errorf("load rocalc effects: %w", err)
	}
	for i := range items {
		items[i].Effects = effects[items[i].ID]
	}
	slog.Default().Info("loaded unmatched entries",
		slog.Int("items", len(items)),
		slog.Int("cards", len(cards)),
		slog.Int("mobs", len(mobs)),
	)

	itemSugg := classifyAll(items, "")
	cardSugg := classifyAll(cards, "_Card")
	mobSugg := classifyAllMobs(mobs)

	out := suggestionsFile{
		Comment: "Suggestions from ratemyserver.net for entries unmatched by cmd/build-rocalc-mapping. status=auto: exact AegisName / SpriteName match; highest confidence. status=auto_fuzzy: identifier within edit distance 1 of expected (catches single-letter typos like 'Dragon' vs 'Dragoon'). status=ambiguous / ambiguous_fuzzy: multiple candidates at the same confidence tier (typical for bracket-variant mobs like 'Goblin [Axe]' where rms returns all sprite-distinguished Goblins). status=rms_empty: rms had no results. status=rms_no_aegis_match: rms returned candidates but none resemble the expected name.",
		Items:   groupByStatus(itemSugg),
		Cards:   groupByStatus(cardSugg),
		Mobs:    groupByStatus(mobSugg),
		Counts: map[string]int{
			"items_total":            len(items),
			"items_auto":             countStatus(itemSugg, "auto"),
			"items_auto_fuzzy":       countStatus(itemSugg, "auto_fuzzy"),
			"items_auto_script":      countStatus(itemSugg, "auto_script"),
			"items_ambiguous":        countStatus(itemSugg, "ambiguous"),
			"items_ambiguous_fuzzy":  countStatus(itemSugg, "ambiguous_fuzzy"),
			"items_ambiguous_script": countStatus(itemSugg, "ambiguous_script"),
			"items_empty":            countStatus(itemSugg, "rms_empty"),
			"items_no_aegis":         countStatus(itemSugg, "rms_no_aegis_match"),
			"cards_total":            len(cards),
			"cards_auto":             countStatus(cardSugg, "auto"),
			"cards_auto_fuzzy":       countStatus(cardSugg, "auto_fuzzy"),
			"cards_auto_script":      countStatus(cardSugg, "auto_script"),
			"cards_ambiguous":        countStatus(cardSugg, "ambiguous"),
			"cards_ambiguous_fuzzy":  countStatus(cardSugg, "ambiguous_fuzzy"),
			"cards_ambiguous_script": countStatus(cardSugg, "ambiguous_script"),
			"cards_empty":            countStatus(cardSugg, "rms_empty"),
			"cards_no_aegis":         countStatus(cardSugg, "rms_no_aegis_match"),
			"mobs_total":             len(mobs),
			"mobs_auto":              countStatus(mobSugg, "auto"),
			"mobs_auto_fuzzy":        countStatus(mobSugg, "auto_fuzzy"),
			"mobs_ambiguous":         countStatus(mobSugg, "ambiguous"),
			"mobs_ambiguous_fuzzy":   countStatus(mobSugg, "ambiguous_fuzzy"),
			"mobs_ambiguous_bracket": countStatus(mobSugg, "ambiguous_bracket"),
			"mobs_empty":             countStatus(mobSugg, "rms_empty"),
			"mobs_no_aegis":          countStatus(mobSugg, "rms_no_aegis_match"),
		},
	}

	if err := writeJSON(filepath.Join(repoRoot, suggestionsPath), out); err != nil {
		return fmt.Errorf("write suggestions: %w", err)
	}
	slog.Default().Info("items classified",
		slog.Int("auto", out.Counts["items_auto"]),
		slog.Int("auto_fuzzy", out.Counts["items_auto_fuzzy"]),
		slog.Int("auto_script", out.Counts["items_auto_script"]),
		slog.Int("ambiguous", out.Counts["items_ambiguous"]),
		slog.Int("ambiguous_fuzzy", out.Counts["items_ambiguous_fuzzy"]),
		slog.Int("ambiguous_script", out.Counts["items_ambiguous_script"]),
		slog.Int("rms_empty", out.Counts["items_empty"]),
		slog.Int("rms_no_aegis", out.Counts["items_no_aegis"]),
	)
	slog.Default().Info("cards classified",
		slog.Int("auto", out.Counts["cards_auto"]),
		slog.Int("auto_fuzzy", out.Counts["cards_auto_fuzzy"]),
		slog.Int("auto_script", out.Counts["cards_auto_script"]),
		slog.Int("ambiguous", out.Counts["cards_ambiguous"]),
		slog.Int("ambiguous_fuzzy", out.Counts["cards_ambiguous_fuzzy"]),
		slog.Int("ambiguous_script", out.Counts["cards_ambiguous_script"]),
		slog.Int("rms_empty", out.Counts["cards_empty"]),
		slog.Int("rms_no_aegis", out.Counts["cards_no_aegis"]),
	)
	slog.Default().Info("mobs classified",
		slog.Int("auto", out.Counts["mobs_auto"]),
		slog.Int("auto_fuzzy", out.Counts["mobs_auto_fuzzy"]),
		slog.Int("ambiguous_exact", out.Counts["mobs_ambiguous"]),
		slog.Int("ambiguous_fuzzy", out.Counts["mobs_ambiguous_fuzzy"]),
		slog.Int("ambiguous_bracket", out.Counts["mobs_ambiguous_bracket"]),
		slog.Int("rms_empty", out.Counts["mobs_empty"]),
		slog.Int("rms_no_aegis", out.Counts["mobs_no_aegis"]),
	)
	slog.Default().Info("wrote suggestions", slog.String("path", suggestionsPath))

	if apply {
		merged, err := applyAuto(filepath.Join(repoRoot, overridesPath), itemSugg, cardSugg, mobSugg)
		if err != nil {
			return fmt.Errorf("apply: %w", err)
		}
		slog.Default().Info("merged into manual_overrides.json",
			slog.Int("new_items", merged.itemsAdded),
			slog.Int("new_cards", merged.cardsAdded),
			slog.Int("new_mobs", merged.mobsAdded),
		)
		slog.Default().Info("re-run `go run ./cmd/build-rocalc-mapping` to regenerate mapping.json")
	}
	return nil
}

// classifyAllMobs queries rms's mob_db for each unmatched rocalc mob and
// classifies the result. Status semantics mirror classifyAll's:
//
//   - auto:     single rms hit whose SpriteName aligns with the rocalc-name
//     normalization rule (spaces→underscores, uppercased); the
//     highest-confidence tier, suitable for unattended apply.
//   - auto_fuzzy: same shape but within edit distance 1 (single-char typos).
//   - ambiguous: multiple plausible candidates; typical for bracket
//     variants ("Goblin [Axe]" returns 5+ Goblin sprites). Surfaced
//     for human review with the full candidate list.
//   - rms_empty / rms_no_aegis_match: nothing to work with.
//
// Bracket-stripping note: rocalc's "Goblin [Axe]" form uses square-brackets
// to encode an attribute that doesn't appear in rms's data at all (rms uses
// SpriteName suffixes like GOBLIN_1 instead). We strip the bracket portion
// before querying so rms returns the variant set; mapping the bracket to
// the right sprite is left as manual-overrides work.
func classifyAllMobs(entries []unmatchedEntry) []suggestion {
	out := make([]suggestion, 0, len(entries))
	for i, e := range entries {
		s := classifyMobOne(e)
		out = append(out, s)
		if (i+1)%50 == 0 {
			slog.Default().Info("progress", slog.String("type", "mobs"), slog.Int("processed", i+1), slog.Int("total", len(entries)))
		}
		time.Sleep(requestDelay)
	}
	return out
}

// classifyMobOne queries rms for one rocalc mob and bins it by confidence:
//
//   - Strip the bracket suffix (rocalc's variant marker) before searching;
//     rms's mob_db doesn't use that convention.
//   - rms returns rows like ">Goblin &nbsp; (GOBLIN_1) &nbsp; Mob-ID#1122<".
//     Filter to rows whose display name exactly matches our bracket-stripped
//     rocalc name (case-insensitive); drops noise like "Christmas Goblin"
//     that rms substring-matches into the result set.
//   - One survivor → auto. Many → ambiguous. Try fuzzy-by-display next.
//
// Bracket-name caveat: rocalc's "X [Y]" is sometimes a sprite-variant
// disambiguator (Goblin [Axe] vs Goblin [Flail], distinct iRO ids) and
// sometimes just a parenthetical (Eremes Guile [Lv99] is the same MVP as
// the bare Eremes Guile, just at MVP level). We can't tell from the name
// alone, and a name-only single-hit auto-mapping has been observed to
// produce wrong matches (Cookie [Red] / [Green] mapped to basic Cookie 1265
// despite stat divergence). For safety, bracket-named rocalc entries never
// auto; they always land in `ambiguous_bracket` for stat-based or human
// review by build-rocalc-mapping's stat-disambiguation pass.
func classifyMobOne(e unmatchedEntry) suggestion {
	queryName := stripBracketSuffix(e.Name)
	hasBracket := queryName != e.Name
	candidates, err := queryRMSMobs(queryName)
	if err != nil {
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: "rms_error: " + err.Error()}
	}
	if len(candidates) == 0 {
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: "rms_empty"}
	}
	queryLower := strings.ToLower(queryName)

	var displayExact []rmsCandidate
	for _, c := range candidates {
		if strings.EqualFold(strings.TrimSpace(c.Name), queryLower) {
			displayExact = append(displayExact, c)
		}
	}
	if len(displayExact) == 1 && !hasBracket {
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: "auto", Auto: displayExact[0].IRoID, Candidates: displayExact}
	}
	if len(displayExact) >= 1 && hasBracket {
		// Bracket-named: even a single name-match isn't trustworthy without
		// stat verification (see Cookie [Red] case in commit history).
		// build-rocalc-mapping's stat-disambiguation pass handles these.
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: "ambiguous_bracket", Candidates: displayExact}
	}
	if len(displayExact) > 1 {
		// Multiple non-bracket candidates; genuinely ambiguous.
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: "ambiguous", Candidates: displayExact}
	}

	var displayFuzzy []rmsCandidate
	for _, c := range candidates {
		if clitools.Levenshtein1(strings.ToLower(strings.TrimSpace(c.Name)), queryLower) {
			displayFuzzy = append(displayFuzzy, c)
		}
	}
	if len(displayFuzzy) == 1 && !hasBracket {
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: "auto_fuzzy", Auto: displayFuzzy[0].IRoID, Candidates: displayFuzzy}
	}
	if len(displayFuzzy) >= 1 && hasBracket {
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: "ambiguous_bracket", Candidates: displayFuzzy}
	}
	if len(displayFuzzy) > 1 {
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: "ambiguous_fuzzy", Candidates: displayFuzzy}
	}
	return suggestion{RocalcID: e.ID, Name: e.Name, Status: "rms_no_aegis_match", Candidates: candidates}
}

// stripBracketSuffix removes a trailing " [X]" attribute marker; rocalc's
// way of disambiguating sprite variants (Goblin [Axe], Venatu [Water]).
// rms's mob_db doesn't use that convention so the bracket would foil the
// substring search; strip before querying and rely on candidate inspection
// to disambiguate.
func stripBracketSuffix(name string) string {
	if i := strings.LastIndex(name, " ["); i > 0 && strings.HasSuffix(name, "]") {
		return name[:i]
	}
	return name
}

// queryRMSMobs hits rms's mob_db with `mob_name=...` and parses the result
// rows. The shared rmsCandidate type's AegisName field carries the
// SpriteName ("PORING" / "GOBLIN_1") since they're parallel concepts; the
// mob's display Name lands in c.Name for the substring-based filter.
func queryRMSMobs(name string) ([]rmsCandidate, error) {
	encoded := url.QueryEscape(name)
	target := fmt.Sprintf(rmsMobURLTemplate, encoded)
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rms %d for mob %q", resp.StatusCode, name)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	matches := mobPattern.FindAllStringSubmatch(string(body), -1)
	seen := make(map[int]bool, len(matches))
	var out []rmsCandidate
	for _, m := range matches {
		id, err := strconv.Atoi(m[3])
		if err != nil {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, rmsCandidate{
			IRoID:     id,
			Name:      strings.TrimSpace(m[1]),
			AegisName: m[2], // SpriteName
		})
	}
	return out, nil
}

func classifyAll(entries []unmatchedEntry, suffix string) []suggestion {
	out := make([]suggestion, 0, len(entries))
	for i, e := range entries {
		s := classifyOne(e, suffix)
		out = append(out, s)
		if (i+1)%50 == 0 {
			slog.Default().Info("progress", slog.Int("processed", i+1), slog.Int("total", len(entries)))
		}
		time.Sleep(requestDelay)
	}
	return out
}

// classifyOne queries rms for a single entry and decides the suggestion bucket.
//
// Two query strategies are tried, in order of preference:
//
//  1. iname (display-name substring); fast and works when names align.
//     Confidence tiers within name results: auto (exact AegisName match),
//     auto_fuzzy (AegisName within edit distance 1).
//
//  2. iscript (script-content substring); fallback when iname doesn't yield
//     a confident hit. Builds the expected Hercules script from the rocalc
//     effect pairs (using rocalcEffectCodes). A single rms result matching
//     by script is "auto_script"; high confidence, since identical script
//     content is a tight semantic match. Multiple results → "ambiguous_script".
//     Skipped when no rocalc effect codes are decodable.
//
// The expected AegisName for tier 1 is rocalc's name with spaces→underscores,
// plus suffix (`""` for items, `"_Card"` for cards).
func classifyOne(e unmatchedEntry, suffix string) suggestion {
	expectedAegis := strings.ReplaceAll(e.Name, " ", "_") + suffix
	candidates, err := queryRMS(e.Name)
	if err != nil {
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: "rms_error: " + err.Error()}
	}
	if len(candidates) == 0 {
		// iname returned nothing; try iscript fallback before giving up.
		return classifyByScript(e, "rms_empty")
	}

	expectedLower := strings.ToLower(expectedAegis)

	var exactMatches, fuzzyMatches []rmsCandidate
	for _, c := range candidates {
		candLower := strings.ToLower(c.AegisName)
		if candLower == expectedLower {
			exactMatches = append(exactMatches, c)
			continue
		}
		if clitools.Levenshtein1(expectedLower, candLower) {
			fuzzyMatches = append(fuzzyMatches, c)
		}
	}
	if len(exactMatches) == 1 {
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: "auto", Auto: exactMatches[0].IRoID, Candidates: exactMatches}
	}
	if len(exactMatches) > 1 {
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: "ambiguous", Candidates: exactMatches}
	}
	if len(fuzzyMatches) == 1 {
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: "auto_fuzzy", Auto: fuzzyMatches[0].IRoID, Candidates: fuzzyMatches}
	}
	if len(fuzzyMatches) > 1 {
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: "ambiguous_fuzzy", Candidates: fuzzyMatches}
	}
	// iname returned candidates but none match by AegisName; try iscript
	// fallback, attaching the rms_no_aegis candidates as supplemental info
	// if iscript also fails.
	scriptResult := classifyByScript(e, "rms_no_aegis_match")
	if scriptResult.Status == "rms_no_aegis_match" || scriptResult.Status == "rms_empty" {
		// No script hit either; surface the iname candidates we already had.
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: "rms_no_aegis_match", Candidates: candidates}
	}
	return scriptResult
}

// classifyByScript performs the iscript fallback. Returns a suggestion whose
// Status is auto_script / ambiguous_script when rms returns hits, or
// fallbackStatus (passed in) when there's nothing to work with; keeps the
// caller's existing "iname empty" / "iname returned non-aegis matches"
// semantics intact when the fallback also yields nothing.
func classifyByScript(e unmatchedEntry, fallbackStatus string) suggestion {
	script, decoded := effectsToScript(e.Effects)
	if decoded == 0 {
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: fallbackStatus}
	}
	time.Sleep(requestDelay)
	candidates, err := queryRMSScript(script)
	if err != nil {
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: "rms_script_error: " + err.Error()}
	}
	if len(candidates) == 0 {
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: fallbackStatus}
	}
	if len(candidates) == 1 {
		return suggestion{RocalcID: e.ID, Name: e.Name, Status: "auto_script", Auto: candidates[0].IRoID, Candidates: candidates}
	}
	return suggestion{RocalcID: e.ID, Name: e.Name, Status: "ambiguous_script", Candidates: candidates}
}

// loadRocalcItemEffects evaluates rocalc's item_2026-*.js in goja and returns
// the effect pairs for every m_Item row, keyed by id. Effect pairs live at
// indices 11..N-1 of each row (terminator 0 ignored), as flat (code, value,
// code, value, ...) tuples. Cards aren't included; the unmatched-cards
// list has been empty since the rocalc-disabled detection landed.
func loadRocalcItemEffects(path string) (map[int][]int, error) {
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
	val := vm.Get("m_Item")
	if val == nil || goja.IsUndefined(val) {
		return nil, fmt.Errorf("m_Item not defined")
	}
	rows, ok := val.Export().([]any)
	if !ok {
		return nil, fmt.Errorf("m_Item is not an array")
	}
	out := make(map[int][]int, len(rows))
	for _, r := range rows {
		row, ok := r.([]any)
		if !ok || len(row) < 12 {
			continue
		}
		id, ok := row[0].(int64)
		if !ok {
			continue
		}
		var eff []int
		for i := 11; i < len(row)-1; i++ {
			if v, ok := row[i].(int64); ok {
				eff = append(eff, int(v))
			}
		}
		if len(eff) > 0 {
			out[int(id)] = eff
		}
	}
	return out, nil
}

// effectsToScript renders rocalc effect-pair tuples to a Hercules-style
// script string suitable for an rms iscript search. Unknown codes are
// silently dropped; the resulting partial script still narrows the search
// space; the caller should consider partial matches lower-confidence.
//
// Returns the joined script and the count of bonuses successfully decoded.
// Empty string + 0 means we have no usable bonuses to search by.
func effectsToScript(effects []int) (string, int) {
	var parts []string
	decoded := 0
	for i := 0; i+1 < len(effects); i += 2 {
		code, value := effects[i], effects[i+1]
		fn, ok := rocalcEffectCodes[code]
		if !ok {
			continue
		}
		parts = append(parts, fn(value))
		decoded++
	}
	if len(parts) == 0 {
		return "", 0
	}
	// rms iscript search is a substring match on the script field. Joining
	// with "; " produces the exact text Hercules emits (e.g. "bonus bStr,2;
	// bonus bAgi,1") so a single-pass substring hit means the candidate has
	// these bonuses in this order.
	return strings.Join(parts, "; ") + ";", decoded
}

// queryRMSScript searches rms by iscript (script substring) instead of name.
// Result parsing is identical to queryRMS; both endpoints render the same
// "Item ID# N (AegisName)" structure for every match row.
func queryRMSScript(script string) ([]rmsCandidate, error) {
	encoded := url.QueryEscape(script)
	target := fmt.Sprintf(rmsScriptURLTemplate, encoded)
	return fetchAndParse(target, fmt.Sprintf("script %q", script))
}

func queryRMS(name string) ([]rmsCandidate, error) {
	encoded := url.QueryEscape(name)
	target := fmt.Sprintf(rmsNameURLTemplate, encoded)
	return fetchAndParse(target, fmt.Sprintf("name %q", name))
}

func fetchAndParse(target, label string) ([]rmsCandidate, error) {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rms %d for %s", resp.StatusCode, label)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	matches := itemPattern.FindAllStringSubmatch(string(body), -1)
	seen := make(map[int]bool, len(matches))
	var out []rmsCandidate
	for _, m := range matches {
		id, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, rmsCandidate{IRoID: id, AegisName: m[2]})
	}
	return out, nil
}

func groupByStatus(suggestions []suggestion) map[string][]suggestion {
	out := make(map[string][]suggestion)
	for _, s := range suggestions {
		out[s.Status] = append(out[s.Status], s)
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool { return out[k][i].RocalcID < out[k][j].RocalcID })
	}
	return out
}

func countStatus(suggestions []suggestion, status string) int {
	n := 0
	for _, s := range suggestions {
		if s.Status == status {
			n++
		}
	}
	return n
}

type overrides struct {
	Comment       string                 `json:"comment,omitempty"`
	Notes         map[string]string      `json:"notes,omitempty"`
	Items         map[string]json.Number `json:"items"`
	Cards         map[string]json.Number `json:"cards"`
	Mobs          map[string]json.Number `json:"mobs,omitempty"`
	ExcludedItems []int                  `json:"excluded_items,omitempty"`
	ExcludedCards []int                  `json:"excluded_cards,omitempty"`
	ExcludedMobs  []int                  `json:"excluded_mobs,omitempty"`
}

type mergedCounts struct {
	itemsAdded int
	cardsAdded int
	mobsAdded  int
}

// applyAuto reads manual_overrides.json, merges in suggestions tagged "auto",
// and writes the file back. Pre-existing override entries are preserved
// unchanged; never overwrite a hand-curated decision.
func applyAuto(path string, itemSugg, cardSugg, mobSugg []suggestion) (mergedCounts, error) {
	var merged mergedCounts

	src, err := os.ReadFile(path)
	if err != nil {
		return merged, err
	}
	var ov overrides
	if err := json.Unmarshal(src, &ov); err != nil {
		return merged, err
	}
	if ov.Items == nil {
		ov.Items = make(map[string]json.Number)
	}
	if ov.Cards == nil {
		ov.Cards = make(map[string]json.Number)
	}
	if ov.Mobs == nil {
		ov.Mobs = make(map[string]json.Number)
	}

	autoStatus := func(s string) bool {
		return s == "auto" || s == "auto_fuzzy" || s == "auto_script"
	}
	mergeInto := func(target map[string]json.Number, sugg []suggestion, counter *int) {
		for _, s := range sugg {
			if !autoStatus(s.Status) {
				continue
			}
			key := strconv.Itoa(s.RocalcID)
			if _, exists := target[key]; exists {
				continue
			}
			target[key] = json.Number(strconv.Itoa(s.Auto))
			*counter++
		}
	}
	mergeInto(ov.Items, itemSugg, &merged.itemsAdded)
	mergeInto(ov.Cards, cardSugg, &merged.cardsAdded)
	mergeInto(ov.Mobs, mobSugg, &merged.mobsAdded)

	if ov.Notes == nil {
		ov.Notes = map[string]string{}
	}
	stamp := time.Now().Format("2006-01-02")
	ov.Notes["enrich-mapping-rms_"+stamp] = fmt.Sprintf(
		"Auto-merged %d item + %d card + %d mob suggestions from rms (single-candidate exact-name matches).",
		merged.itemsAdded, merged.cardsAdded, merged.mobsAdded,
	)

	if err := writeJSON(path, ov); err != nil {
		return merged, err
	}
	return merged, nil
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func readUnmatched(path string) ([]unmatchedEntry, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []unmatchedEntry
	for _, line := range strings.Split(string(src), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		id, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		out = append(out, unmatchedEntry{ID: id, Name: parts[1]})
	}
	return out, nil
}
