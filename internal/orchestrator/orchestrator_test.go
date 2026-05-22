package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/llm"
	"github.com/levonn-dev/ro-builder/internal/llm/tools"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// fakeProvider returns canned responses in sequence; lets us drive the
// orchestrator's loop deterministically without a live LLM.
type fakeProvider struct {
	responses []llm.Response
	calls     []callCapture
}

type callCapture struct {
	system   string
	messages []llm.Message
	tools    []llm.Tool
}

func (f *fakeProvider) Complete(_ context.Context, system string, messages []llm.Message, t []llm.Tool) (llm.Response, error) {
	f.calls = append(f.calls, callCapture{system: system, messages: append([]llm.Message{}, messages...), tools: t})
	if len(f.responses) == 0 {
		return llm.Response{}, errors.New("fakeProvider: no more canned responses")
	}
	r := f.responses[0]
	f.responses = f.responses[1:]
	return r, nil
}

// fakeTool returns a fixed JSON payload, recording its inputs and the
// context it received (so tests can verify orchestrator-stamped values
// reach the dispatch site).
type fakeTool struct {
	name    string
	output  json.RawMessage
	err     error
	inputs  []json.RawMessage
	lastCtx context.Context
}

func (s *fakeTool) Definition() llm.Tool {
	return llm.Tool{Name: s.name, Description: "fake", InputSchema: json.RawMessage(`{"type":"object"}`)}
}
func (s *fakeTool) Execute(ctx context.Context, in json.RawMessage) (json.RawMessage, error) {
	s.inputs = append(s.inputs, in)
	s.lastCtx = ctx
	return s.output, s.err
}

// fakeScoringClient returns canned ScoreResponse values per call. Each
// call advances the index; tests assert call count to verify which
// snapshots the canonical loop scored.
type fakeScoringClient struct {
	responses []*scoring.ScoreResponse
	err       error
	calls     []*scoring.ScoreRequest
}

func (f *fakeScoringClient) Score(_ context.Context, req *scoring.ScoreRequest) (*scoring.ScoreResponse, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return nil, f.err
	}
	if len(f.responses) == 0 {
		return &scoring.ScoreResponse{}, nil
	}
	r := f.responses[0]
	f.responses = f.responses[1:]
	return r, nil
}

func TestOrchestrator_SingleTurn_NoTools(t *testing.T) {
	prov := &fakeProvider{responses: []llm.Response{
		{
			StopReason: llm.StopEndTurn,
			Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "Here is your build: ..."}},
		},
	}}
	o := New(prov, tools.NewRegistry())
	// No submission → FailureError, but the returned partial result
	// still carries the final prose and the trace.
	res, err := o.Generate(context.Background(), GenerateRequest{Class: "taekwon_kid", Mode: "pre-renewal"})
	var fe *FailureError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *FailureError, got %T: %v", err, err)
	}
	if fe.Reason != "no_submission" {
		t.Fatalf("Reason: got %q, want no_submission", fe.Reason)
	}
	if res == nil {
		t.Fatal("expected partial GenerateResult even on failure")
	}
	if res.Iters != 1 {
		t.Errorf("iters: got %d want 1", res.Iters)
	}
	if !strings.Contains(res.Final, "Here is your build") {
		t.Errorf("final: got %q", res.Final)
	}
	// System prompt + user message arrived as expected.
	if !strings.Contains(prov.calls[0].system, "build optimizer") {
		t.Errorf("system: got %q", prov.calls[0].system)
	}
	if len(prov.calls[0].messages) != 1 {
		t.Errorf("messages count: got %d", len(prov.calls[0].messages))
	}
}

