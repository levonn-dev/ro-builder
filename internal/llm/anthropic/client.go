// Package anthropic implements llm.Provider against Anthropic's
// /v1/messages API. Pure net/http; no SDK dependency, same posture as
// internal/scoring/client.go.
//
// Registers itself with the llm package's provider registry under the
// name "anthropic" via init(). cmd/api side-effect-imports this package
// to make the registration happen; it never references the package's
// types directly. Model default is claude-sonnet-4-6 (strong tool use,
// fast, lower cost than Opus); override via Config.Model or the WithModel
// option for direct construction.
//
// The wire format is a thin translation of llm package types: text /
// tool_use / tool_result content blocks map almost 1:1 to Anthropic's
// shape; orchestrator's tool_result content (json.RawMessage) is sent as
// a stringified `content` field per Anthropic's contract (tool_result
// content can be string or array-of-blocks; string is simpler and works).
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/levonn-dev/ro-builder/internal/llm"
)

// init registers this package's factory under the canonical name. The
// llm package's New() looks the name up at runtime; cmd/api just imports
// this package for the side effect.
func init() {
	llm.Register("anthropic", buildFromConfig)
}

// buildFromConfig adapts the generic llm.Config into anthropic-native
// Options. Returns an error when required fields (API key) are missing;
// surfaces at process start so operators see configuration problems
// immediately, not on first request.
func buildFromConfig(c llm.Config) (llm.Provider, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("anthropic: LLM_API_KEY is required")
	}
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
	defaultBaseURL   = "https://api.anthropic.com"
	defaultModel     = "claude-sonnet-4-6"
	defaultMaxTokens = 4096
	apiVersion       = "2023-06-01"
	// defaultHTTPTimeout caps any single Complete call when the caller
	// supplies no override. Long enough for slow-tool-use turns; short
	// enough that a stalled HTTPS connection doesn't pin a worker
	// goroutine indefinitely.
	defaultHTTPTimeout = 5 * time.Minute
)

// Client implements llm.Provider against api.anthropic.com. Safe for
// concurrent use across goroutines; http.Client is internally thread-safe
// and we never mutate fields after NewClient.
type Client struct {
	apiKey    string
	model     string
	baseURL   string
	maxTokens int
	httpC     *http.Client
}

// Option tweaks Client construction. Used by tests to point at httptest
// servers and by callers wanting non-default models or token caps.
type Option func(*Client)

func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }
func WithModel(m string) Option   { return func(c *Client) { c.model = m } }
func WithMaxTokens(n int) Option  { return func(c *Client) { c.maxTokens = n } }
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpC = h }
}

// NewClient constructs an Anthropic provider. apiKey is required; pass an
// empty string only in tests that use WithBaseURL to point at a local
// httptest server (the server can choose to ignore the header).
//
// The default HTTP client carries a defaultHTTPTimeout deadline so a
// caller that never sets WithHTTPClient is still bounded. http.DefaultClient
// has no timeout, which would let a stalled connection pin one of the
// orchestrator's worker goroutines forever. Tests/operators that want
// an unbounded client must pass WithHTTPClient explicitly.
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

// Error is a non-2xx response from the Anthropic API. Status carries the
// HTTP code and Type / Message echo Anthropic's error envelope (the API
// returns {"type":"error","error":{"type":"...","message":"..."}}).
type Error struct {
	Status  int
	Type    string
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("anthropic HTTP %d (%s): %s", e.Status, e.Type, e.Message)
}

// IsAuthError reports whether the error is a 401/403; typically a missing
// or invalid API key, surfaced to the caller as configuration rather than
// a transient failure.
func (e *Error) IsAuthError() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

