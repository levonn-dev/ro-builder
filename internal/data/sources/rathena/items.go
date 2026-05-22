// Package rathena parses rAthena's YAML-format item database.
//
// rAthena is a sibling RO-server emulator to Hercules; both descend from
// eAthena, but rAthena is more actively maintained. Its item database lives
// under db/<mode>/ and is split across multiple YAML files (item_db_equip,
// item_db_etc, item_db_usable) instead of Hercules's single libconfig file.
// Field names also diverge in places: rAthena uses Attack/Defense/Locations/
// EquipLevelMin where Hercules uses Atk/Def/Loc/EquipLv.
//
// This package returns the same Item struct hercules.LoadItemDB does so
// downstream consumers (the orchestrator, the build-time mapping
// cross-check tool) can treat the two sources interchangeably. We normalize
// Type to Hercules's "IT_X" format so cross-source comparison is direct.
package rathena

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	// yaml.v2 over v3 deliberately: rAthena's wild YAML occasionally has
	// duplicate keys (e.g. renewal Sinulog Hat 400276 lists `Trade:` twice).
	// v3 raises a hard error on those; v2 silently keeps the last value,
	// matching how rAthena's own loaders behave. We're parsing real-world
	// YAML, not authoring spec-pure documents.
	"gopkg.in/yaml.v2"

	"github.com/levonn-dev/ro-builder/internal/data/sources/hercules"
)

// Item is the same shape hercules.Item exposes; re-exported here so callers
// who only know the rathena package can use Item without touching hercules.
type Item = hercules.Item

type rawItem struct {
	ID            int             `yaml:"Id"`
	AegisName     string          `yaml:"AegisName"`
	Name          string          `yaml:"Name"`
	Type          string          `yaml:"Type"`
	SubType       string          `yaml:"SubType"`
	Buy           int             `yaml:"Buy"`
	Sell          int             `yaml:"Sell"`
	Weight        int             `yaml:"Weight"`
	Attack        int             `yaml:"Attack"`
	Defense       int             `yaml:"Defense"`
	Range         int             `yaml:"Range"`
	Slots         int             `yaml:"Slots"`
	WeaponLevel   int             `yaml:"WeaponLevel"`
	EquipLevelMin int             `yaml:"EquipLevelMin"`
	Locations     map[string]bool `yaml:"Locations"`
}

type rawFile struct {
	Header struct {
		Type    string `yaml:"Type"`
		Version int    `yaml:"Version"`
	} `yaml:"Header"`
	Body []rawItem `yaml:"Body"`
}

// rAthena splits the item DB across three YAML files per mode dir: equip
// (weapons + armor), etc (cards, ammo, dyes), and usable (potions, foods,
// scrolls). LoadItemDB merges them into one slice.
var dbFiles = []string{"item_db_equip.yml", "item_db_etc.yml", "item_db_usable.yml"}

// LoadItemDB reads rAthena's per-mode item-database YAMLs from rathenaRoot
// and returns one Item per row. modeDir is rAthena's directory name;
// "pre-re" or "re", same convention as Hercules.
func LoadItemDB(rathenaRoot, modeDir string) ([]Item, error) {
	var items []Item
	for _, f := range dbFiles {
		path := filepath.Join(rathenaRoot, "db", modeDir, f)
		part, err := loadOne(path)
		if err != nil {
			return nil, err
		}
		items = append(items, part...)
	}
	return items, nil
}

