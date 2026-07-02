package buildlibrary

import (
	"context"
	"testing"
)

func TestRecallAtK(t *testing.T) {
	lib := openWithEmbedding(t, 4, "test-model@4")
	ctx := context.Background()

	// Seed five rows with distinct unit-ish vectors so the cosine ordering
	// is deterministic. At tiny corpus size exact KNN and HNSW agree, so
	// recall must be 1.0.
	vecs := [][]float32{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
		{0, 0, 1, 0},
		{0, 0, 0, 1},
		{0.7071, 0.7071, 0, 0},
	}
	classes := []string{"taekwon_kid", "knight", "wizard", "hunter", "monk"}
	for i, v := range vecs {
		saveWithEmbedding(t, lib, classes[i], "uaro", v)
	}

	probes, err := lib.SampleVectors(ctx, 10)
	if err != nil {
		t.Fatalf("SampleVectors: %v", err)
	}
	if len(probes) != len(vecs) {
		t.Fatalf("SampleVectors: want %d probes, got %d", len(vecs), len(probes))
	}

	recall, err := lib.RecallAtK(ctx, probes, 3, 0)
	if err != nil {
		t.Fatalf("RecallAtK: %v", err)
	}
	if recall != 1.0 {
		t.Fatalf("RecallAtK = %.4f, want 1.0 (HNSW and exact should agree at tiny corpus)", recall)
	}
}
