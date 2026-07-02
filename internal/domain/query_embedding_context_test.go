package domain

import (
	"context"
	"testing"
)

func TestQueryEmbeddingContext(t *testing.T) {
	if _, ok := QueryEmbeddingFromContext(context.Background()); ok {
		t.Error("empty context should have no query embedding")
	}
	ctx := WithQueryEmbedding(context.Background(), []float32{1, 2, 3})
	v, ok := QueryEmbeddingFromContext(ctx)
	if !ok || len(v) != 3 || v[2] != 3 {
		t.Fatalf("got %v %v", v, ok)
	}
}