// Tool-use loop with an arbitrary tool registered in the shared registry;
// submit_trajectory isn't called, so the loop exits with no_submission.
// The point of this test is to exercise the dispatch path: the tool gets
// invoked, its output reaches the next assistant turn.
func TestOrchestrator_ToolUseLoop(t *testing.T) {
	tool := &fakeTool{
		name:   "score_build",
		output: json.RawMessage(`{"derived":{"hit":71}}`),
	}
	reg := tools.NewRegistry()
	reg.Register(tool)

	prov := &fakeProvider{responses: []llm.Response{
		// Turn 1: model calls score_build
		{
			StopReason: llm.StopToolUse,
			Content: []llm.ContentBlock{
				{Type: llm.BlockText, Text: "Let me check this build."},
				{Type: llm.BlockToolUse, ToolUseID: "toolu_1", ToolName: "score_build", ToolInput: json.RawMessage(`{"build":{"class":"novice"}}`)},
			},
		},
		// Turn 2: model produces final answer (no submission)
		{
			StopReason: llm.StopEndTurn,
			Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "Build verified: HIT=71."}},
		},
	}}
	o := New(prov, reg)
	res, err := o.Generate(context.Background(), GenerateRequest{Class: "novice"})
	var fe *FailureError
	if !errors.As(err, &fe) || fe.Reason != "no_submission" {
		t.Fatalf("expected no_submission FailureError, got %v", err)
	}
	if res.Iters != 2 {
		t.Errorf("iters: got %d want 2", res.Iters)
	}
	if len(tool.inputs) != 1 {
		t.Errorf("tool calls: got %d want 1", len(tool.inputs))
	}
	if !strings.Contains(res.Final, "HIT=71") {
		t.Errorf("final: got %q", res.Final)
	}
	// Trace should contain user, assistant (with tool_use), user
	// (with tool_result), assistant (final); 4 messages.
	if len(res.Trace) != 4 {
		t.Errorf("trace length: got %d want 4", len(res.Trace))
	}
}

func TestOrchestrator_ToolErrorBecomesIsErrorBlock(t *testing.T) {
	tool := &fakeTool{
		name: "score_build",
		err:  errors.New("iRO mob id 9999999 is unmapped"),
	}
	reg := tools.NewRegistry()
	reg.Register(tool)

	prov := &fakeProvider{responses: []llm.Response{
		{
			StopReason: llm.StopToolUse,
			Content: []llm.ContentBlock{
				{Type: llm.BlockToolUse, ToolUseID: "toolu_1", ToolName: "score_build", ToolInput: json.RawMessage(`{}`)},
			},
		},
		{
			StopReason: llm.StopEndTurn,
			Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "I'll fix the mob id."}},
		},
	}}
	o := New(prov, reg)
	res, err := o.Generate(context.Background(), GenerateRequest{Class: "novice"})
	var fe *FailureError
	if !errors.As(err, &fe) || fe.Reason != "no_submission" {
		t.Fatalf("expected no_submission FailureError, got %v", err)
	}

	// Inspect turn 2's user message; should contain a tool_result with
	// is_error=true and the error text in the body.
	turn2User := prov.calls[1].messages[len(prov.calls[1].messages)-1]
	if turn2User.Role != llm.RoleUser {
		t.Fatalf("expected user role for turn 2 last message, got %q", turn2User.Role)
	}
	if len(turn2User.Content) != 1 || turn2User.Content[0].Type != llm.BlockToolResult {
		t.Fatalf("expected single tool_result block, got %+v", turn2User.Content)
	}
	if !turn2User.Content[0].IsError {
		t.Error("tool_result should have IsError=true")
	}
	if !strings.Contains(string(turn2User.Content[0].ToolResult), "9999999") {
		t.Errorf("tool_result body should mention the bad id: got %s", turn2User.Content[0].ToolResult)
	}
	if !strings.Contains(res.Final, "fix the mob id") {
		t.Errorf("final: got %q", res.Final)
	}
}

// Loop exhaustion with no accepted submission must surface as a
// FailureError carrying max_iters_exhausted so the worker pool can
// record the right failure_reason on the generations row.
func TestGenerate_MaxItersExhaustedEndsAsFailure(t *testing.T) {
	tool := &fakeTool{name: "score_build", output: json.RawMessage(`{}`)}
	reg := tools.NewRegistry()
	reg.Register(tool)

	// Always returns tool_use → loop never ends, never submits.
	loopForever := llm.Response{
		StopReason: llm.StopToolUse,
		Content:    []llm.ContentBlock{{Type: llm.BlockToolUse, ToolUseID: "t", ToolName: "score_build", ToolInput: json.RawMessage(`{}`)}},
	}
	prov := &fakeProvider{responses: []llm.Response{loopForever, loopForever, loopForever, loopForever}}

	o := New(prov, reg).WithMaxIters(3)
	_, err := o.Generate(context.Background(), GenerateRequest{Class: "novice"})
	var fe *FailureError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *FailureError, got %T: %v", err, err)
	}
	if fe.Reason != "max_iters_exhausted" {
		t.Fatalf("Reason: got %q, want max_iters_exhausted", fe.Reason)
	}
}

