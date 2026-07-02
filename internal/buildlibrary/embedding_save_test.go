package buildlibrary

import (
	"context"
	"testing"
)

func TestSave_WritesEmbeddingWhenEnabled(t *testing.T) {
	lib := openWithEmbedding(t, 4, "test-model@4")
	id := seedGeneration(t, lib)
	in := validSaveInput(t, id, "w1")
	in.Embedding = []float32{0.1, 0.2, 0.3, 0.4}
	if _, err := lib.SaveAndComplete(context.Background(), in); err != nil {
		t.Fatalf("SaveAndComplete: %v", err)
	}
	var nonNull bool
	if err := lib.db.QueryRowContext(context.Background(),
		`SELECT embedding IS NOT NULL FROM saved_trajectories WHERE id=$1`, id).Scan(&nonNull); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !nonNull {
		t.Fatal("embedding column is NULL; expected the supplied vector")
	}
}

func TestSave_NullEmbeddingWhenNotSupplied(t *testing.T) {
	lib := openWithEmbedding(t, 4, "test-model@4")
	id := seedGeneration(t, lib)
	in := validSaveInput(t, id, "w1") // no Embedding
	if _, err := lib.SaveAndComplete(context.Background(), in); err != nil {
		t.Fatalf("SaveAndComplete: %v", err)
	}
	var nonNull bool
	_ = lib.db.QueryRowContext(context.Background(),
		`SELECT embedding IS NOT NULL FROM saved_trajectories WHERE id=$1`, id).Scan(&nonNull)
	if nonNull {
		t.Fatal("embedding should be NULL when not supplied")
	}
}
