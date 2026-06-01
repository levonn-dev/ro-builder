package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// fakeScoringClient returns a fixed score for every Score call, or the
// injected error if err is set. Sufficient for the single-snapshot
// trajectories the unit tests use.
type fakeScoringClient struct {
	resp *scoring.ScoreResponse
	err  error
}

func (f *fakeScoringClient) Score(_ context.Context, _ *scoring.ScoreRequest) (*scoring.ScoreResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func passingSnapshot() domain.Snapshot {
	return domain.Snapshot{
		Class: "novice",
		Level: domain.Level{Base: 99, Job: 50},
		Stats: domain.Stats{Str: 1, Agi: 1, Vit: 1, Int: 1, Dex: 1, Luk: 1},
	}
}

func TestSubmit_AcceptsWhenGatesPass(t *testing.T) {
	var cat *catalog.Catalog // nil; tests don't exercise the resolved-names echo path
	sc := &fakeScoringClient{resp: &scoring.ScoreResponse{Derived: scoring.DerivedStats{MaxHP: 5000}, CalcVersion: "rocalc-test"}}

	accepted := false
	var savedTraj domain.Trajectory
	tool := NewSubmitTrajectory(SubmitTrajectoryDeps{
		Catalog: cat,
		Scoring: sc,
		EvaluateGates: func(_ *scoring.ScoreResponse, _ *domain.Snapshot) []domain.GateResult {
			return []domain.GateResult{{Name: "hit_floor", Severity: domain.GateSeverityPass}}
		},
		Accept: func(a domain.Trajectory, _ []domain.Trajectory, _ string) bool {
			accepted = true
			savedTraj = a
			return true
		},
	})

	in := SubmitTrajectoryInput{
		Primary: domain.Trajectory{
			Class:     "novice",
			Snapshots: []domain.Snapshot{passingSnapshot()},
		},
	}
	raw, _ := json.Marshal(in)
	out, err := tool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !accepted {
		t.Fatalf("Accept callback not invoked")
	}
	if savedTraj.Class != "novice" {
		t.Fatalf("saved trajectory class: %q", savedTraj.Class)
	}
	if !strings.Contains(string(out), `"accepted":true`) {
		t.Fatalf("expected accepted:true in tool result, got %s", string(out))
	}
}

func TestSubmit_RejectsWhenGatesFail(t *testing.T) {
	var cat *catalog.Catalog // nil
	sc := &fakeScoringClient{resp: &scoring.ScoreResponse{Derived: scoring.DerivedStats{MaxHP: 100}}}

	accepted := false
	tool := NewSubmitTrajectory(SubmitTrajectoryDeps{
		Catalog: cat,
		Scoring: sc,
		EvaluateGates: func(_ *scoring.ScoreResponse, _ *domain.Snapshot) []domain.GateResult {
			return []domain.GateResult{{
				Name:      "hit_floor",
				Severity:  domain.GateSeverityFail,
				Threshold: 80,
				Actual:    42,
				Reason:    "hit rate below threshold",
			}}
		},
		Accept: func(_ domain.Trajectory, _ []domain.Trajectory, _ string) bool {
			accepted = true
			return true
		},
	})

	in := SubmitTrajectoryInput{
		Primary: domain.Trajectory{Class: "novice", Snapshots: []domain.Snapshot{passingSnapshot()}},
	}
	raw, _ := json.Marshal(in)
	_, err := tool.Execute(context.Background(), raw)
	if err == nil {
		t.Fatalf("Execute should have returned a Go error for failing gates")
	}
	if accepted {
		t.Fatalf("Accept callback should not have fired on gate failure")
	}
	if !strings.Contains(err.Error(), "hit_floor") {
		t.Fatalf("expected gate name in error, got: %v", err)
	}
}

func TestSubmit_AcceptCallbackFiresEachTime(t *testing.T) {
	// The tool's job is to forward every successful (gates-passing)
	// submission to the callback; the accept/replace/lock policy
	// (first-clean-wins) lives in the callback (Session), not the tool.
	var cat *catalog.Catalog
	sc := &fakeScoringClient{resp: &scoring.ScoreResponse{Derived: scoring.DerivedStats{MaxHP: 5000}}}

	acceptCount := 0
	tool := NewSubmitTrajectory(SubmitTrajectoryDeps{
		Catalog: cat,
		Scoring: sc,
		EvaluateGates: func(_ *scoring.ScoreResponse, _ *domain.Snapshot) []domain.GateResult {
			return []domain.GateResult{{Name: "ok", Severity: domain.GateSeverityPass}}
		},
		Accept: func(_ domain.Trajectory, _ []domain.Trajectory, _ string) bool {
			acceptCount++
			return acceptCount == 1
		},
	})

	in := SubmitTrajectoryInput{Primary: domain.Trajectory{Class: "novice", Snapshots: []domain.Snapshot{passingSnapshot()}}}
	raw, _ := json.Marshal(in)
	if _, err := tool.Execute(context.Background(), raw); err != nil {
		t.Fatalf("first execute: %v", err)
	}
	if _, err := tool.Execute(context.Background(), raw); err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if acceptCount != 2 {
		t.Fatalf("Accept should fire on every gates-passing execute, got count=%d", acceptCount)
	}
}

func TestSubmitTrajectory_Definition(t *testing.T) {
	tool := NewSubmitTrajectory(SubmitTrajectoryDeps{})
	def := tool.Definition()
	if def.Name != SubmitTrajectoryToolName {
		t.Errorf("name: got %q want %q", def.Name, SubmitTrajectoryToolName)
	}
	if !strings.Contains(string(def.InputSchema), "primary") {
		t.Errorf("schema missing 'primary' field: %s", def.InputSchema)
	}
	if !strings.Contains(string(def.InputSchema), "snapshots") {
		t.Errorf("schema missing 'snapshots' field: %s", def.InputSchema)
	}
}

func TestSubmitTrajectory_Execute_AcceptsValidInput(t *testing.T) {
	tool := NewSubmitTrajectory(SubmitTrajectoryDeps{})
	in := json.RawMessage(`{
		"primary": {
			"class": "novice",
			"snapshots": [
				{"class": "novice", "level": {"base": 1, "job": 1}, "stats": {"str": 1, "agi": 1, "vit": 1, "int": 1, "dex": 1, "luk": 1}}
			]
		}
	}`)
	out, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("expected ok ack, got err: %v", err)
	}
	if !strings.Contains(string(out), `"accepted":true`) {
		t.Errorf("ack missing accepted=true: %s", out)
	}
	if !strings.Contains(string(out), `"primary_snapshots":1`) {
		t.Errorf("ack missing snapshot count: %s", out)
	}
}

