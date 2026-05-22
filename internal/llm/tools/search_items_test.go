package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSearchItems_Definition(t *testing.T) {
	tool := NewSearchItems(loadCatalog(t))
	def := tool.Definition()
	if def.Name != "search_items" {
		t.Errorf("name: got %q", def.Name)
	}
	var schema map[string]any
	if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
		t.Fatalf("schema not valid JSON: %v", err)
	}
}

func TestSearchItems_Execute_TypeFilter(t *testing.T) {
	cat := loadCatalog(t)
	tool := NewSearchItems(cat)
	out, err := tool.Execute(context.Background(), json.RawMessage(`{"type":"IT_WEAPON","limit":10}`))
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Count int              `json:"count"`
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Count != 10 {
		t.Errorf("count: got %d want 10", resp.Count)
	}
	for _, it := range resp.Items {
		if it["type"] != "IT_WEAPON" {
			t.Errorf("non-weapon in result: %v", it)
		}
	}
}

func TestSearchItems_Execute_NameContains(t *testing.T) {
	cat := loadCatalog(t)
	tool := NewSearchItems(cat)
	out, _ := tool.Execute(context.Background(), json.RawMessage(`{"type":"IT_WEAPON","name_contains":"Knife","limit":50}`))
	var resp struct {
		Count int              `json:"count"`
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(out, &resp)
	if resp.Count == 0 {
		t.Fatal("expected at least one Knife match")
	}
	// Basic Knife (1201) should be in there.
	found := false
	for _, it := range resp.Items {
		if it["id"] == float64(1201) {
			found = true
		}
	}
	if !found {
		t.Errorf("basic Knife (1201) missing from search")
	}
}

func TestSearchItems_NilCatalogPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on nil catalog")
		}
	}()
	NewSearchItems(nil)
}
