package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/llm"
)

// fakeAnthropic stands in for api.anthropic.com; captures the request
// body and replies with whatever JSON the test wants. Lets us assert the
// wire shape without an API key or live API.
func fakeAnthropic(t *testing.T, status int, respBody string) (string, *map[string]any, *http.Header) {
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
	url, captured, headers := fakeAnthropic(t, http.StatusOK, `{
		"id": "msg_test",
		"model": "claude-sonnet-4-6",
		"stop_reason": "end_turn",
		"content": [{"type": "text", "text": "OK"}]
	}`)

	c := NewClient("sk-test-key", WithBaseURL(url), WithModel("claude-sonnet-4-6"))
	resp, err := c.Complete(context.Background(), "you are a helpful assistant", []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.BlockText, Text: "hi"}}},
	}, nil)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	// Headers; auth + version are non-negotiable.
	if got := headers.Get("x-api-key"); got != "sk-test-key" {
		t.Errorf("x-api-key: got %q want %q", got, "sk-test-key")
	}
	if got := headers.Get("anthropic-version"); got != apiVersion {
		t.Errorf("anthropic-version: got %q want %q", got, apiVersion)
	}

	// Body; keys Anthropic requires.
	if (*captured)["model"] != "claude-sonnet-4-6" {
		t.Errorf("model: got %v", (*captured)["model"])
	}
	// system is sent as an array of text blocks with cache_control on the
	// last block (prompt-caching layout). We assert the text round-trips
	// and the cache marker is present so a regression that drops caching
	// surfaces here.
	sysBlocks, ok := (*captured)["system"].([]any)
	if !ok || len(sysBlocks) != 1 {
		t.Fatalf("system: expected []block of length 1, got %v", (*captured)["system"])
	}
	sb := sysBlocks[0].(map[string]any)
	if sb["text"] != "you are a helpful assistant" {
		t.Errorf("system.text: got %v", sb["text"])
	}
	if sb["cache_control"] == nil {
		t.Errorf("system.cache_control missing; prompt caching regressed")
	}
	msgs := (*captured)["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages: got %d want 1", len(msgs))
	}

	// Response decode.
	if resp.StopReason != llm.StopEndTurn {
		t.Errorf("stop_reason: got %q want end_turn", resp.StopReason)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "OK" {
		t.Errorf("content: got %+v", resp.Content)
	}
}

func TestClient_Complete_DecodesToolUse(t *testing.T) {
	url, _, _ := fakeAnthropic(t, http.StatusOK, `{
		"id": "msg_test",
		"model": "claude-sonnet-4-6",
		"stop_reason": "tool_use",
		"content": [
			{"type": "text", "text": "Let me check."},
			{"type": "tool_use", "id": "toolu_01", "name": "score_build", "input": {"build":{"class":"novice"}}}
		]
	}`)

	c := NewClient("k", WithBaseURL(url))
	resp, err := c.Complete(context.Background(), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != llm.StopToolUse {
		t.Errorf("stop_reason: got %q", resp.StopReason)
	}
	if len(resp.Content) != 2 {
		t.Fatalf("content blocks: got %d", len(resp.Content))
	}
	if resp.Content[1].Type != llm.BlockToolUse {
		t.Errorf("block 1 type: got %q", resp.Content[1].Type)
	}
	if resp.Content[1].ToolName != "score_build" {
		t.Errorf("tool name: got %q", resp.Content[1].ToolName)
	}
	if resp.Content[1].ToolUseID != "toolu_01" {
		t.Errorf("tool use id: got %q", resp.Content[1].ToolUseID)
	}
}

func TestClient_Complete_SerializesToolResultMessage(t *testing.T) {
	url, captured, _ := fakeAnthropic(t, http.StatusOK, `{"id":"x","model":"m","stop_reason":"end_turn","content":[]}`)

	c := NewClient("k", WithBaseURL(url))
	_, err := c.Complete(context.Background(), "", []llm.Message{
		{
			Role: llm.RoleUser,
			Content: []llm.ContentBlock{
				{
					Type:       llm.BlockToolResult,
					ToolUseID:  "toolu_01",
					ToolResult: json.RawMessage(`{"derived":{"hit":2}}`),
				},
			},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	msgs := (*captured)["messages"].([]any)
	msg0 := msgs[0].(map[string]any)
	content := msg0["content"].([]any)
	block := content[0].(map[string]any)
	if block["type"] != "tool_result" {
		t.Errorf("type: got %v", block["type"])
	}
	if block["tool_use_id"] != "toolu_01" {
		t.Errorf("tool_use_id: got %v", block["tool_use_id"])
	}
	// Anthropic accepts tool_result content as a string; the orchestrator
	// hands us JSON, we pass it through verbatim.
	if !strings.Contains(block["content"].(string), `"derived"`) {
		t.Errorf("content: got %v", block["content"])
	}
}

func TestClient_Complete_ErrorEnvelope(t *testing.T) {
	url, _, _ := fakeAnthropic(t, http.StatusUnauthorized, `{
		"type": "error",
		"error": {"type": "authentication_error", "message": "x-api-key header is required"}
	}`)

	c := NewClient("", WithBaseURL(url))
	_, err := c.Complete(context.Background(), "", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsAuthError(err) {
		t.Errorf("expected auth error, got %v", err)
	}
}

// TestIntegration_LiveAnthropic exercises the real API when LLM_API_KEY is
// set. Same env var the runtime reads, so a working .env is enough to
// trigger the test; no parallel anthropic-specific key to maintain.
// Skipped otherwise so the suite stays runnable without credentials.
// Catches contract drift between this client and the live wire format that
// the httptest fakes can't catch.
func TestIntegration_LiveAnthropic(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: skipping in -short mode")
	}
	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		t.Skip("LLM_API_KEY not set; skipping live API test")
	}

	c := NewClient(apiKey)
	resp, err := c.Complete(context.Background(),
		"You are a calculator. Reply with only a number.",
		[]llm.Message{
			{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.BlockText, Text: "What is 2+2?"}}},
		},
		nil)
	if err != nil {
		t.Fatalf("live Complete: %v", err)
	}
	if len(resp.Content) == 0 || resp.Content[0].Type != llm.BlockText {
		t.Fatalf("expected text content, got %+v", resp.Content)
	}
	if !strings.Contains(resp.Content[0].Text, "4") {
		t.Errorf("expected '4' in response, got %q", resp.Content[0].Text)
	}
}