// Complete sends one turn to Anthropic and decodes the response. Implements
// llm.Provider.
//
// Prompt caching: the system prompt and tool definitions are sent as
// cacheable blocks so subsequent calls within the cache TTL (5 minutes)
// reuse server-side tokens instead of re-charging full input cost. This
// matters here because the orchestrator's loop makes several Complete
// calls per request; without caching, the static system+tools (which
// include the UARO server profile and 7 tool schemas) blow the
// per-minute token rate limit at ~5 iterations. With caching, each
// follow-up call only pays for the growing message history.
func (c *Client) Complete(ctx context.Context, system string, messages []llm.Message, tools []llm.Tool) (llm.Response, error) {
	reqBody := apiRequest{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		System:    toAPISystem(system),
		Messages:  toAPIMessages(messages),
		Tools:     toAPITools(tools),
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return llm.Response{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return llm.Response{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpC.Do(httpReq)
	if err != nil {
		return llm.Response{}, fmt.Errorf("call anthropic: %w", err)
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
	return fromAPIResponse(apiResp), nil
}

// --- Wire types ---
//
// These mirror Anthropic's documented schema for /v1/messages. Kept
// internal to the package so the Provider interface stays decoupled.

type apiRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	System    []apiSystemBlock `json:"system,omitempty"`
	Messages  []apiMessage     `json:"messages"`
	Tools     []apiTool        `json:"tools,omitempty"`
}

// apiSystemBlock is the array form of `system` Anthropic accepts; the
// string form doesn't support cache_control. We always emit one block
// when the system prompt is non-empty, with cache_control on it so
// the prompt is server-side cached for follow-up calls in the same
// orchestrator loop.
type apiSystemBlock struct {
	Type         string        `json:"type"` // "text"
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

// cacheControl marks a content block as a cache breakpoint. Anthropic
// caches everything from the start of the request up to and including
// the marked block. ttl="ephemeral" is the 5-minute default; the
// extended 1h cache requires explicit ttl="1h" plus an extended-caching
// beta header which is not currently enabled.
type cacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// ephemeralCache is the singleton cache-control marker shared across
// every Complete call. The value is read-only (a pointer to a struct
// with one immutable Type field) so concurrent use is safe and we save
// the allocation per request.
var ephemeralCache = &cacheControl{Type: "ephemeral"}

type apiMessage struct {
	Role    string     `json:"role"` // "user" | "assistant"
	Content []apiBlock `json:"content"`
}

// apiBlock is the union of every content-block shape Anthropic accepts on
// the wire. We use a single struct with all-optional fields rather than a
// type-discriminated sum because Go's json package handles "extra fields
// the server ignores" silently and it keeps the marshaling code linear.
type apiBlock struct {
	Type string `json:"type"`

	Text string `json:"text,omitempty"`

	// tool_use (assistant blocks)
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result (user blocks)
	ToolUseID string `json:"tool_use_id,omitempty"`
	// Content for tool_result is documented as `string | content-block[]`.
	// We always send string (the orchestrator stringifies its JSON result),
	// which is the simpler half of the union.
	Content string `json:"content,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
}

type apiTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *cacheControl   `json:"cache_control,omitempty"`
}

type apiResponse struct {
	ID         string     `json:"id"`
	Model      string     `json:"model"`
	StopReason string     `json:"stop_reason"`
	Content    []apiBlock `json:"content"`
}

type apiErrorEnvelope struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// --- Translation ---

func toAPIMessages(in []llm.Message) []apiMessage {
	out := make([]apiMessage, 0, len(in))
	for _, m := range in {
		out = append(out, apiMessage{
			Role:    string(m.Role),
			Content: toAPIBlocks(m.Content),
		})
	}
	return out
}

func toAPIBlocks(in []llm.ContentBlock) []apiBlock {
	out := make([]apiBlock, 0, len(in))
	for _, b := range in {
		switch b.Type {
		case llm.BlockText:
			out = append(out, apiBlock{Type: "text", Text: b.Text})
		case llm.BlockToolUse:
			out = append(out, apiBlock{
				Type:  "tool_use",
				ID:    b.ToolUseID,
				Name:  b.ToolName,
				Input: b.ToolInput,
			})
		case llm.BlockToolResult:
			// tool_result's Content is the serialized payload; pass the
			// orchestrator's JSON RawMessage through as a string.
			payload := string(b.ToolResult)
			if payload == "" {
				payload = "null"
			}
			out = append(out, apiBlock{
				Type:      "tool_result",
				ToolUseID: b.ToolUseID,
				Content:   payload,
				IsError:   b.IsError,
			})
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
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	// Mark the last tool with cache_control so Anthropic caches the
	// entire tools block (everything up to and including this tool).
	// Tool definitions are static across the orchestrator's loop, so
	// every iteration after the first hits the cache.
	if len(out) > 0 {
		out[len(out)-1].CacheControl = ephemeralCache
	}
	return out
}

// toAPISystem converts the orchestrator's plain-string system prompt
// into the array form Anthropic requires for cache_control. Empty
// system → nil so the field is omitted from the request body.
func toAPISystem(s string) []apiSystemBlock {
	if s == "" {
		return nil
	}
	return []apiSystemBlock{{
		Type:         "text",
		Text:         s,
		CacheControl: ephemeralCache,
	}}
}

func fromAPIResponse(r apiResponse) llm.Response {
	resp := llm.Response{
		StopReason: llm.StopReason(r.StopReason),
		Content:    make([]llm.ContentBlock, 0, len(r.Content)),
	}
	for _, b := range r.Content {
		switch b.Type {
		case "text":
			resp.Content = append(resp.Content, llm.ContentBlock{Type: llm.BlockText, Text: b.Text})
		case "tool_use":
			resp.Content = append(resp.Content, llm.ContentBlock{
				Type:      llm.BlockToolUse,
				ToolUseID: b.ID,
				ToolName:  b.Name,
				ToolInput: b.Input,
			})
		}
	}
	return resp
}

func parseAPIError(status int, body []byte) error {
	var env apiErrorEnvelope
	if err := json.Unmarshal(body, &env); err == nil && env.Error.Message != "" {
		return &Error{Status: status, Type: env.Error.Type, Message: env.Error.Message}
	}
	return &Error{Status: status, Message: string(body)}
}

// IsAuthError is the errors.As-friendly auth-failure check.
func IsAuthError(err error) bool {
	var aErr *Error
	return errors.As(err, &aErr) && aErr.IsAuthError()
}
