package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/data"
	"github.com/levonn-dev/ro-builder/internal/domain"
)

func TestLookupMonster_Definition(t *testing.T) {
	tool := NewLookupMonster(loadCatalog(t))
	def := tool.Definition()
	if def.Name != "lookup_monster" {
		t.Errorf("name: got %q", def.Name)
	}
	var schema map[string]any
	if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
}

func TestLookupMonster_Execute_HappyPath(t *testing.T) {
	cat := loadCatalog(t)
	tool := NewLookupMonster(cat)
	out, err := tool.Execute(context.Background(), json.RawMessage(`{"iro_id":1002}`))
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if resp["id"] != float64(1002) {
		t.Errorf("id: got %v", resp["id"])
	}
	if resp["name"] != "Poring" {
		t.Errorf("name: got %v", resp["name"])
	}
	if resp["hp"] != float64(50) {
		t.Errorf("hp: got %v", resp["hp"])
	}
}

func TestLookupMonster_Execute_NotFound(t *testing.T) {
	cat := loadCatalog(t)
	tool := NewLookupMonster(cat)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"iro_id":99999999}`))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestLookupMonster_NilCatalogPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on nil catalog")
		}
	}()
	NewLookupMonster(nil)
}

// Server profile in context with a CustomMobs entry overrides the base
// catalog for that id. Verifies the overlay-first behavior; a lookup
// for an id the server re-stat's returns the overlay's stats, not stock.
func TestLookupMonster_OverlayWinsOverBase(t *testing.T) {
	cat := loadCatalog(t)
	tool := NewLookupMonster(cat)

	// Re-stat'd Poring (1002) for a hypothetical server: lvl 50, 5000 HP.
	// Same iRO id; overlay must win.
	profile := &domain.ServerProfile{
		Key: "test_server",
		CustomMobs: []data.Mob{
			{ID: 1002, Name: "Beefy Poring", Lv: 50, Hp: 5000, AtkMin: 100, AtkMax: 150},
		},
	}
	ctx := domain.WithServerProfile(context.Background(), profile)
	out, err := tool.Execute(ctx, json.RawMessage(`{"iro_id":1002}`))
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if resp["name"] != "Beefy Poring" {
		t.Errorf("expected overlay's name, got %v", resp["name"])
	}
	if resp["hp"] != float64(5000) {
		t.Errorf("expected overlay's hp 5000, got %v", resp["hp"])
	}
}

// New custom mob ids (not in the base catalog) resolve via the overlay.
// This is the "OGH Khalitzburg has a UARO-allocated id" case.
func TestLookupMonster_NewCustomIdResolvesViaOverlay(t *testing.T) {
	cat := loadCatalog(t)
	tool := NewLookupMonster(cat)

	profile := &domain.ServerProfile{
		Key: "test_server",
		CustomMobs: []data.Mob{
			{ID: 4001, Name: "OGH Boss", Lv: 99, Hp: 250000},
		},
	}
	ctx := domain.WithServerProfile(context.Background(), profile)
	out, err := tool.Execute(ctx, json.RawMessage(`{"iro_id":4001}`))
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if resp["name"] != "OGH Boss" {
		t.Errorf("expected custom mob's name, got %v", resp["name"])
	}
}

// When the overlay doesn't override a particular id, the base catalog
// is consulted normally; Poring stays Poring on a server whose overlay
// only re-stats other mobs.
func TestLookupMonster_FallsThroughToBaseWhenOverlayMisses(t *testing.T) {
	cat := loadCatalog(t)
	tool := NewLookupMonster(cat)

	profile := &domain.ServerProfile{
		Key: "test_server",
		CustomMobs: []data.Mob{
			{ID: 4001, Name: "Some Custom Mob"},
		},
	}
	ctx := domain.WithServerProfile(context.Background(), profile)
	out, err := tool.Execute(ctx, json.RawMessage(`{"iro_id":1002}`))
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if resp["name"] != "Poring" {
		t.Errorf("expected base catalog Poring, got %v", resp["name"])
	}
}
