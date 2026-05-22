package llm

import (
	"os"
	"strconv"
	"time"
)

// Config is the provider-agnostic configuration the orchestrator hands to
// llm.New. Fields are populated from generic environment variables
// (LLM_*) by LoadConfigFromEnv; concrete provider packages translate the
// fields they care about into their native option types.
//
// Adding a new provider should NOT require new fields here; provider-
// specific tuning belongs in the provider's own constructor options.
// Config carries only what every chat / tool-use API needs.
type Config struct {
	// Provider names the registered backend ("anthropic", "openai",
	// "huggingface", ...). Empty defaults to "anthropic".
	Provider string

	// APIKey is the credential the provider authenticates with. Required
	// for cloud providers; may be ignored by self-hosted backends.
	APIKey string

	// Model is the model identifier (e.g. "claude-sonnet-4-6",
	// "gpt-4-turbo"). Empty falls back to whatever default the provider
	// defines; mismatched names surface as a 400 from the provider's API.
	Model string

	// BaseURL overrides the provider's default endpoint. Useful for
	// proxies, local mocks, or self-hosted variants. Empty uses the
	// provider's standard endpoint.
	BaseURL string

	// Timeout caps a single LLM call. Zero falls back to DefaultTimeout.
	Timeout time.Duration

	// MaxTokens caps the model's completion length per turn. Zero lets
	// the provider apply its own default (Anthropic: 4096, OpenAI: 4096).
	// Reasoning models (Qwen3, DeepSeek-R1, o-series) spend most of this
	// budget on hidden chain-of-thought and can hit the ceiling before
	// emitting any tool_call; raise this (e.g. 16384) or disable the
	// model's thinking mode if you see finish_reason=length with empty
	// content.
	MaxTokens int
}

// DefaultTimeout is the per-LLM-call ceiling when Config.Timeout is zero.
// The orchestrator may make many calls per /generate; this bounds each
// one. Total runtime is bounded by the request's HTTP context.
const DefaultTimeout = 5 * time.Minute

// LoadConfigFromEnv reads the LLM_* environment variables into a Config.
// All fields are optional at this layer; the registered provider's
// factory decides which are required.
//
// Env vars:
//
//	LLM_PROVIDER         "anthropic" | "openai" | ... (default "anthropic")
//	LLM_API_KEY          credential (required by most cloud providers)
//	LLM_MODEL            model id (provider-specific default if empty)
//	LLM_BASE_URL         optional endpoint override
//	LLM_TIMEOUT_SECONDS  per-call timeout in seconds (default DefaultTimeout)
//	LLM_MAX_TOKENS       max completion tokens per turn (provider default if unset)
func LoadConfigFromEnv() Config {
	timeout := DefaultTimeout
	if v := os.Getenv("LLM_TIMEOUT_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeout = time.Duration(n) * time.Second
		}
	}
	maxTokens := 0
	if v := os.Getenv("LLM_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxTokens = n
		}
	}
	return Config{
		Provider:  os.Getenv("LLM_PROVIDER"),
		APIKey:    os.Getenv("LLM_API_KEY"),
		Model:     os.Getenv("LLM_MODEL"),
		BaseURL:   os.Getenv("LLM_BASE_URL"),
		Timeout:   timeout,
		MaxTokens: maxTokens,
	}
}
