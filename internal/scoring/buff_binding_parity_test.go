package scoring

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Every self_buff name in skill_buffs.yaml must have a BUFF_BINDINGS entry in
// the rocalc backend, else a declared buff fails only at score time. This guard
// fails the build instead. Text-level check (no JS execution): asserts each
// "name:" appears as a binding key in index.ts.
func TestBuffNamesHaveSidecarBindings(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	yamlPath := filepath.Join(root, "internal/catalog/data/skill_buffs.yaml")
	idxPath := filepath.Join(root, "calc-sidecar/src/backends/rocalc/index.ts")

	raw, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("read overlay: %v", err)
	}
	var f struct {
		Skills []struct {
			SelfBuff struct {
				Name string `yaml:"name"`
			} `yaml:"self_buff"`
		} `yaml:"skills"`
	}
	if err := yaml.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse overlay: %v", err)
	}
	idx, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatalf("read index.ts: %v", err)
	}
	src := string(idx)
	for _, s := range f.Skills {
		name := s.SelfBuff.Name
		if name == "" {
			continue
		}
		if !strings.Contains(src, name+":") {
			t.Errorf("buff %q has no BUFF_BINDINGS entry in index.ts", name)
		}
	}
}
