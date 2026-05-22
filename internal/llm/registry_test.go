package llm

import (
	"context"
	"strings"
	"testing"
)

// stubProvider satisfies Provider for registry tests without pulling in a
// concrete impl. Each test that needs a stand-in registers a unique name
// inside its own t.Run so parallel tests don't trip the duplicate-panic.
type stubProvider struct{ tag string }

func (s *stubProvider) Complete(_ context.Context, _ string, _ []Message, _ []Tool) (Response, error) {
	return Response{}, nil
}

func TestRegister_New_RoundTrip(t *testing.T) {
	const name = "test-roundtrip"
	Register(name, func(c Config) (Provider, error) {
		return &stubProvider{tag: c.APIKey}, nil
	})
	p, err := New(Config{Provider: name, APIKey: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	stub, ok := p.(*stubProvider)
	if !ok {
		t.Fatalf("expected *stubProvider, got %T", p)
	}
	if stub.tag != "abc" {
		t.Errorf("config not threaded through: got %q", stub.tag)
	}
}

func TestRegister_PanicsOnDuplicate(t *testing.T) {
	const name = "test-dup"
	Register(name, func(Config) (Provider, error) { return nil, nil })
	defer func() {
		if recover() == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	Register(name, func(Config) (Provider, error) { return nil, nil })
}

func TestNew_UnknownProviderListsRegistered(t *testing.T) {
	const name = "test-known"
	Register(name, func(Config) (Provider, error) { return &stubProvider{}, nil })
	_, err := New(Config{Provider: "does-not-exist"})
	if err == nil {
		t.Fatal("expected error")
	}
	// Error message should help the operator diagnose: name they tried,
	// list of names they could have used.
	if !strings.Contains(err.Error(), `"does-not-exist"`) {
		t.Errorf("error should mention the bad name: got %q", err.Error())
	}
	if !strings.Contains(err.Error(), name) {
		t.Errorf("error should list registered providers: got %q", err.Error())
	}
}

func TestNew_EmptyProviderDefaultsToAnthropic(t *testing.T) {
	// An empty Config.Provider should default to "anthropic" so the
	// common case doesn't require LLM_PROVIDER to be set.
	_, err := New(Config{Provider: ""})
	// We don't expect anthropic to be registered in this isolated test
	// (the side-effect import lives in cmd/api). The error message should
	// mention "anthropic"; proving that's what was looked up.
	if err == nil {
		// If anthropic IS registered (e.g. by another test in the same
		// package), the call may succeed with an empty key; that's fine,
		// the default-name behavior is what we're checking.
		return
	}
	if !strings.Contains(err.Error(), "anthropic") {
		t.Errorf("expected default-to-anthropic in error: got %q", err.Error())
	}
}

func TestListProviders_ReturnsSorted(t *testing.T) {
	// Register in non-alphabetical order; the output should still be sorted.
	Register("test-zeta", func(Config) (Provider, error) { return &stubProvider{}, nil })
	Register("test-alpha", func(Config) (Provider, error) { return &stubProvider{}, nil })
	got := ListProviders()
	idxA, idxZ := -1, -1
	for i, n := range got {
		if n == "test-alpha" {
			idxA = i
		}
		if n == "test-zeta" {
			idxZ = i
		}
	}
	if idxA == -1 || idxZ == -1 || idxA > idxZ {
		t.Errorf("expected test-alpha before test-zeta: %v", got)
	}
}

// Sanity check that the package's exported error formatting includes the
// faulty provider name so operators can self-diagnose.
func TestNew_ErrorMentionsProvider(t *testing.T) {
	_, err := New(Config{Provider: "ghost-provider"})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if !strings.Contains(err.Error(), "ghost-provider") {
		t.Errorf("error should name the missing provider: %v", err)
	}
}