// Provider returning an authentication error is bucketed as
// provider_auth_error so the worker can decide whether to retry.
func TestGenerate_ProviderAuthErrorClassified(t *testing.T) {
	prov := &authFailProvider{}
	o := New(prov, tools.NewRegistry())
	_, err := o.Generate(context.Background(), GenerateRequest{Class: "novice"})
	var fe *FailureError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *FailureError, got %T: %v", err, err)
	}
	if fe.Reason != "provider_auth_error" {
		t.Errorf("Reason: got %q, want provider_auth_error", fe.Reason)
	}
}

// Anthropic occasionally adds new stop_reason values to its API
// (pause_turn for extended thinking, refusal for safety, etc.). The
// orchestrator should fail with validation_error_late rather than
// hanging or silently mishandling them; this guards the default case
// in the loop's switch.
func TestOrchestrator_UnknownStopReasonReturnsError(t *testing.T) {
	prov := &fakeProvider{responses: []llm.Response{
		{
			StopReason: llm.StopReason("future_value"),
			Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "..."}},
		},
	}}
	o := New(prov, tools.NewRegistry())
	_, err := o.Generate(context.Background(), GenerateRequest{Class: "novice"})
	var fe *FailureError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *FailureError, got %T: %v", err, err)
	}
	if fe.Reason != "validation_error_late" {
		t.Errorf("Reason: got %q, want validation_error_late", fe.Reason)
	}
	if !strings.Contains(fe.Detail, "future_value") {
		t.Errorf("Detail should name the unrecognized stop_reason: got %q", fe.Detail)
	}
}

// Provider hitting max_tokens means the response was truncated mid-turn;
// surface as a typed failure so the worker can flag it for retry.
func TestGenerate_ProviderMaxTokensFailure(t *testing.T) {
	prov := &fakeProvider{responses: []llm.Response{
		{
			StopReason: llm.StopMaxTokens,
			Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "I was just getting to ..."}},
		},
	}}
	o := New(prov, tools.NewRegistry())
	_, err := o.Generate(context.Background(), GenerateRequest{Class: "novice"})
	var fe *FailureError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *FailureError, got %T: %v", err, err)
	}
	if fe.Reason != "provider_max_tokens" {
		t.Errorf("Reason: got %q, want provider_max_tokens", fe.Reason)
	}
}

// When a profile is configured for the requested server, the user message
// includes a profile context block; that's how the LLM sees authoritative
// server rules instead of guessing.
func TestOrchestrator_ServerProfileInjectedIntoPrompt(t *testing.T) {
	prov := &fakeProvider{responses: []llm.Response{{
		StopReason: llm.StopEndTurn,
		Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "ok"}},
	}}}

	profile := &domain.ServerProfile{
		Key:  "uaro",
		Name: "uaRO",
		Mode: domain.PreRenewal,
		Caps: domain.Caps{MaxBaseLevel: 99, MaxStat: 99, InstantCastDex: 150},
		ClassChanges: []domain.ClassChange{
			{Class: "taekwon_kid", Rules: []string{"Taekwon Mission rebalanced"}},
			{Class: "knight", Rules: []string{"Bowling Bash doubled"}},
		},
	}
	o := New(prov, tools.NewRegistry()).
		WithProfiles(map[string]*domain.ServerProfile{"uaro": profile})

	// no_submission FailureError is expected here; we don't care, the
	// assertion is on the user prompt the provider received.
	_, _ = o.Generate(context.Background(), GenerateRequest{
		Class: "taekwon_kid", Server: "uaro",
	})

	got := prov.calls[0].messages[0].Content[0].Text
	if !strings.Contains(got, "Server profile (uaRO)") {
		t.Errorf("user prompt should embed profile header; got:\n%s", got)
	}
	if !strings.Contains(got, "Taekwon Mission rebalanced") {
		t.Errorf("user prompt should embed taekwon_kid rules; got:\n%s", got)
	}
	if strings.Contains(got, "Bowling Bash") {
		t.Errorf("knight rules should be filtered out when class=taekwon_kid; got:\n%s", got)
	}
}

