package buildlibrary

import (
	"context"
	"testing"
)

// saveWithEmbedding seeds a generation, builds a valid SaveInput with the
// given class/server, sets Embedding=vec, calls SaveAndComplete, and
// returns the saved id.
func saveWithEmbedding(t *testing.T, lib *Library, class, server string, vec []float32) string {
	t.Helper()
	saved := saveEmbeddedPending(t, lib, class, server, vec)
	// A normal saved-and-usable build is user-accepted, so it appears on the
	// RAG retrieval surface (SimilarTop / get_similar_past_builds).
	if _, err := lib.Accept(context.Background(), saved); err != nil {
		t.Fatalf("saveWithEmbedding accept: %v", err)
	}
	return saved
}

// saveEmbeddedPending saves a build WITHOUT accepting it, so it is fetchable
// but not yet visible to the RAG retrieval surface. Used by the acceptance-
// gate tests.
func saveEmbeddedPending(t *testing.T, lib *Library, class, server string, vec []float32) string {
	t.Helper()
	id := seedGeneration(t, lib)
	in := validSaveInput(t, id, "w1")
	in.Class = class
	in.Server = server
	in.Embedding = vec
	saved, err := lib.SaveAndComplete(context.Background(), in)
	if err != nil {
		t.Fatalf("saveEmbeddedPending: %v", err)
	}
	return saved
}

func TestFindSimilar_OrdersByCosineDistance(t *testing.T) {
	lib := openWithEmbedding(t, 4, "m@4")
	// Two saved builds, same class/server, different vectors.
	near := saveWithEmbedding(t, lib, "taekwon_kid", "uaro", []float32{1, 0, 0, 0})
	_ = saveWithEmbedding(t, lib, "taekwon_kid", "uaro", []float32{0, 1, 0, 0})
	got, err := lib.FindSimilar(context.Background(), []float32{0.9, 0.1, 0, 0},
		FindParams{Class: "taekwon_kid", Server: "uaro", Limit: 5})
	if err != nil {
		t.Fatalf("FindSimilar: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 results, got %d", len(got))
	}
	if got[0].ID != near {
		t.Fatalf("closest first: got %s want %s", got[0].ID, near)
	}
	if got[0].Distance > got[1].Distance {
		t.Fatalf("distances not ascending: %v", got)
	}
}

func TestFindSimilar_MaxDistanceFiltersFarRows(t *testing.T) {
	lib := openWithEmbedding(t, 4, "md@4")
	near := saveWithEmbedding(t, lib, "taekwon_kid", "uaro", []float32{1, 0, 0, 0})
	// Orthogonal vector: cosine distance to the query below is ~1.0.
	_ = saveWithEmbedding(t, lib, "taekwon_kid", "uaro", []float32{0, 1, 0, 0})

	// Query identical to `near` (distance ~0); the orthogonal row sits at
	// ~1.0. A 0.5 floor must keep only `near`.
	got, err := lib.FindSimilar(context.Background(), []float32{1, 0, 0, 0},
		FindParams{Class: "taekwon_kid", Server: "uaro", Limit: 5, MaxDistance: 0.5})
	if err != nil {
		t.Fatalf("FindSimilar: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("MaxDistance 0.5 should keep only the near row; got %d results", len(got))
	}
	if got[0].ID != near {
		t.Fatalf("kept row = %s; want %s", got[0].ID, near)
	}
}

func TestFindSimilar_EfSearchPathReturnsResults(t *testing.T) {
	// WithHNSW ef_search > 0 routes FindSimilar through the SET LOCAL
	// transaction path; results must still be correct.
	lib := openWithEmbedding(t, 4, "efs@4", WithHNSW(0, 0, 64))
	near := saveWithEmbedding(t, lib, "taekwon_kid", "uaro", []float32{1, 0, 0, 0})
	_ = saveWithEmbedding(t, lib, "taekwon_kid", "uaro", []float32{0, 1, 0, 0})
	got, err := lib.FindSimilar(context.Background(), []float32{0.9, 0.1, 0, 0},
		FindParams{Class: "taekwon_kid", Server: "uaro", Limit: 5})
	if err != nil {
		t.Fatalf("FindSimilar (ef_search path): %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 results, got %d", len(got))
	}
	if got[0].ID != near {
		t.Fatalf("closest first: got %s want %s", got[0].ID, near)
	}
}

func TestSimilarTop_ReturnsFullTrajectory(t *testing.T) {
	lib := openWithEmbedding(t, 4, "m@4")
	id := saveWithEmbedding(t, lib, "taekwon_kid", "uaro", []float32{1, 0, 0, 0})
	st, dist, ok, err := lib.SimilarTop(context.Background(), []float32{1, 0, 0, 0}, "taekwon_kid", "uaro")
	if err != nil || !ok {
		t.Fatalf("SimilarTop ok=%v err=%v", ok, err)
	}
	if st.ID != id {
		t.Fatalf("got %s want %s", st.ID, id)
	}
	if dist > 0.001 {
		t.Fatalf("identical vector distance should be ~0, got %v", dist)
	}
}
