package domain

import "context"

type queryEmbeddingKey struct{}

// WithQueryEmbedding stamps the request's query vector on the context so
// retrieval tools can rank semantically without re-embedding. The
// orchestrator computes it once per request.
func WithQueryEmbedding(ctx context.Context, vec []float32) context.Context {
	return context.WithValue(ctx, queryEmbeddingKey{}, vec)
}

// QueryEmbeddingFromContext returns the stamped query vector, if any.
func QueryEmbeddingFromContext(ctx context.Context) ([]float32, bool) {
	v, ok := ctx.Value(queryEmbeddingKey{}).([]float32)
	return v, ok && len(v) > 0
}