// Unknown server keys are a caller bug; surface them as ValidationError
// rather than silently dropping the context the caller expected.
func TestOrchestrator_UnknownServerWithProfilesConfigured(t *testing.T) {
	o := New(&fakeProvider{}, tools.NewRegistry()).
		WithProfiles(map[string]*domain.ServerProfile{
			"uaro": {Key: "uaro", Mode: domain.PreRenewal},
		})

	_, err := o.Generate(context.Background(), GenerateRequest{
		Class: "taekwon_kid", Server: "freebro",
	})
	if err == nil {
		t.Fatal("expected ValidationError for unknown server")
	}
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Field != "server" {
		t.Errorf("expected ValidationError(field=server); got %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "freebro") {
		t.Errorf("error should name the bad key: %v", err)
	}
}

// Orchestrator stamps the active server profile on the context passed
// into tool dispatch so overlay-aware tools (lookup_monster, score_build)
// can apply the right server's overlays. Verifying the plumbing here
// avoids the gap-B "tool sees nil profile" regression.
func TestOrchestrator_StampsProfileOnDispatchContext(t *testing.T) {
	tool := &fakeTool{
		name:   "score_build",
		output: json.RawMessage(`{}`),
	}
	reg := tools.NewRegistry()
	reg.Register(tool)

	prov := &fakeProvider{responses: []llm.Response{
		{
			StopReason: llm.StopToolUse,
			Content: []llm.ContentBlock{
				{Type: llm.BlockToolUse, ToolUseID: "t1", ToolName: "score_build", ToolInput: json.RawMessage(`{}`)},
			},
		},
		{
			StopReason: llm.StopEndTurn,
			Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "done"}},
		},
	}}

	profile := &domain.ServerProfile{Key: "uaro", Mode: domain.PreRenewal}
	o := New(prov, reg).
		WithProfiles(map[string]*domain.ServerProfile{"uaro": profile})
	// no_submission FailureError expected; we only care about the
	// dispatch-side context plumbing.
	_, _ = o.Generate(context.Background(), GenerateRequest{Class: "novice", Server: "uaro"})

	got := domain.ServerProfileFromContext(tool.lastCtx)
	if got == nil {
		t.Fatal("expected stamped profile in dispatch ctx, got nil")
	}
	if got.Key != "uaro" {
		t.Errorf("stamped profile: got key %q want uaro", got.Key)
	}
}

