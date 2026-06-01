package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/scoring"
)

func TestScoreBuilds_Definition(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	tool := NewScoreBuilds(scoring.NewClient(srv.URL, nil))
	def := tool.Definition()
	if def.Name != "score_builds" {
		t.Errorf("name: got %q", def.Name)
	}
	var parsed map[string]any
	if err := json.Unmarshal(def.InputSchema, &parsed); err != nil {
		t.Fatalf("schema not valid JSON: %v", err)
	}
}

func TestScoreBuilds_NilClientPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil client")
		}
	}()
	NewScoreBuilds(nil)
}

func TestScoreBuilds_Execute_ScoresEachCandidate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"derived":{"hit":71,"flee":61,"cri":1,"atk":{"base":61,"plus":14},"matk":{"min":2,"max":2},"def":{"hard":0,"soft":6},"mdef":{"hard":0,"soft":2},"aspd":139.3,"maxHp":302,"maxSp":61,"statPointsRemaining":5}}`))
	}))
	defer srv.Close()

	tool := NewScoreBuilds(scoring.NewClient(srv.URL, nil))
	input := json.RawMessage(`{
		"builds": [
			{"label": "agi", "build": {"class": "taekwon_kid", "stats": {"agi": 50}}},
			{"label": "str", "build": {"class": "taekwon_kid", "stats": {"str": 50}}}
		]
	}`)
	out, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp struct {
		Results []struct {
			Label string          `json:"label"`
			Index int             `json:"index"`
			Score json.RawMessage `json:"score"`
			Error string          `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results: got %d want 2", len(resp.Results))
	}
	if resp.Results[0].Label != "agi" || resp.Results[1].Label != "str" {
		t.Errorf("labels: got %q,%q want agi,str", resp.Results[0].Label, resp.Results[1].Label)
	}
	for i, r := range resp.Results {
		if r.Error != "" {
			t.Errorf("result %d unexpected error: %s", i, r.Error)
		}
		if len(r.Score) == 0 {
			t.Errorf("result %d missing score", i)
		}
	}
}

func TestScoreBuilds_Execute_PerCandidateErrorDoesNotAbortBatch(t *testing.T) {
	// One candidate fails validation (bad refine) before the sidecar; the
	// batch still returns, that candidate's error captured, the other scored.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"derived":{"hit":71,"flee":61,"cri":1,"atk":{"base":61,"plus":14},"matk":{"min":2,"max":2},"def":{"hard":0,"soft":6},"mdef":{"hard":0,"soft":2},"aspd":139.3,"maxHp":302,"maxSp":61,"statPointsRemaining":5}}`))
	}))
	defer srv.Close()

	tool := NewScoreBuilds(scoring.NewClient(srv.URL, nil))
	input := json.RawMessage(`{
		"builds": [
			{"label": "ok", "build": {"class": "novice"}},
			{"label": "bad", "build": {"class": "novice", "equipment": {"weapon": {"id": 1201, "refine": 99}}}}
		]
	}`)
	out, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute should not fail the whole batch: %v", err)
	}
	var resp struct {
		Results []struct {
			Label string `json:"label"`
			Error string `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results: got %d want 2", len(resp.Results))
	}
	if resp.Results[0].Error != "" {
		t.Errorf("candidate 0 should have scored, got error %q", resp.Results[0].Error)
	}
	if !strings.Contains(resp.Results[1].Error, "refine") {
		t.Errorf("candidate 1 should carry a validation error mentioning refine, got %q", resp.Results[1].Error)
	}
}

func TestScoreBuilds_Execute_SidecarOutageAbortsBatch(t *testing.T) {
	// A 5xx is a global infrastructure failure, not a per-candidate input
	// problem: every candidate would hit it. The batch must surface it as a
	// tool error (is_error) so the model learns the engine is down, rather
	// than returning a comparison where every candidate silently "failed".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"shim crashed"}`))
	}))
	defer srv.Close()

	tool := NewScoreBuilds(scoring.NewClient(srv.URL, nil))
	input := json.RawMessage(`{
		"builds": [
			{"label": "a", "build": {"class": "novice"}},
			{"label": "b", "build": {"class": "swordsman"}}
		]
	}`)
	if _, err := tool.Execute(context.Background(), input); err == nil {
		t.Fatal("expected a 5xx sidecar outage to abort the batch with an error")
	}
}

func TestScoreBuilds_Execute_EmptyBuildsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	tool := NewScoreBuilds(scoring.NewClient(srv.URL, nil))
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"builds":[]}`)); err == nil {
		t.Fatal("expected error for empty builds")
	}
}
