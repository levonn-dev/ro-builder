package llm

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Factory builds a Provider from a Config. Provider packages register
// their factories via Register at init() time; cmd/api side-effect-imports
// the packages it wants available, then calls New(config) to construct.
type Factory func(Config) (Provider, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register adds a Factory under the given name. Names are normalized to
// lowercase so callers can pass "Anthropic" / "anthropic" interchangeably.
// Panics on duplicate registration; same posture as tools.Registry, so
// wiring bugs surface at process start rather than at first call.
func Register(name string, f Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		panic("llm.Register: empty provider name")
	}
	if _, exists := registry[key]; exists {
		panic(fmt.Sprintf("llm.Register: duplicate provider %q", name))
	}
	registry[key] = f
}

// New looks up the configured provider's factory and constructs a
// Provider. Returns an error listing the registered providers when the
// name doesn't match anything; the operator gets actionable feedback at
// process start rather than a silent miss at first request.
//
// An empty Config.Provider defaults to "anthropic" so installations with
// only the default backend don't have to set LLM_PROVIDER explicitly.
func New(c Config) (Provider, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	name := strings.ToLower(strings.TrimSpace(c.Provider))
	if name == "" {
		name = "anthropic"
	}
	f, ok := registry[name]
	if !ok {
		// Report the resolved name (post-default) so the operator sees
		// "anthropic" when they didn't set LLM_PROVIDER and forgot to
		// import the anthropic package, rather than a confusing "" miss.
		return nil, fmt.Errorf("unknown llm provider %q (registered: %s)",
			name, strings.Join(listProvidersLocked(), ", "))
	}
	return f(c)
}

// ListProviders returns the names of registered providers, sorted. Used
// by main to log what's available at startup.
func ListProviders() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return listProvidersLocked()
}

func listProvidersLocked() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