// End-to-end submit-and-accept flow: the LLM calls submit_trajectory,
// the per-request tool runs canonical scoring + the Accept callback,
// the session captures the accepted submission, the loop's end_turn
// step returns success with Primary populated.
func TestOrchestrator_SubmitTrajectory_AcceptedFlowsToResult(t *testing.T) {
	submission := json.RawMessage(`{
		"primary": {
			"class": "novice",
			"snapshots": [
				{"class":"novice","level":{"base":1,"job":1},"stats":{"str":1,"agi":1,"vit":1,"int":1,"dex":1,"luk":1}}
			]
		},
		"alternatives": [
			{"class":"novice","snapshots":[
				{"class":"novice","level":{"base":1,"job":1},"stats":{"str":1,"agi":1,"vit":1,"int":1,"dex":1,"luk":1}}
			]}
		]
	}`)
	prov := &fakeProvider{responses: []llm.Response{
		// Turn 1: model calls submit_trajectory
		{
			StopReason: llm.StopToolUse,
			Content: []llm.ContentBlock{
				{Type: llm.BlockToolUse, ToolUseID: "t1", ToolName: tools.SubmitTrajectoryToolName, ToolInput: submission},
			},
		},
		// Turn 2: model wraps up with prose summary
		{
			StopReason: llm.StopEndTurn,
			Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "Submitted: novice training trajectory."}},
		},
	}}

	// Shared registry has no submit_trajectory; the orchestrator's
	// per-request WithTool overlay supplies one wired with the session
	// Accept callback + the fake scoring client.
	reg := tools.NewRegistry()

	canned := &scoring.ScoreResponse{Derived: scoring.DerivedStats{Hit: 42}}
	scoringFake := &fakeScoringClient{responses: []*scoring.ScoreResponse{canned, canned}}

	o := New(prov, reg).WithScoringClient(scoringFake)
	res, err := o.Generate(context.Background(), GenerateRequest{Class: "novice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Primary == nil {
		t.Fatal("expected Primary populated after submit_trajectory")
	}
	if res.Primary.Class != "novice" {
		t.Errorf("Primary.Class: got %q want %q", res.Primary.Class, "novice")
	}
	// Single-snapshot trajectory → endgame is the only scored index.
	if res.Primary.Snapshots[0].Score == nil {
		t.Errorf("expected canonical scoring to stamp Score on the endgame snapshot")
	}
	if res.Primary.Snapshots[0].Score.Derived.Hit != 42 {
		t.Errorf("score.derived.hit: got %d want 42", res.Primary.Snapshots[0].Score.Derived.Hit)
	}
	if len(res.Alternatives) != 1 {
		t.Errorf("alternatives: got %d want 1", len(res.Alternatives))
	}
	if res.Alternatives[0].Snapshots[0].Score == nil {
		t.Errorf("alternative trajectory should also be canonically scored")
	}
	if !strings.Contains(res.Final, "Submitted") {
		t.Errorf("Final missing prose summary: got %q", res.Final)
	}
}

// LLM ends the turn without a passing submit_trajectory; return
// FailureError{Reason:"no_submission"} so the worker pool records that
// as the failure_reason. Final still carries the assistant prose so
// debug tooling can render the trace.
func TestGenerate_NoSubmissionEndsAsFailure(t *testing.T) {
	prov := &fakeProvider{
		responses: []llm.Response{
			{StopReason: llm.StopEndTurn, Content: []llm.ContentBlock{{Type: llm.BlockText, Text: "I'm not sure how to build this."}}},
		},
	}
	o := New(prov, tools.NewRegistry())
	res, err := o.Generate(context.Background(), GenerateRequest{Class: "novice"})
	var fe *FailureError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *FailureError, got %T: %v", err, err)
	}
	if fe.Reason != "no_submission" {
		t.Fatalf("Reason: got %q, want no_submission", fe.Reason)
	}
	if res.Primary != nil {
		t.Errorf("expected Primary=nil when LLM doesn't submit, got %+v", res.Primary)
	}
	if res.Final == "" {
		t.Error("Final should still carry the assistant prose")
	}
}

// Empty profiles map (test default) keeps the orchestrator permissive;
// any Server value passes through, no profile injection happens. Preserves
// the pre-server-profile test surface for fixtures that don't care.
func TestOrchestrator_NoProfilesConfigured_ServerIgnored(t *testing.T) {
	prov := &fakeProvider{responses: []llm.Response{{
		StopReason: llm.StopEndTurn,
		Content:    []llm.ContentBlock{{Type: llm.BlockText, Text: "ok"}},
	}}}
	o := New(prov, tools.NewRegistry())

	// no_submission FailureError expected; we only care about the prompt.
	_, _ = o.Generate(context.Background(), GenerateRequest{
		Class: "taekwon_kid", Server: "anything",
	})
	got := prov.calls[0].messages[0].Content[0].Text
	if strings.Contains(got, "Server profile") {
		t.Errorf("no profile injection expected when profiles map empty; got:\n%s", got)
	}
}

