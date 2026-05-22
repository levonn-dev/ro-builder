package catalog

import "testing"

// TestClassAliases_ResolveToCatalogKey locks the request-style alias
// table down: every alias must point at a class that exists in the
// embedded catalog. Mirrors calc-sidecar/test/supported-classes.test.ts
// on the Go side so the two language-side mappings can't silently drift.
func TestClassAliases_ResolveToCatalogKey(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	for alias, target := range classAliases {
		if _, ok := c.classes[target]; !ok {
			t.Errorf("alias %q → %q: target not present in catalog classes", alias, target)
		}
		if _, ok := c.ClassWithAliases(alias); !ok {
			t.Errorf("alias %q: ClassWithAliases returned not-found", alias)
		}
	}
}

// TestClassWithAliases_DirectMatch confirms non-aliased names still
// resolve directly so the alias table doesn't shadow the direct path.
func TestClassWithAliases_DirectMatch(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	// Pick a known direct-match name; "taekwon" is the catalog key the
	// "taekwon_kid" alias points at, so it must resolve directly.
	if _, ok := c.ClassWithAliases("taekwon"); !ok {
		t.Fatalf("ClassWithAliases(\"taekwon\"): expected direct match")
	}
}

// TestClassWithAliases_Unknown returns (zero, false) for nonsense
// inputs; neither a direct hit nor an alias.
func TestClassWithAliases_Unknown(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	if _, ok := c.ClassWithAliases("not_a_real_class"); ok {
		t.Fatalf("ClassWithAliases on unknown name should return false")
	}
}

// TestClassWithAliases_NormalizesInput confirms request-side casing/spacing
// variants resolve to the same record as the canonical key. The LLM
// sometimes emits "Taekwon_Kid" or "taekwon kid"; both must hit the row
// "taekwon_kid" lands on.
func TestClassWithAliases_NormalizesInput(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	want, ok := c.ClassWithAliases("taekwon_kid")
	if !ok {
		t.Fatalf("baseline ClassWithAliases(\"taekwon_kid\") missed")
	}
	got, ok := c.ClassWithAliases("Taekwon_Kid")
	if !ok {
		t.Fatalf("ClassWithAliases(\"Taekwon_Kid\") missed; input not normalized")
	}
	if got.Name != want.Name {
		t.Errorf("normalized lookup resolved different record: got %q, want %q", got.Name, want.Name)
	}
}
