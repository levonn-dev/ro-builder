package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/buildlibrary"
	"github.com/levonn-dev/ro-builder/internal/domain"
)

type fakeEmbedder struct{ vec []float32 }

func (f fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = f.vec
	}
	return out, nil
}

type countingEmbedder struct {
	calls int
	vec   []float32
}

func (c *countingEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	c.calls++
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = c.vec
	}
	return out, nil
}

type fakeSeeder struct {
	st              *buildlibrary.SavedTrajectory
	dist            float64
	ok              bool
	semanticEnabled bool
}

func (f fakeSeeder) SemanticEnabled() bool { return f.semanticEnabled }
func (f fakeSeeder) SimilarTop(_ context.Context, _ []float32, _, _ string) (*buildlibrary.SavedTrajectory, float64, bool, error) {
	return f.st, f.dist, f.ok, nil
}

func TestMaybeSeed_InjectsWithinThreshold(t *testing.T) {
	st := &buildlibrary.SavedTrajectory{ID: "abc", Description: "agi crit", FinalText: "use jur + crit"}
	o := New(nil, nil).WithEmbedder(fakeEmbedder{vec: []float32{1}}).
		WithSeeder(fakeSeeder{st: st, dist: 0.10, ok: true, semanticEnabled: true}).WithSeedMaxDistance(0.15)
	ctx, seed := o.maybeSeed(context.Background(), GenerateRequest{Class: "taekwon_kid", Server: "uaro", Description: "agi crit tk"})
	if seed == "" || !strings.Contains(seed, "abc") {
		t.Fatalf("expected seed text referencing the build, got %q", seed)
	}
	if _, ok := domain.QueryEmbeddingFromContext(ctx); !ok {
		t.Fatal("query embedding should be stamped on the context")
	}
}

func TestMaybeSeed_SkipsBeyondThreshold(t *testing.T) {
	st := &buildlibrary.SavedTrajectory{ID: "abc"}
	o := New(nil, nil).WithEmbedder(fakeEmbedder{vec: []float32{1}}).
		WithSeeder(fakeSeeder{st: st, dist: 0.90, ok: true, semanticEnabled: true}).WithSeedMaxDistance(0.15)
	ctx, seed := o.maybeSeed(context.Background(), GenerateRequest{})
	if seed != "" {
		t.Fatalf("expected no seed beyond threshold, got %q", seed)
	}
	if _, ok := domain.QueryEmbeddingFromContext(ctx); !ok {
		t.Fatal("embed succeeded: vector should be stamped even when the seed block is suppressed")
	}
}

func TestMaybeSeed_NoEmbedderNoop(t *testing.T) {
	o := New(nil, nil)
	ctx, seed := o.maybeSeed(context.Background(), GenerateRequest{})
	if seed != "" {
		t.Fatal("no embedder => no seed")
	}
	if _, ok := domain.QueryEmbeddingFromContext(ctx); ok {
		t.Fatal("no embedder => no stamped vector")
	}
}

func TestMaybeSeed_SkipsWhenSemanticDisabled(t *testing.T) {
	emb := &countingEmbedder{vec: []float32{1}}
	o := New(nil, nil).WithEmbedder(emb).
		WithSeeder(fakeSeeder{semanticEnabled: false}).WithSeedMaxDistance(0.15)
	ctx, seed := o.maybeSeed(context.Background(), GenerateRequest{})
	if seed != "" {
		t.Fatalf("semantic disabled => no seed, got %q", seed)
	}
	if _, ok := domain.QueryEmbeddingFromContext(ctx); ok {
		t.Fatal("semantic disabled => no stamped vector")
	}
	if emb.calls != 0 {
		t.Fatalf("semantic disabled => embedder must not be called, got %d call(s)", emb.calls)
	}
}
