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
	sc := &fakeScoringClient{resp: &scoring.ScoreResponse{Derived: scoring.DerivedStats{MaxHP: 5000}, CalcVersion: "calc-test"}}

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
	if !json.Valid(def.InputSchema) {
		t.Errorf("InputSchema is not valid JSON: %s", def.InputSchema)
	}
	if !strings.Contains(string(def.InputSchema), "active_buffs") {
		t.Errorf("schema missing 'active_buffs' field: %s", def.InputSchema)
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
		CalcVersion: "calc-test",
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

func TestParseSubmitTrajectory_ActiveBuffs(t *testing.T) {
	raw := []byte(`{"primary":{"class":"taekwon_kid","snapshots":[
		{"class":"taekwon_kid","level":{"base":99,"job":50},"stats":{"str":80,"agi":90,"vit":40,"int":1,"dex":60,"luk":30},
		 "active_buffs":[{"name":"mild_wind","element":"holy"},{"name":"taekwon_ranker"}]}
	]}}`)
	in, err := ParseSubmitTrajectory(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	bs := in.Primary.Snapshots[0].ActiveBuffs
	if len(bs) != 2 || bs[0].Name != "mild_wind" || bs[0].Element != "holy" || bs[1].Name != "taekwon_ranker" {
		t.Fatalf("active_buffs not parsed: %+v", bs)
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

// TestSubmit_BadBuffOnAlternativeDropsAlt asserts invariant 1: a bad buff on
// an alternative drops only that alternative; the primary is still accepted
// and the tool returns success (no Go error).
//
// Uses the real catalog because resolveBuffs needs ClassBuffs to validate
// buff names. The alternative declares "not_a_buff" which does not exist for
// taekwon_kid, so resolveBuffs returns an error that was previously fatal.
func TestSubmit_BadBuffOnAlternativeDropsAlt(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	sc := &fakeScoringClient{resp: &scoring.ScoreResponse{
		Derived: scoring.DerivedStats{MaxHP: 5000}, CalcVersion: "calc-test",
	}}
	accepted := false
	var acceptedPrimary domain.Trajectory
	var acceptedAlts []domain.Trajectory
	tool := NewSubmitTrajectory(SubmitTrajectoryDeps{
		Catalog: cat,
		Scoring: sc,
		EvaluateGates: func(_ *scoring.ScoreResponse, _ *domain.Snapshot) []domain.GateResult {
			return []domain.GateResult{{Name: "ok", Severity: domain.GateSeverityPass}}
		},
		Accept: func(primary domain.Trajectory, alts []domain.Trajectory, _ string) bool {
			accepted = true
			acceptedPrimary = primary
			acceptedAlts = alts
			return true
		},
	})

	// Primary: valid taekwon_kid snapshot, no active_buffs (no buff resolution needed).
	primary := domain.Trajectory{
		Class: "taekwon_kid",
		Snapshots: []domain.Snapshot{{
			Class: "taekwon_kid",
			Level: domain.Level{Base: 99, Job: 50},
			Stats: domain.Stats{Str: 1, Agi: 1, Vit: 1, Int: 1, Dex: 1, Luk: 1},
		}},
	}
	// Alternative: same class/level but declares an invalid buff name that
	// resolveBuffs will reject ("not_a_buff" is not in taekwon_kid's buff list).
	badAlt := domain.Trajectory{
		Class: "taekwon_kid",
		Snapshots: []domain.Snapshot{{
			Class: "taekwon_kid",
			Level: domain.Level{Base: 99, Job: 50},
			Stats: domain.Stats{Str: 1, Agi: 1, Vit: 1, Int: 1, Dex: 1, Luk: 1},
			ActiveBuffs: []domain.ActiveBuff{
				{Name: "not_a_buff"},
			},
		}},
	}

	raw, _ := json.Marshal(SubmitTrajectoryInput{Primary: primary, Alternatives: []domain.Trajectory{badAlt}})
	out, toolErr := tool.Execute(context.Background(), raw)
	if toolErr != nil {
		t.Fatalf("Execute returned error; bad-buff alternative should be dropped, not fatal: %v", toolErr)
	}
	if !accepted {
		t.Fatal("Accept callback not invoked; primary should have been accepted")
	}
	if acceptedPrimary.Class != "taekwon_kid" {
		t.Errorf("primary class: got %q want taekwon_kid", acceptedPrimary.Class)
	}
	if len(acceptedAlts) != 0 {
		t.Errorf("bad-buff alternative should have been dropped; got %d accepted alts", len(acceptedAlts))
	}
	// The dropped alternative must be reported in the ack so the LLM can
	// see why it was discarded without aborting.
	if !strings.Contains(string(out), `"dropped_alternatives"`) {
		t.Errorf("ack missing dropped_alternatives field: %s", out)
	}
	if !strings.Contains(string(out), "not_a_buff") {
		t.Errorf("dropped_alternatives should mention the bad buff name: %s", out)
	}
}

// failOnCallScoringClient succeeds on every Score call except the one at
// index failAt (1-based), where it returns failErr. Lets a test score the
// primary cleanly and then inject a specific failure on the alternative's
// scoring call.
type failOnCallScoringClient struct {
	resp    *scoring.ScoreResponse
	failAt  int
	failErr error
	n       int
}

func (f *failOnCallScoringClient) Score(_ context.Context, _ *scoring.ScoreRequest) (*scoring.ScoreResponse, error) {
	f.n++
	if f.n == f.failAt {
		return nil, f.failErr
	}
	return f.resp, nil
}

// TestSubmit_InfraErrorOnAlternativePropagates asserts that a sidecar 5xx
// (infrastructure failure) while scoring an alternative aborts the whole
// submission rather than silently dropping the alternative. Liveness is not
// monotonic across sidecar calls: the primary scoring cleanly does not
// guarantee the alternative will, so a real outage must surface as an error,
// not be recorded as one bad alternative.
func TestSubmit_InfraErrorOnAlternativePropagates(t *testing.T) {
	sc := &failOnCallScoringClient{
		resp:    &scoring.ScoreResponse{Derived: scoring.DerivedStats{MaxHP: 5000}, CalcVersion: "calc-test"},
		failAt:  2, // call 1 = primary (clean), call 2 = alternative (5xx)
		failErr: &scoring.Error{Status: 503, Message: "sidecar restarting"},
	}
	tool := NewSubmitTrajectory(SubmitTrajectoryDeps{
		Scoring: sc,
		EvaluateGates: func(_ *scoring.ScoreResponse, _ *domain.Snapshot) []domain.GateResult {
			return []domain.GateResult{{Name: "ok", Severity: domain.GateSeverityPass}}
		},
		Accept: func(_ domain.Trajectory, _ []domain.Trajectory, _ string) bool { return true },
	})
	primary := domain.Trajectory{Class: "novice", Snapshots: []domain.Snapshot{passingSnapshot()}}
	alt := domain.Trajectory{Class: "novice", Snapshots: []domain.Snapshot{passingSnapshot()}}
	raw, _ := json.Marshal(SubmitTrajectoryInput{Primary: primary, Alternatives: []domain.Trajectory{alt}})
	if _, err := tool.Execute(context.Background(), raw); err == nil {
		t.Fatal("Execute should propagate a sidecar 5xx on an alternative; got nil (silent drop masks a real outage)")
	}
}

// TestSubmit_ClientErrorOnAlternativeDrops asserts the other half of the
// contract: a sidecar 4xx (caller-fixable input, e.g. an unmapped item id)
// on an alternative drops just that alternative and the submission still
// succeeds, exactly like a bad buff name does.
func TestSubmit_ClientErrorOnAlternativeDrops(t *testing.T) {
	sc := &failOnCallScoringClient{
		resp:    &scoring.ScoreResponse{Derived: scoring.DerivedStats{MaxHP: 5000}, CalcVersion: "calc-test"},
		failAt:  2, // alternative's scoring call returns a 4xx
		failErr: &scoring.Error{Status: 400, Message: "unmapped item id 99999"},
	}
	accepted := false
	tool := NewSubmitTrajectory(SubmitTrajectoryDeps{
		Scoring: sc,
		EvaluateGates: func(_ *scoring.ScoreResponse, _ *domain.Snapshot) []domain.GateResult {
			return []domain.GateResult{{Name: "ok", Severity: domain.GateSeverityPass}}
		},
		Accept: func(_ domain.Trajectory, _ []domain.Trajectory, _ string) bool { accepted = true; return true },
	})
	primary := domain.Trajectory{Class: "novice", Snapshots: []domain.Snapshot{passingSnapshot()}}
	alt := domain.Trajectory{Class: "novice", Snapshots: []domain.Snapshot{passingSnapshot()}}
	raw, _ := json.Marshal(SubmitTrajectoryInput{Primary: primary, Alternatives: []domain.Trajectory{alt}})
	out, err := tool.Execute(context.Background(), raw)
	if err != nil {
		t.Fatalf("a sidecar 4xx on an alternative should drop it, not abort: %v", err)
	}
	if !accepted {
		t.Fatal("primary should still be accepted when only an alternative 4xx-fails")
	}
	if !strings.Contains(string(out), `"dropped_alternatives"`) {
		t.Errorf("ack should record the dropped alternative: %s", out)
	}
}

// TestSubmit_BadBuffOnPrimaryErrors asserts invariant 2: a bad buff on the
// PRIMARY still surfaces to the LLM as a fixable error (is_error=true),
// not a silent success or a silent drop.
//
// Uses the real catalog for the same reason as the alternative test.
func TestSubmit_BadBuffOnPrimaryErrors(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("catalog.Load: %v", err)
	}

	sc := &fakeScoringClient{resp: &scoring.ScoreResponse{
		Derived: scoring.DerivedStats{MaxHP: 5000}, CalcVersion: "calc-test",
	}}
	accepted := false
	tool := NewSubmitTrajectory(SubmitTrajectoryDeps{
		Catalog: cat,
		Scoring: sc,
		EvaluateGates: func(_ *scoring.ScoreResponse, _ *domain.Snapshot) []domain.GateResult {
			return []domain.GateResult{{Name: "ok", Severity: domain.GateSeverityPass}}
		},
		Accept: func(_ domain.Trajectory, _ []domain.Trajectory, _ string) bool {
			accepted = true
			return true
		},
	})

	// Primary declares a buff whose anchor skill is NOT allocated on the
	// snapshot; resolveBuffs must return an error and the tool must surface
	// that error to the caller (LLM sees is_error=true and self-corrects).
	primary := domain.Trajectory{
		Class: "taekwon_kid",
		Snapshots: []domain.Snapshot{{
			Class:  "taekwon_kid",
			Level:  domain.Level{Base: 99, Job: 50},
			Stats:  domain.Stats{Str: 1, Agi: 1, Vit: 1, Int: 1, Dex: 1, Luk: 1},
			Skills: []domain.SkillAlloc{}, // Mild Wind (id 425) NOT allocated
			ActiveBuffs: []domain.ActiveBuff{
				{Name: "mild_wind", Element: "holy"}, // anchor skill missing -> error
			},
		}},
	}

	raw, _ := json.Marshal(SubmitTrajectoryInput{Primary: primary})
	_, toolErr := tool.Execute(context.Background(), raw)
	if toolErr == nil {
		t.Fatal("Execute should return an error when primary has a bad buff; got nil (silent success would suppress LLM self-correction)")
	}
	if accepted {
		t.Fatal("Accept callback must not fire when primary scoring fails")
	}
	// The error must mention the buff so the LLM knows what to fix.
	if !strings.Contains(toolErr.Error(), "mild_wind") {
		t.Errorf("error should mention the offending buff name; got: %v", toolErr)
	}
}

// TestFormatActiveBuffs covers the buff summary formatter directly.
func TestFormatActiveBuffs(t *testing.T) {
	cases := []struct {
		name  string
		buffs []domain.ActiveBuff
		want  string
	}{
		{
			name:  "nil buffs returns empty",
			buffs: nil,
			want:  "",
		},
		{
			name:  "empty slice returns empty",
			buffs: []domain.ActiveBuff{},
			want:  "",
		},
		{
			name:  "single buff without element",
			buffs: []domain.ActiveBuff{{Name: "taekwon_ranker"}},
			want:  "taekwon_ranker",
		},
		{
			name:  "single buff with element",
			buffs: []domain.ActiveBuff{{Name: "mild_wind", Element: "holy"}},
			want:  "mild_wind (holy endow)",
		},
		{
			name: "multiple buffs mixed",
			buffs: []domain.ActiveBuff{
				{Name: "taekwon_ranker"},
				{Name: "mild_wind", Element: "holy"},
				{Name: "spurt"},
			},
			want: "taekwon_ranker, mild_wind (holy endow), spurt",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatActiveBuffs(tc.buffs)
			if got != tc.want {
				t.Errorf("formatActiveBuffs: got %q want %q", got, tc.want)
			}
		})
	}
}

// TestBuildCheckpointReports_BuffSummary verifies that buildCheckpointReports
// populates the Buffs field from a snapshot's ActiveBuffs and that a snapshot
// with no buffs produces an empty Buffs field (no "Buffs:" noise).
func TestBuildCheckpointReports_BuffSummary(t *testing.T) {
	sc := scoring.ScoreResponse{
		Derived:     scoring.DerivedStats{StatPointsRemaining: 0},
		CalcVersion: "calc-test",
	}

	t.Run("snapshot with buffs includes buff line", func(t *testing.T) {
		traj := domain.Trajectory{
			Class: "taekwon_kid",
			Snapshots: []domain.Snapshot{{
				Class: "taekwon_kid",
				Level: domain.Level{Base: 99, Job: 50},
				Stats: domain.Stats{Str: 1, Agi: 1, Vit: 1, Int: 1, Dex: 1, Luk: 1},
				ActiveBuffs: []domain.ActiveBuff{
					{Name: "taekwon_ranker"},
					{Name: "mild_wind", Element: "holy"},
				},
				Score: &sc,
			}},
		}
		reports := buildCheckpointReports(&traj)
		if len(reports) != 1 {
			t.Fatalf("expected 1 report, got %d", len(reports))
		}
		r := reports[0]
		if !strings.Contains(r.Buffs, "taekwon_ranker") {
			t.Errorf("Buffs missing taekwon_ranker: %q", r.Buffs)
		}
		if !strings.Contains(r.Buffs, "mild_wind") {
			t.Errorf("Buffs missing mild_wind: %q", r.Buffs)
		}
		if !strings.Contains(r.Buffs, "holy") {
			t.Errorf("Buffs missing holy endow annotation: %q", r.Buffs)
		}
	})

	t.Run("snapshot with no buffs produces empty Buffs field", func(t *testing.T) {
		traj := domain.Trajectory{
			Class: "novice",
			Snapshots: []domain.Snapshot{{
				Class: "novice",
				Level: domain.Level{Base: 99, Job: 50},
				Stats: domain.Stats{Str: 1, Agi: 1, Vit: 1, Int: 1, Dex: 1, Luk: 1},
				Score: &sc,
			}},
		}
		reports := buildCheckpointReports(&traj)
		if len(reports) != 1 {
			t.Fatalf("expected 1 report, got %d", len(reports))
		}
		if reports[0].Buffs != "" {
			t.Errorf("expected empty Buffs for snapshot with no active_buffs; got %q", reports[0].Buffs)
		}
	})
}

// TestFormatCheckpointLines_BuffSummary verifies that the human-readable
// rejection block includes the buff line when buffs are present and omits
// the field when there are none.
func TestFormatCheckpointLines_BuffSummary(t *testing.T) {
	t.Run("buffs appear in prose output", func(t *testing.T) {
		reports := []checkpointReport{{
			SnapshotIndex:       0,
			Class:               "taekwon_kid",
			Level:               "99/50",
			StatPointsRemaining: 0,
			Buffs:               "taekwon_ranker, mild_wind (holy endow), spurt",
		}}
		out := formatCheckpointLines(reports)
		if !strings.Contains(out, "taekwon_ranker") {
			t.Errorf("prose missing taekwon_ranker: %s", out)
		}
		if !strings.Contains(out, "mild_wind") {
			t.Errorf("prose missing mild_wind: %s", out)
		}
		if !strings.Contains(out, "holy") {
			t.Errorf("prose missing holy endow annotation: %s", out)
		}
		if !strings.Contains(out, "spurt") {
			t.Errorf("prose missing spurt: %s", out)
		}
	})

	t.Run("no buffs produces no Buffs: line", func(t *testing.T) {
		reports := []checkpointReport{{
			SnapshotIndex:       0,
			Class:               "novice",
			Level:               "99/50",
			StatPointsRemaining: 3,
		}}
		out := formatCheckpointLines(reports)
		if strings.Contains(out, "buffs=") {
			t.Errorf("prose should not contain buffs= when no buffs active; got: %s", out)
		}
	})
}
