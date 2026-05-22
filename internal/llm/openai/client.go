// Package openai implements llm.Provider against the OpenAI Chat
// Completions API (POST /chat/completions). The same wire format is
// served by many OpenAI-compatible backends (LM Studio, llama.cpp's
// server, Ollama's /v1 endpoint, vLLM, LiteLLM, Azure OpenAI, OpenRouter),
// so this provider doubles as the "local LLM" path. Point LLM_BASE_URL
// at the local server's /v1 root and the rest is just model selection.
//
// Pure net/http; no SDK dependency, same posture as the anthropic
// provider and the scoring client.
//
// Registers itself with the llm package's provider registry under the
// name "openai" via init(). cmd/api side-effect-imports this package to
// make the registration happen.
//
// Tool-use note: this provider speaks OpenAI's tool_calls schema, which
// is structurally different from Anthropic's tool_use blocks. The
// translation layer below splits a single llm.Message containing N
// tool_result blocks into N role="tool" messages on the wire (OpenAI
// requires one message per tool reply, keyed by tool_call_id), and
// merges text + tool_use blocks from one assistant turn into a single
// message with both `content` and `tool_calls` set. The orchestrator
// never sees this; the llm.Response shape is identical to anthropic's.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/levonn-dev/ro-builder/internal/llm"
)

func init() {
	llm.Register("openai", buildFromConfig)
}

// buildFromConfig adapts the generic llm.Config into openai-native
// Options. APIKey is NOT required; local OpenAI-compatible servers
// (LM Studio, llama.cpp, Ollama) ignore the Authorization header. The
// header is still sent when APIKey is non-empty so cloud OpenAI / Azure
// / OpenRouter work without special-casing.
func buildFromConfig(c llm.Config) (llm.Provider, error) {
	opts := []Option{}
	if c.BaseURL != "" {
		opts = append(opts, WithBaseURL(c.BaseURL))
	}
	if c.Model != "" {
		opts = append(opts, WithModel(c.Model))
	}
	if c.Timeout > 0 {
		opts = append(opts, WithHTTPClient(&http.Client{Timeout: c.Timeout}))
	}
	// c.Timeout == 0 falls through to NewClient's defaultHTTPTimeout so
	// a misconfigured caller never ends up on http.DefaultClient (no
	// timeout; a stalled call would pin a worker goroutine forever).
	if c.MaxTokens > 0 {
		opts = append(opts, WithMaxTokens(c.MaxTokens))
	}
	return NewClient(c.APIKey, opts...), nil
}

const (
	// defaultBaseURL matches the OpenAI SDK convention; the path
	// "/chat/completions" is appended at request time, so LM Studio users
	// set LLM_BASE_URL=http://localhost:1234/v1 and it Just Works.
	defaultBaseURL   = "https://api.openai.com/v1"
	defaultModel     = "gpt-4o-mini"
	defaultMaxTokens = 4096
	// defaultHTTPTimeout caps any single Complete call when the caller
	// supplies no override. See anthropic/client.go for rationale.
	defaultHTTPTimeout = 5 * time.Minute
)

type Client struct {
	apiKey    string
	model     string
	baseURL   string
	maxTokens int
	httpC     *http.Client
}

type Option func(*Client)

func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }
func WithModel(m string) Option   { return func(c *Client) { c.model = m } }
func WithMaxTokens(n int) Option  { return func(c *Client) { c.maxTokens = n } }
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpC = h }
}

