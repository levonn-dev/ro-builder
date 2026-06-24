package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestListClassBuffs_Taekwon(t *testing.T) {
	cat := loadCat(t) // helper from buff_resolve_test.go
	tool := NewListClassBuffs(cat)
	raw, err := tool.Execute(context.Background(), json.RawMessage(`{"class":"taekwon_kid"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var out struct {
		Buffs []struct {
			Name string `json:"name"`
			Kind string `json:"kind"`
		} `json:"buffs"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	// 3 base TK buffs (mild_wind/spurt/taekwon_ranker) + 4 shared TaeKwon skills
	// (tumbling/peaceful_break/happy_break/kihop, added with Soul Linker) = 7.
	if len(out.Buffs) != 7 {
		t.Fatalf("expected 7 taekwon buffs, got %d: %s", len(out.Buffs), raw)
	}
}

func TestListClassBuffs_SuperNoviceInnate(t *testing.T) {
	cat := loadCat(t)
	tool := NewListClassBuffs(cat)
	raw, err := tool.Execute(context.Background(), json.RawMessage(`{"class":"super_novice"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var out struct {
		Buffs []struct {
			Name        string `json:"name"`
			AnchorSkill string `json:"anchor_skill"`
		} `json:"buffs"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, b := range out.Buffs {
		if b.Name == "no_death_bonus" {
			found = true
			if b.AnchorSkill != "(innate)" {
				t.Errorf("no_death_bonus anchor = %q, want (innate)", b.AnchorSkill)
			}
		}
	}
	if !found {
		t.Fatalf("no_death_bonus not listed for super_novice: %s", raw)
	}
}
