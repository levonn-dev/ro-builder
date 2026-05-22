package rathena

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"

	"github.com/levonn-dev/ro-builder/internal/data/sources/hercules"
)

// Mob is the same shape hercules.Mob exposes; re-exported here so callers
// who only know the rathena package can use Mob without touching hercules.
// rAthena's mob_db field names diverge from Hercules's (AegisName instead of
// SpriteName, JapaneseName instead of JName, Level instead of Lv) but the
// concepts are identical, so the loader normalizes back to hercules.Mob's
// field names for cross-source comparison.
type Mob = hercules.Mob

type rawMob struct {
	ID           int    `yaml:"Id"`
	AegisName    string `yaml:"AegisName"`
	Name         string `yaml:"Name"`
	JapaneseName string `yaml:"JapaneseName"`
	Level        int    `yaml:"Level"`
	Hp           int    `yaml:"Hp"`
	Attack       int    `yaml:"Attack"`
	Attack2      int    `yaml:"Attack2"`
	Defense      int    `yaml:"Defense"`
	MagicDefense int    `yaml:"MagicDefense"`
	Race         string `yaml:"Race"`
	Element      string `yaml:"Element"`
	ElementLevel int    `yaml:"ElementLevel"`
	Size         string `yaml:"Size"`
}

type rawMobFile struct {
	Header struct {
		Type    string `yaml:"Type"`
		Version int    `yaml:"Version"`
	} `yaml:"Header"`
	Body []rawMob `yaml:"Body"`
}

// LoadMobDB reads rAthena's per-mode mob_db.yml and returns one Mob per row.
// modeDir is "pre-re" or "re", same convention as Hercules.
func LoadMobDB(rathenaRoot, modeDir string) ([]Mob, error) {
	path := filepath.Join(rathenaRoot, "db", modeDir, "mob_db.yml")
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var file rawMobFile
	if err := yaml.Unmarshal(src, &file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := make([]Mob, 0, len(file.Body))
	for _, raw := range file.Body {
		out = append(out, Mob{
			ID:         raw.ID,
			SpriteName: raw.AegisName, // rAthena AegisName ≈ Hercules SpriteName
			Name:       raw.Name,
			JName:      raw.JapaneseName, // rAthena JapaneseName ≈ Hercules JName
			Lv:         raw.Level,
			Hp:         raw.Hp,
			AtkMin:     raw.Attack,
			AtkMax:     raw.Attack2,
			Def:        raw.Defense,
			MDef:       raw.MagicDefense,
			// rAthena emits bare names ("Plant", "Water", "Medium").
			// Normalize to Hercules's prefixed form so downstream consumers
			// (lookup_monster, custom-mob YAML authors) see one shape.
			Race:      normalizeRace(raw.Race),
			Element:   normalizeMobElement(raw.Element),
			ElementLv: raw.ElementLevel,
			Size:      normalizeSize(raw.Size),
		})
	}
	return out, nil
}

func normalizeRace(r string) string {
	if r == "" {
		return ""
	}
	if len(r) >= 3 && r[:3] == "RC_" {
		return r
	}
	return "RC_" + r
}

func normalizeMobElement(e string) string {
	if e == "" {
		return ""
	}
	if len(e) >= 4 && e[:4] == "Ele_" {
		return e
	}
	return "Ele_" + e
}

func normalizeSize(s string) string {
	if s == "" {
		return ""
	}
	if len(s) >= 5 && s[:5] == "Size_" {
		return s
	}
	return "Size_" + s
}
