package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/levonn-dev/ro-builder/internal/buildlibrary"
	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/logging"
)

// QueryEmbedder embeds the request's query text. Satisfied by
// *embedding.Client; a local interface keeps the orchestrator decoupled
// from the embedding package and trivially fakeable.
type QueryEmbedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// BuildSeeder finds the single closest saved build. Satisfied by
// *buildlibrary.Library.
type BuildSeeder interface {
	SemanticEnabled() bool
	SimilarTop(ctx context.Context, queryVec []float32, class, server string) (*buildlibrary.SavedTrajectory, float64, bool, error)
}

// composeQuery builds the query text embedded at generate time: the
// request's intent, mirroring the document side (which also includes the
// answer's final_text). Deterministic.
func composeQuery(req GenerateRequest) string {
	// Class intentionally omitted: the seeder hard-filters by class/server, so class carries no extra retrieval signal here.
	return strings.TrimSpace(req.Playstyle + "\n" + req.Description)
}

// maybeSeed embeds the request, stamps the vector on the context (so
// get_similar_past_builds can rank semantically), and returns a warm-start
// block when the closest saved build is within the seed threshold. Returns
// the (possibly augmented) context and the seed text ("" when none).
// Never fails the request: embedder/seeder errors degrade to no seed.
func (o *Orchestrator) maybeSeed(ctx context.Context, req GenerateRequest) (context.Context, string) {
	if o.embedder == nil || o.seeder == nil {
		return ctx, ""
	}
	if !o.seeder.SemanticEnabled() {
		return ctx, ""
	}
	logger := logging.From(ctx)
	vecs, err := o.embedder.Embed(ctx, []string{composeQuery(req)})
	if err != nil || len(vecs) == 0 {
		logger.Warn("seed: query embed failed; skipping semantic retrieval", slog.Any("err", err))
		return ctx, ""
	}
	qv := vecs[0]
	ctx = domain.WithQueryEmbedding(ctx, qv)

	st, dist, ok, err := o.seeder.SimilarTop(ctx, qv, req.Class, req.Server)
	if err != nil {
		logger.Warn("seed: similar lookup failed", slog.Any("err", err))
		return ctx, ""
	}
	if !ok || st == nil {
		logger.Debug("seed: no embedded candidates for this class/server; proceeding without a seed")
		return ctx, ""
	}
	if dist > o.seedMaxDistance {
		logger.Info("seed: closest saved build beyond threshold; not seeding",
			slog.String("id", st.ID), slog.Float64("distance", dist), slog.Float64("threshold", o.seedMaxDistance))
		return ctx, ""
	}
	logger.Info("seed: injecting closest saved build",
		slog.String("id", st.ID), slog.Float64("distance", dist))
	return ctx, renderSeed(st)
}

// renderSeed turns a saved build into a labelled warm-start block. A
// reference point, not a template; the prompt frames it as such.
func renderSeed(st *buildlibrary.SavedTrajectory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Reference: a previously validated build closely matching this request\n")
	fmt.Fprintf(&b, "(id %s; use get_saved_build(%q) for its full trajectory.)\n", st.ID, st.ID)
	fmt.Fprintf(&b, "Treat this as a reference point, NOT a template: adopt what fits, diverge where the current request differs.\n\n")
	if st.Description != "" {
		fmt.Fprintf(&b, "Original request: %s\n", st.Description)
	}
	if st.FinalText != "" {
		fmt.Fprintf(&b, "\nReasoning from that build:\n%s\n", st.FinalText)
	}
	return b.String()
}
