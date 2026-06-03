package catalog

import (
	"testing"
)

// Uses the embedded catalog.json (regenerated in Task 2 with TK's buffs).
func TestClassBuffs_Taekwon(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("taekwon_kid") // alias of "taekwon"
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for taekwon_kid")
	}
	want := map[string]bool{"mild_wind": false, "spurt": false, "taekwon_ranker": false}
	for _, b := range buffs {
		if b.SelfBuff == nil {
			t.Errorf("ClassBuffs returned a skill with nil SelfBuff: %s", b.AegisName)
			continue
		}
		want[b.SelfBuff.Name] = true
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected buff %q in taekwon ClassBuffs, missing", name)
		}
	}
}

// TestMildWindOrder pins the element unlock order for Mild Wind (TK_SEVENWIND)
// read from the catalog. This is the SOURCE OF TRUTH for the unlock sequence;
// the sidecar's MILD_WIND_ORDER const in
// calc-sidecar/src/backends/rocalc/index.ts is a defense-in-depth mirror of
// exactly this list. If either diverges, the behavioral tests in
// calc-sidecar/test/backends/rocalc/buffs.test.ts (sidecar side) or this test
// (catalog side) will fail. Changing this order is a coordinated two-file edit.
func TestMildWindOrder(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	buffs, ok := c.ClassBuffs("taekwon_kid")
	if !ok {
		t.Fatal("ClassBuffs returned ok=false for taekwon_kid")
	}
	var mildWind *ClassSkill
	for i := range buffs {
		if buffs[i].SelfBuff != nil && buffs[i].SelfBuff.Name == "mild_wind" {
			mildWind = &buffs[i]
			break
		}
	}
	if mildWind == nil {
		t.Fatal("mild_wind buff not found in taekwon_kid ClassBuffs")
	}
	if mildWind.SelfBuff.Endow == nil {
		t.Fatal("mild_wind SelfBuff.Endow is nil; expected an EndowSpec")
	}

	// Canonical order: index (1-based) == required Mild Wind level.
	// earth=1, wind=2, water=3, fire=4, ghost=5, shadow=6, holy=7.
	// Mirrors MILD_WIND_ORDER in calc-sidecar/src/backends/rocalc/index.ts.
	want := []string{"earth", "wind", "water", "fire", "ghost", "shadow", "holy"}
	got := mildWind.SelfBuff.Endow.Elements
	if len(got) != len(want) {
		t.Fatalf("mild_wind endow.elements: got %v (len %d), want %v (len %d)", got, len(got), want, len(want))
	}
	for i, el := range want {
		if got[i] != el {
			t.Errorf("endow.elements[%d]: got %q, want %q", i, got[i], el)
		}
	}
}