func loadOne(path string) ([]Item, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var file rawFile
	if err := yaml.Unmarshal(src, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := make([]Item, 0, len(file.Body))
	for _, raw := range file.Body {
		out = append(out, Item{
			ID:        raw.ID,
			AegisName: raw.AegisName,
			Name:      raw.Name,
			Type:      normalizeType(raw.Type),
			Atk:       raw.Attack,
			Def:       raw.Defense,
			Slots:     raw.Slots,
			Weight:    raw.Weight,
			Range:     raw.Range,
			Loc:       firstLoc(raw.Locations),
			WeaponLv:  raw.WeaponLevel,
			EquipLv:   raw.EquipLevelMin,
			Subtype:   normalizeSubtype(raw.Type, raw.SubType),
		})
	}
	return out, nil
}

// normalizeType converts rAthena's bare "Weapon" / "Armor" / "Card" / "Etc"
// type tags into Hercules's "IT_WEAPON" / "IT_ARMOR" / etc. so cross-source
// comparison works without a translation table on the consumer side.
//
// Tradeoff; IT_HEALING vs IT_USABLE: Hercules splits consumables into
// IT_HEALING (potions, foods) and IT_USABLE (scrolls, town-return, ad-hoc
// usables). rAthena's pre-re YAML mostly tags the lot as "Usable", with a
// "Healing" subset surfacing in renewal. We collapse rAthena's Usable into
// IT_HEALING here so downstream type filters (e.g. SearchItems for healing
// pots) return non-empty results when the catalog is rAthena-sourced. The
// cost is that rAthena-sourced IT_HEALING includes some non-healing usables
// that would be IT_USABLE under Hercules. If precise type distinctions
// matter to the caller (e.g. building a strict potion-only list), prefer
// Hercules-source loading via the data.Source switch in cmd/build-catalog.
//
// DelayConsume: rAthena surfaces delay-consume items as their own Type
// (both "DelayConsume" and "Delayconsume" casings appear in the real db),
// which maps cleanly to Hercules's IT_DELAYCONSUME; no conflation needed.
func normalizeType(t string) string {
	if t == "" {
		return "IT_ETC"
	}
	switch strings.ToLower(t) {
	case "usable":
		// Hercules's potions/foods/scrolls all carry IT_HEALING; rAthena
		// uses the broader "Usable" bucket. Map to the Hercules canonical
		// so type filters match across sources.
		return "IT_HEALING"
	case "delayconsume":
		// rAthena's delayed-consume items (some scrolls). Hercules uses
		// IT_DELAYCONSUME for the same.
		return "IT_DELAYCONSUME"
	}
	return "IT_" + strings.ToUpper(t)
}

// normalizeSubtype maps rAthena's SubType strings to Hercules's W_X / A_X
// format for weapons and shields. rAthena: "Dagger" → Hercules: "W_DAGGER";
// rAthena armor SubType isn't used, return empty.
func normalizeSubtype(itemType, sub string) string {
	if sub == "" {
		return ""
	}
	t := strings.ToLower(itemType)
	if t != "weapon" && t != "armor" {
		return ""
	}
	prefix := "W_"
	if t == "armor" {
		prefix = "A_"
	}
	// rAthena uses "1hSword", "2hSword"; Hercules uses "W_1HSWORD".
	upper := strings.ToUpper(sub)
	upper = strings.ReplaceAll(upper, " ", "_")
	return prefix + upper
}

// firstLoc returns the first true location key from rAthena's Locations map
// in canonical Hercules form. rAthena uses keys like "Right_Hand", "Armor",
// "Head_Top"; Hercules emits "EQP_HAND_R", "EQP_ARMOR", "EQP_HEAD_TOP".
// Return value is one of those EQP_ strings, or "" if no location is set.
//
// rAthena permits multi-slot equipment (Goggles → Head_Top + Head_Mid) by
// listing multiple keys; Hercules represents the same via a Loc array
// which our hercules.Item parser also collapses to "" for those rows. So
// returning "first" is consistent with hercules's current behavior.
//
// Iteration order is a fixed priority list rather than Go's randomized
// map range so the resulting catalog is byte-identical across loads. The
// order roughly tracks an equipment paper-doll top-to-bottom (head → body
// → weapons → garment → boots → accessories), which is also the order
// downstream filters tend to prefer when multiple slots are set.
func firstLoc(locs map[string]bool) string {
	for _, k := range locPriority {
		if locs[k] {
			return rathenaLocToHercules(k)
		}
	}
	return ""
}

// locPriority defines the deterministic iteration order for firstLoc.
// Keys are rAthena's literal Locations keys (case-sensitive; rAthena's
// YAML loader is case-sensitive too). Both_Hand sits with weapon; the
// accessory split (Left_Accessory / Right_Accessory) covers the
// non-Both_Accessory cases. Ammo is appended last as a catch-all for
// throwable / projectile items.
var locPriority = []string{
	"Head_Top",
	"Head_Mid",
	"Head_Low",
	"Armor",
	"Right_Hand",
	"Both_Hand",
	"Left_Hand",
	"Garment",
	"Shoes",
	"Left_Accessory",
	"Right_Accessory",
	"Both_Accessory",
	"Ammo",
}

func rathenaLocToHercules(k string) string {
	switch strings.ToLower(k) {
	case "right_hand":
		return "EQP_HAND_R"
	case "left_hand":
		return "EQP_HAND_L"
	case "armor":
		return "EQP_ARMOR"
	case "garment":
		return "EQP_GARMENT"
	case "shoes":
		return "EQP_SHOES"
	case "accessory_l", "accessory_r":
		return "EQP_ACC"
	case "head_top":
		return "EQP_HEAD_TOP"
	case "head_mid":
		return "EQP_HEAD_MID"
	case "head_low":
		return "EQP_HEAD_LOW"
	case "ammo":
		return "EQP_AMMO"
	}
	return "EQP_" + strings.ToUpper(k)
}