func TestSubmitTrajectory_Execute_RejectsInvalidTrajectory(t *testing.T) {
	tool := NewSubmitTrajectory(SubmitTrajectoryDeps{})
	// last snapshot's class doesn't match trajectory's class; Validate
	// rejects this. The LLM should see an is_error tool_result and self-correct.
	in := json.RawMessage(`{
		"primary": {
			"class": "assassin_cross",
			"snapshots": [
				{"class": "thief", "level": {"base": 50, "job": 50}, "stats": {"str": 30, "agi": 30, "vit": 1, "int": 1, "dex": 1, "luk": 1}}
			]
		}
	}`)
	_, err := tool.Execute(context.Background(), in)
	if err == nil {
		t.Fatal("expected error for last-class mismatch")
	}
	if !strings.Contains(err.Error(), "last snapshot class") {
		t.Errorf("err: got %v, want substring 'last snapshot class'", err)
	}
}

func TestSubmitTrajectory_Execute_RejectsMalformedJSON(t *testing.T) {
	tool := NewSubmitTrajectory(SubmitTrajectoryDeps{})
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"primary": "not an object"}`))
	if err == nil {
		t.Fatal("expected error for malformed input")
	}
}

// TestSubmitTrajectory_Execute_EchoesResolvedNames covers the
// pretrained-id-mistake guardrail: when the LLM submits equipment with
// IDs that resolve to different catalog names than it expected, the
// echo must surface the actual names so the model can self-correct.
// Uses the real embedded catalog to keep the test honest about which
// ids resolve to which names.
func TestSubmitTrajectory_Execute_EchoesResolvedNames(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	tool := NewSubmitTrajectory(SubmitTrajectoryDeps{Catalog: cat})
	in := json.RawMessage(`{
		"primary": {
			"class": "novice",
			"snapshots": [
				{
					"class": "novice",
					"level": {"base": 1, "job": 1},
					"stats": {"str": 1, "agi": 1, "vit": 1, "int": 1, "dex": 1, "luk": 1},
					"equipment": {"headTop": {"id": 5100}}
				}
			]
		}
	}`)
	out, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("expected ok ack, got err: %v", err)
	}
	// Item 5100 is "Sales Banner" in the embedded Hercules catalog;
	// the test asserts the echo names what the catalog actually has,
	// not whatever the model thought it was submitting.
	if !strings.Contains(string(out), `"resolved_equipment"`) {
		t.Errorf("ack missing resolved_equipment: %s", out)
	}
	if !strings.Contains(string(out), "5100") || !strings.Contains(string(out), "Sales Banner") {
		t.Errorf("expected echoed slot name to include id 5100 + Sales Banner, got: %s", out)
	}
	if !strings.Contains(string(out), `"verification_instructions"`) {
		t.Errorf("ack missing verification_instructions: %s", out)
	}
}

// TestSubmitTrajectory_Execute_FlagsUnknownIds covers the case where
// the LLM picks an id that doesn't resolve to anything in the catalog.
// The echo must surface a clear NOT FOUND signal so the model can fix.
func TestSubmitTrajectory_Execute_FlagsUnknownIds(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}
	tool := NewSubmitTrajectory(SubmitTrajectoryDeps{Catalog: cat})
	in := json.RawMessage(`{
		"primary": {
			"class": "novice",
			"snapshots": [
				{
					"class": "novice",
					"level": {"base": 1, "job": 1},
					"stats": {"str": 1, "agi": 1, "vit": 1, "int": 1, "dex": 1, "luk": 1},
					"equipment": {"weapon": {"id": 99999999}}
				}
			]
		}
	}`)
	out, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("expected ok ack, got err: %v", err)
	}
	if !strings.Contains(string(out), "NOT FOUND IN CATALOG") {
		t.Errorf("expected NOT FOUND signal for unknown id, got: %s", out)
	}
}

// TestSubmitTrajectory_Execute_NilCatalogSkipsEcho covers the catalog-
// less wiring path (tests, library-only consumers): the tool must
// still validate and ack without panicking, just without the echo.
func TestSubmitTrajectory_Execute_NilCatalogSkipsEcho(t *testing.T) {
	tool := NewSubmitTrajectory(SubmitTrajectoryDeps{})
	in := json.RawMessage(`{
		"primary": {
			"class": "novice",
			"snapshots": [
				{
					"class": "novice",
					"level": {"base": 1, "job": 1},
					"stats": {"str": 1, "agi": 1, "vit": 1, "int": 1, "dex": 1, "luk": 1},
					"equipment": {"weapon": {"id": 1201}}
				}
			]
		}
	}`)
	out, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("expected ok ack, got err: %v", err)
	}
	if strings.Contains(string(out), `"resolved_equipment"`) {
		t.Errorf("nil catalog should omit resolved_equipment, got: %s", out)
	}
}

// TestSubmit_SuccessAckReportsPerCheckpointBudget covers Claim 1's
// success path: the model no longer pre-scores checkpoints, so the
// accept ack must surface each scored snapshot's statPointsRemaining and
// any non-pass gate (e.g. an under-spend warning) so the model can decide
// whether to resubmit without a separate score_build pass.
func TestSubmit_SuccessAckReportsPerCheckpointBudget(t *testing.T) {
	var cat *catalog.Catalog
	sc := &fakeScoringClient{resp: &scoring.ScoreResponse{
		Derived:     scoring.DerivedStats{MaxHP: 5000, StatPointsRemaining: 12},
		CalcVersion: "rocalc-test",
	}}
	tool := NewSubmitTrajectory(SubmitTrajectoryDeps{
		Catalog: cat,
		Scoring: sc,
		EvaluateGates: func(_ *scoring.ScoreResponse, _ *domain.Snapshot) []domain.GateResult {
			return []domain.GateResult{{
				Name:      "stat_points_underspent",
				Severity:  domain.GateSeverityWarn,
				Threshold: 5,
				Actual:    12,
				Reason:    "12 unspent stat points; raise stats to use the full budget",
			}}
		},
		Accept: func(_ domain.Trajectory, _ []domain.Trajectory, _ string) bool { return true },
	})

	in := SubmitTrajectoryInput{Primary: domain.Trajectory{Class: "novice", Snapshots: []domain.Snapshot{passingSnapshot()}}}
	raw, _ := json.Marshal(in)
	out, err := tool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `"checkpoints"`) {
		t.Fatalf("ack missing checkpoints array: %s", s)
	}
	if !strings.Contains(s, `"stat_points_remaining":12`) {
		t.Fatalf("ack missing per-checkpoint statPointsRemaining: %s", s)
	}
	if !strings.Contains(s, "stat_points_underspent") {
		t.Fatalf("ack should surface the under-spend warn gate so the model can resubmit: %s", s)
	}
}

// TestSubmit_RejectionIncludesPerCheckpointBudget covers Claim 1's
// rejection path: a gate failure must report the offending checkpoint's
// statPointsRemaining inline so the model can correct the allocation in a
// single resubmit instead of re-deriving the number with score_build.
func TestSubmit_RejectionIncludesPerCheckpointBudget(t *testing.T) {
	var cat *catalog.Catalog
	sc := &fakeScoringClient{resp: &scoring.ScoreResponse{
		Derived: scoring.DerivedStats{MaxHP: 100, StatPointsRemaining: -8},
	}}
	tool := NewSubmitTrajectory(SubmitTrajectoryDeps{
		Catalog: cat,
		Scoring: sc,
		EvaluateGates: func(_ *scoring.ScoreResponse, _ *domain.Snapshot) []domain.GateResult {
			return []domain.GateResult{{
				Name:      "stat_points_overspent",
				Severity:  domain.GateSeverityFailHard,
				Threshold: 0,
				Actual:    -8,
				Reason:    "build allocates 8 more stat points than the level provides",
			}}
		},
		Accept: func(_ domain.Trajectory, _ []domain.Trajectory, _ string) bool { return true },
	})

	in := SubmitTrajectoryInput{Primary: domain.Trajectory{Class: "novice", Snapshots: []domain.Snapshot{passingSnapshot()}}}
	raw, _ := json.Marshal(in)
	_, err := tool.Execute(context.Background(), raw)
	if err == nil {
		t.Fatalf("expected rejection error for stat overspend")
	}
	if !strings.Contains(err.Error(), "statPointsRemaining=-8") {
		t.Fatalf("rejection should include the offending checkpoint's statPointsRemaining; got: %v", err)
	}
	if !strings.Contains(err.Error(), "stat_points_overspent") {
		t.Fatalf("rejection should still name the failing gate; got: %v", err)
	}
}

func TestParseSubmitTrajectory_PassesAlternativesThrough(t *testing.T) {
	in := json.RawMessage(`{
		"primary": {"class": "novice", "snapshots": []},
		"alternatives": [
			{"class": "novice", "snapshots": []},
			{"class": "novice", "snapshots": []}
		]
	}`)
	parsed, err := ParseSubmitTrajectory(in)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(parsed.Alternatives) != 2 {
		t.Errorf("alternatives: got %d want 2", len(parsed.Alternatives))
	}
}
