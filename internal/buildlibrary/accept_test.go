package buildlibrary

import (
	"context"
	"errors"
	"testing"
)

func TestAccept_StampsAndIsIdempotent(t *testing.T) {
	lib := openWithEmbedding(t, 4, "acc@4")
	id := saveEmbeddedPending(t, lib, "taekwon_kid", "uaro", []float32{1, 0, 0, 0})

	at1, err := lib.Accept(context.Background(), id)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if at1.IsZero() {
		t.Fatal("accepted_at should be set")
	}
	// Re-accepting keeps the original timestamp (COALESCE).
	at2, err := lib.Accept(context.Background(), id)
	if err != nil {
		t.Fatalf("Accept (2nd): %v", err)
	}
	if !at2.Equal(at1) {
		t.Fatalf("re-accept changed the timestamp: %v -> %v", at1, at2)
	}
}

func TestAccept_NotFound(t *testing.T) {
	lib := openWithEmbedding(t, 4, "acc@4")
	if _, err := lib.Accept(context.Background(), "does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestFindSimilar_AcceptedOnlyHidesPending(t *testing.T) {
	lib := openWithEmbedding(t, 4, "acc@4")
	// A pending build CLOSER to the query than the accepted one; the filter
	// must still hide it.
	_ = saveEmbeddedPending(t, lib, "taekwon_kid", "uaro", []float32{1, 0, 0, 0})
	accepted := saveWithEmbedding(t, lib, "taekwon_kid", "uaro", []float32{0, 1, 0, 0})

	acceptedOnly := true
	got, err := lib.FindSimilar(context.Background(), []float32{1, 0, 0, 0},
		FindParams{Class: "taekwon_kid", Server: "uaro", Limit: 5, Accepted: &acceptedOnly})
	if err != nil {
		t.Fatalf("FindSimilar: %v", err)
	}
	if len(got) != 1 || got[0].ID != accepted {
		t.Fatalf("accepted-only should return just the accepted build; got %d rows", len(got))
	}
	// Without the filter (nil), both are visible.
	all, err := lib.FindSimilar(context.Background(), []float32{1, 0, 0, 0},
		FindParams{Class: "taekwon_kid", Server: "uaro", Limit: 5})
	if err != nil {
		t.Fatalf("FindSimilar (all): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("no filter should return both; got %d", len(all))
	}
}

func TestSimilarTop_SkipsPending(t *testing.T) {
	lib := openWithEmbedding(t, 4, "acc@4")
	// The closer build is pending; SimilarTop (RAG) must skip it.
	_ = saveEmbeddedPending(t, lib, "taekwon_kid", "uaro", []float32{1, 0, 0, 0})
	accepted := saveWithEmbedding(t, lib, "taekwon_kid", "uaro", []float32{0, 1, 0, 0})
	st, _, ok, err := lib.SimilarTop(context.Background(), []float32{1, 0, 0, 0}, "taekwon_kid", "uaro")
	if err != nil {
		t.Fatalf("SimilarTop: %v", err)
	}
	if !ok || st.ID != accepted {
		t.Fatalf("SimilarTop should return the accepted build, not the closer pending one; ok=%v", ok)
	}
}

func TestFind_AcceptedFilter(t *testing.T) {
	lib := openWithEmbedding(t, 4, "acc@4")
	pending := saveEmbeddedPending(t, lib, "taekwon_kid", "uaro", nil)
	accepted := saveWithEmbedding(t, lib, "taekwon_kid", "uaro", nil)

	yes, no := true, false
	acc, err := lib.Find(context.Background(), FindParams{Class: "taekwon_kid", Server: "uaro", Accepted: &yes})
	if err != nil {
		t.Fatal(err)
	}
	if len(acc) != 1 || acc[0].ID != accepted {
		t.Fatalf("Accepted=true should return only the accepted build; got %d", len(acc))
	}
	pend, err := lib.Find(context.Background(), FindParams{Class: "taekwon_kid", Server: "uaro", Accepted: &no})
	if err != nil {
		t.Fatal(err)
	}
	if len(pend) != 1 || pend[0].ID != pending {
		t.Fatalf("Accepted=false should return only the pending build; got %d", len(pend))
	}
	all, err := lib.Find(context.Background(), FindParams{Class: "taekwon_kid", Server: "uaro"})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("no filter should return both; got %d", len(all))
	}
	// accepted_at is surfaced on the summary so the user can see state.
	for _, s := range all {
		if s.ID == accepted && s.AcceptedAt == nil {
			t.Error("accepted build summary should carry accepted_at")
		}
		if s.ID == pending && s.AcceptedAt != nil {
			t.Error("pending build summary should have nil accepted_at")
		}
	}
}
