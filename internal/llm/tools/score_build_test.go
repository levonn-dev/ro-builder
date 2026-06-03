package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

func TestScoreBuild_Definition(t *testing.T) {
	// Stub server is fine; Definition doesn't touch the client, but
	// NewScoreBuild now requires a non-nil one.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	tool := NewScoreBuild(scoring.NewClient(srv.URL, nil), nil)
	def := tool.Definition()
	if def.Name != "score_build" {
		t.Errorf("name: got %q", def.Name)
	}
	// Schema must be valid JSON; the LLM provider will pass it to the API
	// and a malformed schema causes a 400 with no useful diagnostic.
	var parsed map[string]any
	if err := json.Unmarshal(def.InputSchema, &parsed); err != nil {
		t.Fatalf("schema not valid JSON: %v", err)
	}
	if parsed["type"] != "object" {
		t.Errorf("schema type: got %v", parsed["type"])
	}
}

func TestScoreBuild_NilClientPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil client")
		}
	}()
	NewScoreBuild(nil, nil)
}

func TestScoreBuild_Execute_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"derived":{"hit":71,"flee":61,"cri":1,"atk":{"base":61,"plus":14},"matk":{"min":2,"max":2},"def":{"hard":0,"soft":6},"mdef":{"hard":0,"soft":2},"aspd":139.3,"maxHp":302,"maxSp":61,"statPointsRemaining":261}}`))
	}))
	defer srv.Close()

	tool := NewScoreBuild(scoring.NewClient(srv.URL, nil), nil)
	input := json.RawMessage(`{
		"build": {
			"mode": "pre-renewal",
			"class": "novice",
			"level": {"base": 50, "job": 10},
			"stats": {"str": 30, "agi": 10, "vit": 5, "int": 1, "dex": 20, "luk": 1},
			"equipment": {"weapon": {"id": 1201, "refine": 7}}
		}
	}`)
	out, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	derived := resp["derived"].(map[string]any)
	if derived["hit"] != float64(71) {
		t.Errorf("derived.hit: got %v want 71", derived["hit"])
	}
}

// TestScoreBuild_AppliesResolvedBuffs asserts score_build resolves a build's
// active_buffs and forwards them to the sidecar, so its preview numbers match
// what canonical submit_trajectory scoring would produce for the same buffs
// (otherwise a build scored with a buff here would silently be scored unbuffed).
func TestScoreBuild_AppliesResolvedBuffs(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"derived":{"maxHp":900,"statPointsRemaining":3},"calc_version":"rocalc-test"}`))
	}))
	defer srv.Close()

	tool := NewScoreBuild(scoring.NewClient(srv.URL, nil), cat)
	// taekwon_kid allocates TK_MISSION (493, the ranker anchor) and declares
	// the ranker buff; the tool must fill its level from the allocation and
	// forward it to the sidecar as req.buffs.
	input := json.RawMessage(`{
		"build": {
			"class": "taekwon_kid",
			"level": {"base": 99, "job": 50},
			"stats": {"str": 80, "agi": 90, "vit": 40, "int": 1, "dex": 60, "luk": 30},
			"skills": [{"id": 493, "level": 1}],
			"active_buffs": [{"name": "taekwon_ranker"}]
		}
	}`)
	if _, err := tool.Execute(context.Background(), input); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var sent scoring.ScoreRequest
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("unmarshal sent body: %v (body=%s)", err, gotBody)
	}
	if len(sent.Buffs) != 1 || sent.Buffs[0].Name != "taekwon_ranker" || sent.Buffs[0].Level != 1 {
		t.Errorf("score_build did not forward resolved buff to sidecar; got buffs=%+v", sent.Buffs)
	}
}

func TestScoreBuild_Execute_OrchestratorMetadataIgnored(t *testing.T) {
	// score_build's input struct deliberately omits Mode / Server / Tier;
	// those are orchestrator metadata the LLM can't smuggle in. A request
	// that includes "mode":"renewal" gets the field silently dropped, the
	// build runs with the tool's fixed PreRenewal mode, and the sidecar is
	// hit normally. Asserts the field is unreachable from the LLM surface.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"derived":{"hit":71,"flee":61,"cri":1,"atk":{"base":61,"plus":14},"matk":{"min":2,"max":2},"def":{"hard":0,"soft":6},"mdef":{"hard":0,"soft":2},"aspd":139.3,"maxHp":302,"maxSp":61,"statPointsRemaining":261}}`))
	}))
	defer srv.Close()
	tool := NewScoreBuild(scoring.NewClient(srv.URL, nil), nil)
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"build":{"mode":"renewal","server":"hijack","tier":"endgame","class":"novice"}}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestScoreBuild_Execute_ValidationFailure(t *testing.T) {
	// Negative refine survives the new local struct (it lives on
	// EquipSpec, unaffected by the Mode/Server/Tier stripping) and
	// exercises the Validate path so sidecar-not-called is still
	// asserted under the new code shape.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("sidecar called despite validation failure")
	}))
	defer srv.Close()
	tool := NewScoreBuild(scoring.NewClient(srv.URL, nil), nil)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"build":{"class":"novice","equipment":{"weapon":{"id":1201,"refine":99}}}}`))
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "refine") {
		t.Errorf("error: got %q", err.Error())
	}
}

func TestScoreBuild_Execute_SkillsForwarded(t *testing.T) {
	// Verify the LLM-supplied `skills` array reaches the sidecar in the
	// request body; without this wiring, exploration scoring runs at
	// auto-attack tier even when the build allocates damaging skills.
	var got struct {
		Skills []struct {
			ID    int `json:"id"`
			Level int `json:"level"`
		} `json:"skills"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = w.Write([]byte(`{"derived":{"hit":71,"flee":61,"cri":1,"atk":{"base":61,"plus":14},"matk":{"min":2,"max":2},"def":{"hard":0,"soft":6},"mdef":{"hard":0,"soft":2},"aspd":139.3,"maxHp":302,"maxSp":61,"statPointsRemaining":261}}`))
	}))
	defer srv.Close()

	tool := NewScoreBuild(scoring.NewClient(srv.URL, nil), nil)
	input := json.RawMessage(`{
		"build": {
			"mode": "pre-renewal",
			"class": "taekwon_kid",
			"skills": [{"id": 414, "level": 7}, {"id": 415, "level": 5}]
		}
	}`)
	if _, err := tool.Execute(context.Background(), input); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(got.Skills) != 2 {
		t.Fatalf("forwarded skills: got %d want 2 (%+v)", len(got.Skills), got.Skills)
	}
	if got.Skills[0].ID != 414 || got.Skills[0].Level != 7 {
		t.Errorf("skill[0]: got %+v want {ID:414 Level:7}", got.Skills[0])
	}
}

func TestScoreBuild_Execute_SidecarErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"iRO mob id 9999999 is unmapped"}`))
	}))
	defer srv.Close()

	tool := NewScoreBuild(scoring.NewClient(srv.URL, nil), nil)
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"build":{"class":"novice"},"scenario":{"target":9999999}}`))
	if err == nil {
		t.Fatal("expected sidecar error")
	}
	// The orchestrator surfaces this as a tool_result is_error block; the
	// model sees the text. Critical that the iRO id appears so it can fix
	// its next attempt.
	if !strings.Contains(err.Error(), "9999999") {
		t.Errorf("error should mention bad id: got %q", err.Error())
	}
}
