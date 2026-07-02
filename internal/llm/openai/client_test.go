package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/llm"
)

func fakeOpenAI(t *testing.T, status int, respBody string) (string, *map[string]any, *http.Header) {
	t.Helper()
	var captured map[string]any
	var capturedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("content-type", "application/json")
		if status != http.StatusOK {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &captured, &capturedHeaders
}

func TestClient_Complete_SendsCorrectWireShape(t *testing.T) {
	url, captured, headers := fakeOpenAI(t, http.StatusOK, `{
		"id":"cmpl-1","model":"gpt-4o-mini",
		"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"OK"}}]
	}`)

	c := NewClient("sk-test", WithBaseURL(url), WithModel("gpt-4o-mini"))
	resp, err := c.Complete(context.Background(), "be helpful", []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.BlockText, Text: "hi"}}},
	}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if got := headers.Get("authorization"); got != "Bearer sk-test" {
		t.Errorf("authorization: got %q", got)
	}
	if (*captured)["model"] != "gpt-4o-mini" {
		t.Errorf("model: got %v", (*captured)["model"])
	}
	msgs := (*captured)["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages: got %d want 2 (system + user)", len(msgs))
	}
	if msgs[0].(map[string]any)["role"] != "system" {
		t.Errorf("msg[0].role: %v", msgs[0])
	}
	if msgs[1].(map[string]any)["role"] != "user" {
		t.Errorf("msg[1].role: %v", msgs[1])
	}

	if resp.StopReason != llm.StopEndTurn {
		t.Errorf("stop_reason: got %q", resp.StopReason)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "OK" {
		t.Errorf("content: %+v", resp.Content)
	}
}