// NewClient constructs an OpenAI-compatible provider. The default HTTP
// client carries a defaultHTTPTimeout deadline so a caller that never sets
// WithHTTPClient is still bounded; http.DefaultClient would otherwise let
// a stalled connection pin a worker goroutine forever. Tests/operators
// wanting an unbounded client must pass WithHTTPClient explicitly.
func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:    apiKey,
		model:     defaultModel,
		baseURL:   defaultBaseURL,
		maxTokens: defaultMaxTokens,
		httpC:     &http.Client{Timeout: defaultHTTPTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type Error struct {
	Status  int
	Type    string
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("openai HTTP %d (%s): %s", e.Status, e.Type, e.Message)
}

func (e *Error) IsAuthError() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

func IsAuthError(err error) bool {
	var oErr *Error
	return errors.As(err, &oErr) && oErr.IsAuthError()
}

// Complete sends one turn to the OpenAI-compatible endpoint and decodes
// the response into llm.Response. Implements llm.Provider.
//
// No prompt caching: OpenAI's automatic prefix caching (when supported by
// the backend) requires no client-side opt-in; LM Studio and other local
// backends typically don't cache at all. The orchestrator's iteration
// cost just isn't amortized here; operators running local models trade
// caching for free inference.
func (c *Client) Complete(ctx context.Context, system string, messages []llm.Message, tools []llm.Tool) (llm.Response, error) {
	apiMsgs := toAPIMessages(system, messages)
	reqBody := apiRequest{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		Messages:  apiMsgs,
		Tools:     toAPITools(tools),
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return llm.Response{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return llm.Response{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpC.Do(httpReq)
	if err != nil {
		return llm.Response{}, fmt.Errorf("call openai: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return llm.Response{}, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return llm.Response{}, parseAPIError(resp.StatusCode, respBytes)
	}

	var apiResp apiResponse
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return llm.Response{}, fmt.Errorf("decode response: %w", err)
	}
	return fromAPIResponse(apiResp)
}

// --- Wire types ---

type apiRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens,omitempty"`
	Messages  []apiMessage `json:"messages"`
	Tools     []apiTool    `json:"tools,omitempty"`
}

// apiMessage is the OpenAI chat-completions message shape. Content is a
// pointer so the JSON marshaller can distinguish an empty string ("",
// valid) from a missing field (nil, required when a tool_calls-only
// assistant message is emitted). The OpenAI spec accepts a null content
// alongside tool_calls.
type apiMessage struct {
	Role       string        `json:"role"` // "system" | "user" | "assistant" | "tool"
	Content    *string       `json:"content"`
	ToolCalls  []apiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"` // role=tool only
	Name       string        `json:"name,omitempty"`         // tool name on role=tool (optional but some backends want it)
}

type apiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"` // "function"
	Function apiToolCallFunc `json:"function"`
}

type apiToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded as a string per OpenAI spec
}

type apiTool struct {
	Type     string      `json:"type"` // "function"
	Function apiToolDecl `json:"function"`
}

type apiToolDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type apiResponse struct {
	ID      string      `json:"id"`
	Model   string      `json:"model"`
	Choices []apiChoice `json:"choices"`
}

type apiChoice struct {
	Index        int        `json:"index"`
	Message      apiMessage `json:"message"`
	FinishReason string     `json:"finish_reason"` // "stop" | "length" | "tool_calls" | ...
}

type apiErrorEnvelope struct {
	Error struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// --- Translation ---

// toAPIMessages flattens (system, []llm.Message) into the OpenAI wire
// shape. The system prompt becomes the first message with role="system"
// when non-empty. llm.Message content blocks split into one or more
// apiMessages:
//
//   - assistant text + tool_use blocks → ONE message with both content
//     and tool_calls populated
//   - user tool_result blocks → ONE role="tool" message per block, each
//     keyed by tool_call_id (OpenAI's hard requirement)
//   - user text blocks → ONE role="user" message
//
// Mixing tool_result and text in a single user message (the orchestrator
// doesn't today, but defensively) emits the tool messages first then the
// text; order matches Anthropic's interpretation of the block array.
// See internal/llm/anthropic/client.go's toAPIBlocks for the sibling
// translation; both providers see the same effective conversation.
func toAPIMessages(system string, in []llm.Message) []apiMessage {
	out := make([]apiMessage, 0, len(in)+1)
	if system != "" {
		out = append(out, apiMessage{Role: "system", Content: ptr(system)})
	}
	for _, m := range in {
		switch m.Role {
		case llm.RoleAssistant:
			var textBuf strings.Builder
			var toolCalls []apiToolCall
			for _, b := range m.Content {
				switch b.Type {
				case llm.BlockText:
					textBuf.WriteString(b.Text)
				case llm.BlockToolUse:
					args := string(b.ToolInput)
					if args == "" {
						args = "{}"
					}
					toolCalls = append(toolCalls, apiToolCall{
						ID:   b.ToolUseID,
						Type: "function",
						Function: apiToolCallFunc{
							Name:      b.ToolName,
							Arguments: args,
						},
					})
				}
			}
			if textBuf.Len() == 0 && len(toolCalls) == 0 {
				// An assistant message with neither text nor tool_calls is
				// wire-invalid (OpenAI rejects empty assistant content + no
				// tool_calls). Skip it rather than emit a malformed message;
				// the orchestrator shouldn't produce these, but defending the
				// translation keeps a future bug from reaching the API.
				continue
			}
			msg := apiMessage{Role: "assistant", ToolCalls: toolCalls}
			if textBuf.Len() > 0 {
				s := textBuf.String()
				msg.Content = &s
			} else {
				// Explicit null content alongside tool_calls; required by
				// the OpenAI schema.
				msg.Content = nil
			}
			out = append(out, msg)
		case llm.RoleUser:
			var textBuf strings.Builder
			for _, b := range m.Content {
				if b.Type == llm.BlockToolResult {
					payload := string(b.ToolResult)
					if payload == "" {
						payload = "null"
					}
					if b.IsError {
						// OpenAI has no native is_error field; wrap the payload so
						// models can distinguish error tool results from the content.
						payload = `{"is_error":true,"result":` + payload + `}`
					}
					out = append(out, apiMessage{
						Role:       "tool",
						Content:    ptr(payload),
						ToolCallID: b.ToolUseID,
					})
				}
			}
			for _, b := range m.Content {
				if b.Type == llm.BlockText {
					textBuf.WriteString(b.Text)
				}
			}
			if textBuf.Len() > 0 {
				s := textBuf.String()
				out = append(out, apiMessage{Role: "user", Content: &s})
			}
		}
	}
	return out
}

func toAPITools(in []llm.Tool) []apiTool {
	if len(in) == 0 {
		return nil
	}
	out := make([]apiTool, 0, len(in))
	for _, t := range in {
		out = append(out, apiTool{
			Type: "function",
			Function: apiToolDecl{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return out
}

func fromAPIResponse(r apiResponse) (llm.Response, error) {
	if len(r.Choices) == 0 {
		return llm.Response{}, fmt.Errorf("openai: no choices in response")
	}
	choice := r.Choices[0]
	resp := llm.Response{
		StopReason: mapFinishReason(choice.FinishReason),
	}
	if choice.Message.Content != nil && *choice.Message.Content != "" {
		resp.Content = append(resp.Content, llm.ContentBlock{
			Type: llm.BlockText,
			Text: *choice.Message.Content,
		})
	}
	for _, tc := range choice.Message.ToolCalls {
		args := tc.Function.Arguments
		if args == "" {
			args = "{}"
		}
		// OpenAI sends arguments as a JSON-encoded string. We hand the
		// orchestrator the raw bytes; downstream tool dispatch unmarshals
		// against the tool's input schema.
		resp.Content = append(resp.Content, llm.ContentBlock{
			Type:      llm.BlockToolUse,
			ToolUseID: tc.ID,
			ToolName:  tc.Function.Name,
			ToolInput: json.RawMessage(args),
		})
	}
	return resp, nil
}

func mapFinishReason(r string) llm.StopReason {
	switch r {
	case "tool_calls", "function_call":
		return llm.StopToolUse
	case "length":
		return llm.StopMaxTokens
	default:
		// "stop", "" and any other unknown finish_reason all collapse to
		// end_turn; the orchestrator treats this as "model is done".
		return llm.StopEndTurn
	}
}

func parseAPIError(status int, body []byte) error {
	var env apiErrorEnvelope
	if err := json.Unmarshal(body, &env); err == nil && env.Error.Message != "" {
		return &Error{Status: status, Type: env.Error.Type, Message: env.Error.Message}
	}
	return &Error{Status: status, Message: string(body)}
}

func ptr[T any](v T) *T { return &v }
