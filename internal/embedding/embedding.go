// Package embedding produces vector embeddings of text via an
// OpenAI-compatible /v1/embeddings endpoint. It mirrors internal/llm/openai:
// one net/http client, no SDK, selected entirely by config (EMBEDDING_*),
// so the same code reaches local Ollama, OpenAI cloud, or Voyage's
// OpenAI-shaped HTTP API. Anthropic (the default LLM) has no embeddings
// API, so an embedder is a genuine separate dependency.
//
// The provider is OPTIONAL: when EMBEDDING_BASE_URL is unset the caller
// constructs no provider and the build library runs recency-only.
package embedding

import "context"

// Provider is the embedding gateway. Dimensions() and ModelID() are the
// two facts the storage layer needs at bootstrap (the column dimension)
// and at the guard (which model+tier produced the corpus). One active
// impl per process.
type Provider interface {
	// Embed returns one vector per input string, in order. Vectors have
	// length Dimensions().
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dimensions is the vector length, fixed by the configured model+tier.
	Dimensions() int
	// ModelID uniquely identifies the model+tier (e.g. "nomic-embed-text@768").
	// Stored in embedding_config and compared at startup to detect a model
	// swap against an existing corpus.
	ModelID() string
}
