// Package configs serves the embedded server-profile YAML files. The
// profiles live under configs/servers/*.yaml and are baked into the binary
// at compile time so the API has no runtime dependency on the working
// directory.
//
// Add a new server: drop the YAML under configs/servers/, re-build. The
// loader picks it up automatically via the embed.FS glob.
package configs

import (
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/levonn-dev/ro-builder/internal/domain"
)

//go:embed servers/*.yaml
var serverFiles embed.FS

// LoadServerProfile reads and validates one profile by key (matching the
// file basename without extension, e.g. "uaro" → servers/uaro.yaml).
func LoadServerProfile(key string) (*domain.ServerProfile, error) {
	if key == "" {
		return nil, fmt.Errorf("LoadServerProfile: empty key")
	}
	name := path.Join("servers", key+".yaml")
	raw, err := serverFiles.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("LoadServerProfile %q: %w", key, err)
	}
	var p domain.ServerProfile
	if err := yaml.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("LoadServerProfile %q: parse: %w", key, err)
	}
	if p.Key == "" {
		// Tolerate missing top-level key in YAML by inferring from filename.
		p.Key = key
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("LoadServerProfile %q: %w", key, err)
	}
	// Surface partial quality_gates blocks at startup. Wholesale-replacement
	// semantics (resolveQualityGates) silently zero unspecified thresholds;
	// these warnings make that observable so an author who forgot to copy
	// the canonical shape sees it in the log, not in a passed gate that
	// should have failed. See ServerProfile.QualityGatesWarnings.
	for _, field := range p.QualityGatesWarnings() {
		slog.Warn("server profile: partial quality_gates block disables gate category",
			slog.String("profile", p.Key),
			slog.String("field", field))
	}
	return &p, nil
}

// LoadAllServerProfiles loads every profile under servers/. Returns a map
// keyed by profile.Key. Used by main() to populate the orchestrator's
// lookup table at process start.
func LoadAllServerProfiles() (map[string]*domain.ServerProfile, error) {
	entries, err := fs.ReadDir(serverFiles, "servers")
	if err != nil {
		return nil, fmt.Errorf("LoadAllServerProfiles: %w", err)
	}
	out := make(map[string]*domain.ServerProfile, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		base := e.Name()
		if !strings.HasSuffix(base, ".yaml") {
			continue
		}
		key := strings.TrimSuffix(base, ".yaml")
		p, err := LoadServerProfile(key)
		if err != nil {
			return nil, err
		}
		out[p.Key] = p
	}
	return out, nil
}
