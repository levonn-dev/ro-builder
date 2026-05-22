package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/catalog"
)

func loadCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load()
	if err != nil {
		t.Fatalf("catalog Load: %v", err)
	}
	return c
}

func TestLookupItem_Definition(t *testing.T) {
	tool := NewLookupItem(loadCatalog(t))
	def := tool.Definition()
	if def.Name != "lookup_item" {
		t.Errorf("name: got %q", def.Name)
	}
	var schema map[string]any
	if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
		t.Fatalf("schema not valid JSON: %v", err)
	}
}

func TestLookupItem_Execute_HappyPath(t *testing.T) {
	cat := loadCatalog(t)
	tool := NewLookupItem(cat)
	out, err := tool.Execute(context.Background(), json.RawMessage(`{"iro_id":1201}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if resp["id"] != float64(1201) {
		t.Errorf("id: got %v", resp["id"])
	}
	if resp["aegis_name"] != "Knife" {
		t.Errorf("aegis_name: got %v", resp["aegis_name"])
	}
	if resp["type"] != "IT_WEAPON" {
		t.Errorf("type: got %v", resp["type"])
	}
}

func TestLookupItem_Execute_NotFound(t *testing.T) {
	cat := loadCatalog(t)
	tool := NewLookupItem(cat)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"iro_id":99999999}`))
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error: got %q", err.Error())
	}
}

func TestLookupItem_NilCatalogPanics(t *testing.T) {
	// Nil catalog is a programmer error, not a graceful-degradation case;
	// the API loads from the embedded asset at startup. NewLookupItem
	// panics so the wiring bug surfaces at process start, not request time.
	defer func() {
		if recover() == nil {
			t.Error("expected panic on nil catalog")
		}
	}()
	NewLookupItem(nil)
}
