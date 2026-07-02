package tools

import (
	"encoding/json"
	"testing"
)

func TestGetSavedBuild_Definition(t *testing.T) {
	def := NewGetSavedBuild(nil).Definition()
	if def.Name != "get_saved_build" {
		t.Errorf("Name = %q, want get_saved_build", def.Name)
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
		t.Fatalf("InputSchema not valid JSON: %v", err)
	}
	found := false
	for _, r := range schema.Required {
		if r == "id" {
			found = true
		}
	}
	if !found {
		t.Errorf("schema must require 'id'; got required=%v", schema.Required)
	}
}
