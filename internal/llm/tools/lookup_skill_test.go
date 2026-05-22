package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLookupSkill_Definition(t *testing.T) {
	tool := NewLookupSkill(loadCatalog(t))
	def := tool.Definition()
	if def.Name != "lookup_skill" {
		t.Errorf("name: got %q", def.Name)
	}
	var schema map[string]any
	if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
		t.Fatalf("schema not valid JSON: %v", err)
	}
}

// Sword Mastery (iRO id 2) is a stable, well-known entry; the focus
// classes (Taekwon Kid, Knight, etc.) all reference skills via iRO id, so
// a regression here would silently break trajectory authoring.
func TestLookupSkill_Execute_HappyPath(t *testing.T) {
	cat := loadCatalog(t)
	tool := NewLookupSkill(cat)
	out, err := tool.Execute(context.Background(), json.RawMessage(`{"iro_id":2}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if resp["id"] != float64(2) {
		t.Errorf("id: got %v", resp["id"])
	}
	if resp["aegis_name"] != "SM_SWORD" {
		t.Errorf("aegis_name: got %v", resp["aegis_name"])
	}
	if resp["name"] != "Sword Mastery" {
		t.Errorf("name: got %v", resp["name"])
	}
	if resp["max_level"] != float64(10) {
		t.Errorf("max_level: got %v", resp["max_level"])
	}
}

func TestLookupSkill_Execute_NotFound(t *testing.T) {
	cat := loadCatalog(t)
	tool := NewLookupSkill(cat)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"iro_id":99999999}`))
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error: got %q", err.Error())
	}
}

func TestLookupSkill_NilCatalogPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on nil catalog")
		}
	}()
	NewLookupSkill(nil)
}
