package domain

import (
	"strings"
	"testing"
)

func TestServerProfile_ClassMaxLevelOverride(t *testing.T) {
	p := &ServerProfile{
		ClassChanges: []ClassChange{
			{Class: "soul_linker", MaxJobLevel: 70},
			{Class: "knight", Rules: []string{"BB doubled"}}, // no override fields
		},
	}

	// Entry with only MaxJobLevel set.
	mb, mj, ok := p.ClassMaxLevelOverride("soul_linker")
	if !ok || mb != 0 || mj != 70 {
		t.Fatalf("soul_linker: got (%d, %d, %v); want (0, 70, true)", mb, mj, ok)
	}

	// Entry exists but both override fields are zero; not an override.
	if _, _, ok := p.ClassMaxLevelOverride("knight"); ok {
		t.Fatalf("knight: should have no override (both fields are zero)")
	}

	// Class not present at all.
	if _, _, ok := p.ClassMaxLevelOverride("priest"); ok {
		t.Fatalf("priest: should have no override (not in class_changes)")
	}

	// nil receiver.
	var nilProfile *ServerProfile
	if _, _, ok := nilProfile.ClassMaxLevelOverride("anything"); ok {
		t.Fatalf("nil profile: should have no override")
	}
}

func TestServerProfile_ClassMaxLevelOverride_BothFields(t *testing.T) {
	p := &ServerProfile{
		ClassChanges: []ClassChange{
			{Class: "taekwon_kid", MaxBaseLevel: 90, MaxJobLevel: 50},
		},
	}
	mb, mj, ok := p.ClassMaxLevelOverride("taekwon_kid")
	if !ok || mb != 90 || mj != 50 {
		t.Fatalf("taekwon_kid: got (%d, %d, %v); want (90, 50, true)", mb, mj, ok)
	}
}

// TestRenderForPrompt_ClassMaxLevelOverride verifies that when a class_changes
// entry carries max_job_level, the rendered prompt block includes the
// structured line before the free-form rules.
func TestRenderForPrompt_ClassMaxLevelOverride(t *testing.T) {
	p := &ServerProfile{
		Key:  "test",
		Name: "Test Server",
		Mode: "pre-renewal",
		ClassChanges: []ClassChange{
			{
				Class:       "soul_linker",
				MaxJobLevel: 70,
				Rules:       []string{"Max job level 70 (vs 50); HP/SP pools unchanged"},
			},
		},
	}
	rendered := p.RenderForPrompt("soul_linker")
	if !strings.Contains(rendered, "max job level: 70") {
		t.Errorf("rendered prompt missing 'max job level: 70':\n%s", rendered)
	}
	// Rules text should still be present too.
	if !strings.Contains(rendered, "Max job level 70 (vs 50)") {
		t.Errorf("rendered prompt missing rules text:\n%s", rendered)
	}
}