func TestClient_Complete_LocalServerNoAuthHeader(t *testing.T) {
	// LM Studio / llama.cpp don't require an API key; verify we don't send
	// the Authorization header when APIKey is empty.
	url, _, headers := fakeOpenAI(t, http.StatusOK, `{
		"id":"x","model":"local","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hi"}}]
	}`)
	c := NewClient("", WithBaseURL(url))
	_, err := c.Complete(context.Background(), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := headers.Get("authorization"); got != "" {
		t.Errorf("authorization should be empty for local server, got %q", got)
	}
}

func TestClient_Complete_DecodesToolUse(t *testing.T) {
	url, _, _ := fakeOpenAI(t, http.StatusOK, `{
		"id":"x","model":"m",
		"choices":[{"index":0,"finish_reason":"tool_calls","message":{
			"role":"assistant","content":null,
			"tool_calls":[{"id":"call_01","type":"function","function":{"name":"score_build","arguments":"{\"build\":{\"class\":\"novice\"}}"}}]
		}}]
	}`)

	c := NewClient("k", WithBaseURL(url))
	resp, err := c.Complete(context.Background(), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != llm.StopToolUse {
		t.Errorf("stop: got %q", resp.StopReason)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("content blocks: got %d want 1", len(resp.Content))
	}
	if resp.Content[0].Type != llm.BlockToolUse || resp.Content[0].ToolName != "score_build" {
		t.Errorf("block: %+v", resp.Content[0])
	}
	if resp.Content[0].ToolUseID != "call_01" {
		t.Errorf("tool_use_id: got %q", resp.Content[0].ToolUseID)
	}
	if !strings.Contains(string(resp.Content[0].ToolInput), `"novice"`) {
		t.Errorf("tool input: %s", string(resp.Content[0].ToolInput))
	}
}

func TestClient_Complete_SerializesToolResultAsToolMessage(t *testing.T) {
	url, captured, _ := fakeOpenAI(t, http.StatusOK, `{
		"id":"x","model":"m","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"done"}}]
	}`)

	c := NewClient("k", WithBaseURL(url))
	_, err := c.Complete(context.Background(), "", []llm.Message{
		{
			Role: llm.RoleUser,
			Content: []llm.ContentBlock{{
				Type:       llm.BlockToolResult,
				ToolUseID:  "call_01",
				ToolResult: json.RawMessage(`{"derived":{"hit":2}}`),
			}},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	msgs := (*captured)["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages: got %d want 1 (tool-only)", len(msgs))
	}
	m := msgs[0].(map[string]any)
	if m["role"] != "tool" {
		t.Errorf("role: %v", m["role"])
	}
	if m["tool_call_id"] != "call_01" {
		t.Errorf("tool_call_id: %v", m["tool_call_id"])
	}
	if !strings.Contains(m["content"].(string), `"derived"`) {
		t.Errorf("content: %v", m["content"])
	}
}

func TestClient_Complete_AssistantMergesTextAndToolCalls(t *testing.T) {
	url, captured, _ := fakeOpenAI(t, http.StatusOK, `{
		"id":"x","model":"m","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]
	}`)

	c := NewClient("k", WithBaseURL(url))
	_, err := c.Complete(context.Background(), "", []llm.Message{
		{
			Role: llm.RoleAssistant,
			Content: []llm.ContentBlock{
				{Type: llm.BlockText, Text: "Let me check."},
				{Type: llm.BlockToolUse, ToolUseID: "call_01", ToolName: "lookup_item", ToolInput: json.RawMessage(`{"id":1201}`)},
			},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	msgs := (*captured)["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages: got %d want 1", len(msgs))
	}
	m := msgs[0].(map[string]any)
	if m["content"] != "Let me check." {
		t.Errorf("content: %v", m["content"])
	}
	tcs, ok := m["tool_calls"].([]any)
	if !ok || len(tcs) != 1 {
		t.Fatalf("tool_calls: %v", m["tool_calls"])
	}
	tc := tcs[0].(map[string]any)
	fn := tc["function"].(map[string]any)
	if fn["name"] != "lookup_item" {
		t.Errorf("function.name: %v", fn["name"])
	}
	if !strings.Contains(fn["arguments"].(string), `"id":1201`) {
		t.Errorf("function.arguments: %v", fn["arguments"])
	}
}

func TestClient_Complete_ToolsTranslated(t *testing.T) {
	url, captured, _ := fakeOpenAI(t, http.StatusOK, `{
		"id":"x","model":"m","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]
	}`)

	c := NewClient("k", WithBaseURL(url))
	_, err := c.Complete(context.Background(), "", nil, []llm.Tool{
		{Name: "lookup_item", Description: "look up", InputSchema: json.RawMessage(`{"type":"object"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	tools := (*captured)["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools: %v", tools)
	}
	t0 := tools[0].(map[string]any)
	if t0["type"] != "function" {
		t.Errorf("tool type: %v", t0["type"])
	}
	fn := t0["function"].(map[string]any)
	if fn["name"] != "lookup_item" {
		t.Errorf("function.name: %v", fn["name"])
	}
	params := fn["parameters"].(map[string]any)
	if params["type"] != "object" {
		t.Errorf("parameters: %v", params)
	}
}

func TestClient_Complete_ErrorEnvelope(t *testing.T) {
	url, _, _ := fakeOpenAI(t, http.StatusUnauthorized, `{
		"error":{"type":"invalid_request_error","code":"invalid_api_key","message":"bad key"}
	}`)

	c := NewClient("bad", WithBaseURL(url))
	_, err := c.Complete(context.Background(), "", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsAuthError(err) {
		t.Errorf("expected auth error, got %v", err)
	}
}

func TestClient_Complete_FinishReasonLengthMapsToMaxTokens(t *testing.T) {
	url, _, _ := fakeOpenAI(t, http.StatusOK, `{
		"id":"x","model":"m","choices":[{"index":0,"finish_reason":"length","message":{"role":"assistant","content":"trunc"}}]
	}`)
	c := NewClient("k", WithBaseURL(url))
	resp, err := c.Complete(context.Background(), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != llm.StopMaxTokens {
		t.Errorf("stop: got %q want max_tokens", resp.StopReason)
	}
}

func TestRecoverToolCalls_QwenXMLFormat(t *testing.T) {
	// Exact shape observed from Qwen3 via LM Studio: the tool call lands in
	// reasoning_content as <function=...><parameter=...> markup while
	// tool_calls stays empty. The build parameter's value is a JSON object.
	reasoning := "thinking...\n</thinking>\n\n<tool_call>\n<function=score_build>\n<parameter=build>\n{\"class\": \"taekwon_kid\", \"stats\": {\"str\": 30}}\n</parameter>\n</function>\n</tool_call>"
	calls := recoverToolCalls(reasoning, "resp1")
	if len(calls) != 1 {
		t.Fatalf("want 1 recovered call, got %d", len(calls))
	}
	if calls[0].Function.Name != "score_build" {
		t.Errorf("name: %q", calls[0].Function.Name)
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("arguments not valid JSON: %v (%s)", err, calls[0].Function.Arguments)
	}
	if _, ok := args["build"]; !ok {
		t.Fatalf("expected a build param, got %s", calls[0].Function.Arguments)
	}
	// The build value must be preserved as a nested object, not a string.
	if !strings.HasPrefix(strings.TrimSpace(string(args["build"])), "{") ||
		!strings.Contains(string(args["build"]), `"taekwon_kid"`) {
		t.Errorf("build value not preserved as object: %s", args["build"])
	}
}

func TestRecoverToolCalls_MultipleAndTypedParams(t *testing.T) {
	xml := "<tool_call><function=a><parameter=x>1</parameter></function></tool_call>\n" +
		"<tool_call><function=b><parameter=y>hi</parameter></function></tool_call>"
	got := recoverToolCalls(xml, "r")
	if len(got) != 2 {
		t.Fatalf("want 2, got %d", len(got))
	}
	if got[0].Function.Name != "a" || got[1].Function.Name != "b" {
		t.Errorf("names: %q %q", got[0].Function.Name, got[1].Function.Name)
	}
	// numeric stays numeric; bare word becomes a quoted string
	if !strings.Contains(got[0].Function.Arguments, `"x":1`) {
		t.Errorf("x arg not numeric: %s", got[0].Function.Arguments)
	}
	if !strings.Contains(got[1].Function.Arguments, `"y":"hi"`) {
		t.Errorf("y arg not string-quoted: %s", got[1].Function.Arguments)
	}
	if got[0].ID == got[1].ID {
		t.Errorf("recovered ids must be unique, both %q", got[0].ID)
	}
}

func TestRecoverToolCalls_HermesJSONForm(t *testing.T) {
	hermes := `<tool_call>{"name":"score_build","arguments":{"build":{"class":"knight"}}}</tool_call>`
	got := recoverToolCalls(hermes, "r")
	if len(got) != 1 || got[0].Function.Name != "score_build" {
		t.Fatalf("hermes recovery: %+v", got)
	}
	if !strings.Contains(got[0].Function.Arguments, `"knight"`) {
		t.Errorf("hermes args: %s", got[0].Function.Arguments)
	}
}

func TestRecoverToolCalls_NoMarkersReturnsNil(t *testing.T) {
	if got := recoverToolCalls("just reasoning, no tool call markup here", ""); len(got) != 0 {
		t.Errorf("expected no recovery, got %d", len(got))
	}
}

func TestClient_Complete_RecoversBuriedToolCall(t *testing.T) {
	// Qwen3-via-LM-Studio failure mode end to end: empty content + empty
	// tool_calls, the real call buried in reasoning_content. Complete must
	// recover it and report a tool turn, not an empty end_turn.
	reasoning := "let me think\n</thinking>\n<tool_call>\n<function=lookup_item>\n<parameter=id_or_name>\n1201\n</parameter>\n</function>\n</tool_call>"
	body, err := json.Marshal(map[string]any{
		"id": "cmpl-x", "model": "qwen",
		"choices": []any{map[string]any{
			"index": 0, "finish_reason": "stop",
			"message": map[string]any{
				"role":              "assistant",
				"content":           "",
				"reasoning_content": reasoning,
				"tool_calls":        []any{},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	url, _, _ := fakeOpenAI(t, http.StatusOK, string(body))
	c := NewClient("", WithBaseURL(url))
	resp, err := c.Complete(context.Background(), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != llm.StopToolUse {
		t.Errorf("stop: got %q want tool_use (recovered)", resp.StopReason)
	}
	if !hasToolUse(resp.Content) {
		t.Fatalf("expected a recovered tool_use block, got %+v", resp.Content)
	}
	var tb llm.ContentBlock
	for _, b := range resp.Content {
		if b.Type == llm.BlockToolUse {
			tb = b
		}
	}
	if tb.ToolName != "lookup_item" {
		t.Errorf("tool name: %q", tb.ToolName)
	}
	if !strings.Contains(string(tb.ToolInput), `"id_or_name"`) {
		t.Errorf("tool input: %s", string(tb.ToolInput))
	}
}
