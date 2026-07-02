package buildlibrary

import (
	"context"
	"testing"
)

func TestSetEmbedding_RoundTrip(t *testing.T) {
	lib := openWithEmbedding(t, 4, "test-model@4")
	ctx := context.Background()

	// Seed a row with a NULL embedding.
	id := saveWithEmbedding(t, lib, "taekwon_kid", "uaro", nil)

	// Confirm embedding is NULL before we write it.
	var nonNull bool
	if err := lib.db.QueryRowContext(ctx,
		`SELECT embedding IS NOT NULL FROM saved_trajectories WHERE id=$1`, id).Scan(&nonNull); err != nil {
		t.Fatalf("query pre-write: %v", err)
	}
	if nonNull {
		t.Fatal("expected NULL embedding before SetEmbedding")
	}

	// Write a known vector via SetEmbedding.
	vec := []float32{1, 0, 0, 0}
	if err := lib.SetEmbedding(ctx, id, vec); err != nil {
		t.Fatalf("SetEmbedding: %v", err)
	}

	// Confirm embedding is now non-NULL.
	if err := lib.db.QueryRowContext(ctx,
		`SELECT embedding IS NOT NULL FROM saved_trajectories WHERE id=$1`, id).Scan(&nonNull); err != nil {
		t.Fatalf("query post-write: %v", err)
	}
	if !nonNull {
		t.Fatal("embedding should be non-NULL after SetEmbedding")
	}

	// SimilarTop with the same vector should return distance ~0 (identical).
	st, dist, ok, err := lib.SimilarTop(ctx, vec, "taekwon_kid", "uaro")
	if err != nil || !ok {
		t.Fatalf("SimilarTop ok=%v err=%v", ok, err)
	}
	if st.ID != id {
		t.Fatalf("SimilarTop returned wrong id: got %s want %s", st.ID, id)
	}
	if dist > 0.001 {
		t.Fatalf("cosine distance for identical vector should be ~0, got %v", dist)
	}
}

func TestSetEmbedding_ErrorWhenSemanticDisabled(t *testing.T) {
	lib := newTestLibrary(t) // no WithEmbedding
	err := lib.SetEmbedding(context.Background(), "any-id", []float32{1, 0, 0, 0})
	if err == nil {
		t.Fatal("expected error from SetEmbedding when semantic disabled")
	}
}

func TestAllIDs_ReturnsAllInOrder(t *testing.T) {
	lib := openWithEmbedding(t, 4, "test-model@4")
	ctx := context.Background()

	// Save two rows; AllIDs should return both.
	id1 := saveWithEmbedding(t, lib, "taekwon_kid", "uaro", nil)
	id2 := saveWithEmbedding(t, lib, "knight", "uaro", nil)

	ids, err := lib.AllIDs(ctx)
	if err != nil {
		t.Fatalf("AllIDs: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("AllIDs: want 2, got %d", len(ids))
	}
	// ORDER BY created_at: id1 first.
	if ids[0] != id1 || ids[1] != id2 {
		t.Fatalf("AllIDs order: got %v want [%s %s]", ids, id1, id2)
	}
}

func TestDocumentText_MatchesSaveOrder(t *testing.T) {
	lib := openWithEmbedding(t, 4, "test-model@4")
	ctx := context.Background()

	id := seedGeneration(t, lib)
	in := validSaveInput(t, id, "w1")
	in.Playstyle = "pvm"
	in.Description = "fast leveling build"
	in.FinalText = "use kicks to farm"
	saved, err := lib.SaveAndComplete(ctx, in)
	if err != nil {
		t.Fatalf("SaveAndComplete: %v", err)
	}

	doc, err := lib.DocumentText(ctx, saved)
	if err != nil {
		t.Fatalf("DocumentText: %v", err)
	}
	// Mirrors strings.TrimSpace(playstyle + "\n" + description + "\n" + final_text).
	want := "pvm\nfast leveling build\nuse kicks to farm"
	if doc != want {
		t.Fatalf("DocumentText = %q, want %q", doc, want)
	}
}
