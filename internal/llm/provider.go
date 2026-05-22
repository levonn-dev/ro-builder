// Package llm is the provider-agnostic LLM layer. Concrete providers
// (Anthropic, future HF / self-hosted) implement Provider; the orchestrator
// only knows about this interface so swapping providers is one new file.
//
// Types here intentionally mirror Anthropic's messages-with-tools shape
// (system + alternating user/assistant messages, tool_use / tool_result
// content blocks, stop_reason) because that's the contract the
// orchestrator's iteration loop is built around. A non-Anthropic provider
// adapts its native shape to these types in its Complete() implementation.
package llm

import (
	"context"
	"encoding/json"
)

// Role identifies who produced a Message; `user` is the orchestrator (and,
// inside a tool-use loop, the orchestrator returning tool_result blocks);
// `assistant` is the LLM. There is no `system` role here; the system
// prompt is a separate parameter on Complete.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// BlockType discriminates ContentBlock variants. Each block carries the
// fields relevant to its type and leaves the others zero. Naming matches
// Anthropic's wire format so debugging logs read naturally.
type BlockType string

const (
	BlockText       BlockType = "text"
	BlockToolUse    BlockType = "tool_use"
	BlockToolResult BlockType = "tool_result"
)

// ContentBlock is one segment of a Message's content array.
//
//   - Text: BlockText with Text populated.
//   - LLM tool call: BlockToolUse with ToolUseID, ToolName, ToolInput.
//   - Orchestrator's tool result: BlockToolResult with ToolUseID and either
//     ToolResult (success) or ToolResult+IsError=true (tool failed).
type ContentBlock struct {
	Type BlockType `json:"type"`

	// Text-block fields.
	Text string `json:"text,omitempty"`

	// Tool-use / tool-result shared fields. ToolUseID pairs a tool_use call
	// with its tool_result reply across messages.
	ToolUseID string `json:"tool_use_id,omitempty"`

	// Tool-use-only fields (the LLM is asking us to invoke a tool).
	ToolName  string          `json:"tool_name,omitempty"`
	ToolInput json.RawMessage `json:"tool_input,omitempty"`

	// Tool-result-only fields (the orchestrator answering a previous
	// tool_use). ToolResult is the JSON-encoded result (or the error
	// payload when IsError is true). The provider is responsible for
	// turning this into the wire format its API expects.
	ToolResult json.RawMessage `json:"tool_result,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`
}

// Message is one turn in the conversation.
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
}

// Tool is the LLM-facing definition of a tool; what it's called, what it
// does, and the JSON Schema for its input. Schema is opaque here; the tool
// registry produces it and the provider passes it through to the wire.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// StopReason mirrors Anthropic's stop_reason field; the orchestrator
// branches on it to decide whether to fulfill tool calls or treat the
// response as final. "max_tokens" is a hard ceiling we surface as an error
// rather than keep iterating.
type StopReason string

const (
	StopEndTurn   StopReason = "end_turn"
	StopToolUse   StopReason = "tool_use"
	StopMaxTokens StopReason = "max_tokens"
)

// Response is one Complete() return; the assistant's content blocks plus
// why generation stopped. The orchestrator turns Content into the next
// Message of role assistant when continuing the loop.
type Response struct {
	Content    []ContentBlock `json:"content"`
	StopReason StopReason     `json:"stop_reason"`
}

// Provider is the LLM gateway. Implementations hold the API key / endpoint
// / model, and translate between the wire format and these types. One
// active impl per process.
type Provider interface {
	// Complete runs one turn. system is the system prompt (may be empty);
	// messages is the full conversation so far; tools is the available
	// tool definitions. Returns the assistant's response plus stop reason.
	Complete(ctx context.Context, system string, messages []Message, tools []Tool) (Response, error)
}