func TestOrchestrator_RequestValidation(t *testing.T) {
	cases := []struct {
		name string
		req  GenerateRequest
		want string
	}{
		{name: "renewal rejected", req: GenerateRequest{Class: "novice", Mode: "renewal"}, want: "not supported"},
		{name: "unknown mode", req: GenerateRequest{Class: "novice", Mode: "kRO"}, want: "unknown mode"},
		{name: "unknown playstyle", req: GenerateRequest{Class: "novice", Playstyle: "afk"}, want: "unknown playstyle"},
		// Note: class names aren't validated Go-side anymore; the shim is
		// the source of truth. An unknown name surfaces from the shim's
		// 4xx response when score_build is first called. The orchestrator
		// happily passes "ranger" or "-1" through to the model, which will
		// see the error in tool_result and self-correct.
	}
	o := New(&fakeProvider{}, tools.NewRegistry())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := o.Generate(context.Background(), tc.req)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error: got %q want substring %q", err.Error(), tc.want)
			}
			// Boundary-level rejections are ValidationError, not
			// FailureError; they hit before the loop runs.
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Errorf("expected *ValidationError, got %T: %v", err, err)
			}
		})
	}
}

// ValidateRequest is the public-facing pre-flight the API handler runs
// before enqueueing a job. Must agree with Generate's internal
// validateRequest for the boundary checks.
func TestValidateRequest_ExportedMatchesInternal(t *testing.T) {
	// Same cases the internal test covers; Mode/playstyle/description.
	cases := []GenerateRequest{
		{Class: "novice", Mode: "renewal"},
		{Class: "novice", Mode: "kRO"},
		{Class: "novice", Playstyle: "afk"},
	}
	for _, tc := range cases {
		if err := ValidateRequest(tc); err == nil {
			t.Errorf("ValidateRequest(%+v) should fail", tc)
		}
	}
	// And the happy path.
	if err := ValidateRequest(GenerateRequest{Class: "novice", Mode: "pre-renewal", Playstyle: "pvm"}); err != nil {
		t.Errorf("ValidateRequest(happy): %v", err)
	}
}

// ValidateRequestWithProfiles short-circuits unknown server keys at the
// boundary so the API handler returns 400 instead of 202+async failure.
func TestValidateRequestWithProfiles_UnknownServerRejected(t *testing.T) {
	profiles := map[string]*domain.ServerProfile{
		"uaro": {Key: "uaro", Mode: domain.PreRenewal},
	}
	err := ValidateRequestWithProfiles(
		GenerateRequest{Class: "novice", Server: "freebro", Mode: "pre-renewal"},
		profiles,
	)
	if err == nil {
		t.Fatal("expected ValidationError for unknown server")
	}
	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Field != "server" {
		t.Errorf("expected ValidationError(field=server); got %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "freebro") {
		t.Errorf("error should name the bad key: %v", err)
	}
}

// Known server keys pass through cleanly even when the registry is set.
func TestValidateRequestWithProfiles_KnownServerAccepted(t *testing.T) {
	profiles := map[string]*domain.ServerProfile{
		"uaro": {Key: "uaro", Mode: domain.PreRenewal},
	}
	err := ValidateRequestWithProfiles(
		GenerateRequest{Class: "novice", Server: "uaro", Mode: "pre-renewal"},
		profiles,
	)
	if err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// Empty profiles map is permissive; any server key passes.
func TestValidateRequestWithProfiles_EmptyMapPermissive(t *testing.T) {
	if err := ValidateRequestWithProfiles(
		GenerateRequest{Class: "novice", Server: "anything", Mode: "pre-renewal"},
		nil,
	); err != nil {
		t.Errorf("expected nil with empty profiles; got %v", err)
	}
}

// authFailProvider returns an error that implements IsAuthError() bool,
// matching the contract llm.IsAuthError checks for. Lets us drive the
// classifyProviderError branch without wiring a real Anthropic client.
// TestFormatUserPrompt verifies that the new skill-point budget, skill
// prerequisite annotations, and ranker note are rendered correctly. Uses
// formatUserPrompt directly to avoid spinning up a provider.
func TestFormatUserPrompt(t *testing.T) {
	skills := []catalog.ClassSkill{
		{ID: 264, AegisName: "TK_STORMKICK", Name: "Whirlwind Kick", MaxLevel: 7},
		{
			ID:        265,
			AegisName: "TK_READYSTORM",
			Name:      "Tornado Stance",
			MaxLevel:  1,
			Requires:  []catalog.ClassSkillRequirement{{AegisName: "TK_STORMKICK", Level: 1}},
		},
		{ID: 270, AegisName: "TK_MISSION", Name: "Taekwon Mission", MaxLevel: 1,
			Requires: []catalog.ClassSkillRequirement{{AegisName: "TK_POWER", Level: 5}},
		},
	}

	t.Run("budget_line_appears", func(t *testing.T) {
		got := formatUserPrompt(
			GenerateRequest{Class: "taekwon_kid", Mode: "pre-renewal"},
			nil, skills, 99, 50, 58)
		if !strings.Contains(got, "Endgame skill point budget: 58") {
			t.Errorf("budget line missing; got:\n%s", got)
		}
	})

	t.Run("prereq_annotation_on_ready_stance", func(t *testing.T) {
		got := formatUserPrompt(
			GenerateRequest{Class: "taekwon_kid"},
			nil, skills, 99, 50, 58)
		if !strings.Contains(got, "requires: TK_STORMKICK lv1") {
			t.Errorf("prereq annotation missing for TK_READYSTORM; got:\n%s", got)
		}
	})

	t.Run("no_prereq_on_plain_skill", func(t *testing.T) {
		got := formatUserPrompt(
			GenerateRequest{Class: "taekwon_kid"},
			nil, skills, 0, 0, 0)
		// TK_STORMKICK has no prereqs; its line must not have a "requires:" token.
		for _, line := range strings.Split(got, "\n") {
			if strings.Contains(line, "TK_STORMKICK") && !strings.Contains(line, "TK_READYSTORM") {
				if strings.Contains(line, "requires:") {
					t.Errorf("TK_STORMKICK line should have no requires annotation: %s", line)
				}
			}
		}
	})

	t.Run("taekwon_kid_options_block_always_injected", func(t *testing.T) {
		// The block is unconditional for taekwon_kid; the LLM decides
		// from the description whether Ranker intent applies.
		got := formatUserPrompt(
			GenerateRequest{Class: "taekwon_kid", Description: "I want to become a Ranker"},
			nil, skills, 99, 50, 58)
		if !strings.Contains(got, "TK_MISSION") {
			t.Errorf("options block should mention TK_MISSION; got:\n%s", got)
		}
		if !strings.Contains(got, "Taekwon Kid options") {
			t.Errorf("options block header missing; got:\n%s", got)
		}
	})

	t.Run("taekwon_kid_options_block_present_even_without_rank_intent", func(t *testing.T) {
		// Description doesn't suggest Ranker; block still appears, but
		// the LLM will read the block and choose not to allocate.
		got := formatUserPrompt(
			GenerateRequest{Class: "taekwon_kid", Description: "pure farming build"},
			nil, skills, 99, 50, 58)
		if !strings.Contains(got, "Taekwon Kid options") {
			t.Errorf("options block should be unconditional for taekwon_kid; got:\n%s", got)
		}
	})

	t.Run("options_block_not_injected_for_non_taekwon_kid", func(t *testing.T) {
		// Only the Kid form is eligible for Ranker. SG/SL/other classes
		// must NOT see the block; they're either post-Ranker (SG/SL)
		// or unrelated entirely.
		for _, c := range []string{"knight", "star_gladiator", "soul_linker"} {
			got := formatUserPrompt(
				GenerateRequest{Class: c, Description: "I want to reach high ranking"},
				nil, skills, 99, 70, 69)
			if strings.Contains(got, "Taekwon Kid options") {
				t.Errorf("options block should not appear for class %q; got:\n%s", c, got)
			}
		}
	})

	t.Run("zero_budget_omits_budget_line", func(t *testing.T) {
		got := formatUserPrompt(
			GenerateRequest{Class: "taekwon_kid"},
			nil, nil, 0, 0, 0)
		if strings.Contains(got, "skill point budget") {
			t.Errorf("budget line should be absent when budget=0; got:\n%s", got)
		}
	})
}

type authFailProvider struct{}

func (a *authFailProvider) Complete(_ context.Context, _ string, _ []llm.Message, _ []llm.Tool) (llm.Response, error) {
	return llm.Response{}, &authErr{}
}

type authErr struct{}

func (e *authErr) Error() string     { return "auth failed: bad API key" }
func (e *authErr) IsAuthError() bool { return true }
