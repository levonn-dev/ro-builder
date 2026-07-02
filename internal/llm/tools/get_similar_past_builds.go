package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/levonn-dev/ro-builder/internal/buildlibrary"
	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/llm"
)

const getSimilarPastBuildsSchema = `{
  "type": "object",
  "properties": {
    "class": {"type": "string", "description": "Class name filter (e.g. \"taekwon_kid\"). Omit to match the request's active class."},
    "server": {"type": "string", "description": "Server profile key filter (e.g. \"uaro\"). Omit to match the request's active server."},
    "limit": {"type": "integer", "description": "Max results to return (default 5, max 50)."}
  }
}`

type getSimilarPastBuildsTool struct {
	lib *buildlibrary.Library
	// maxDistance is the Tier B list floor: semantic results beyond this
	// cosine distance are dropped so the LLM never sees far-off matches.
	// 0 = no floor (return the nearest `limit` regardless of distance).
	maxDistance float64
}

// NewGetSimilarPastBuilds constructs the get_similar_past_builds tool.
// lib is required; a nil library here would mean a deployment without
// persistent build storage, which we treat as a programming error
// rather than a graceful "no results." maxDistance is the semantic list
// floor (EMBEDDING_SIMILAR_MAX_DISTANCE); ignored in recency mode.
func NewGetSimilarPastBuilds(lib *buildlibrary.Library, maxDistance float64) Tool {
	if lib == nil {
		panic("tools.NewGetSimilarPastBuilds: library is required")
	}
	return &getSimilarPastBuildsTool{lib: lib, maxDistance: maxDistance}
}

func (t *getSimilarPastBuildsTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "get_similar_past_builds",
		Description: "Retrieve summaries of previously generated and quality-gate-validated trajectories matching the given class + server. " +
			"Returns lightweight records (id, class, server, playstyle, description, gate counts) ranked by similarity to the active request when embeddings are available, otherwise sorted by recency; at most `limit` (default 5). " +
			"USE THESE AS REFERENCE POINTS, NOT TEMPLATES. Past builds are signals about what's worked under similar constraints; the active build should still be designed from scratch given the current request's class definition, gear pool, scenario, and server profile. Diverging from a past build's choices is fine when the reasoning supports it. " +
			"Calling without args defaults to filtering by the request's active class + server, which is usually what you want.",
		InputSchema: json.RawMessage(getSimilarPastBuildsSchema),
	}
}

type getSimilarPastBuildsInput struct {
	Class  string `json:"class,omitempty"`
	Server string `json:"server,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

func (t *getSimilarPastBuildsTool) Execute(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var in getSimilarPastBuildsInput
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, fmt.Errorf("decode get_similar_past_builds input: %w", err)
		}
	}
	// Fill in defaults from the request context the orchestrator stamps.
	// This lets the LLM call with no args and still get class/server-
	// scoped results; the common case.
	if in.Class == "" || in.Server == "" {
		if profile := domain.ServerProfileFromContext(ctx); profile != nil && in.Server == "" {
			in.Server = profile.Key
		}
		// Class fallback would need a similar context plumb (the request's
		// class isn't currently on ctx). Skip for now; caller can pass
		// `class` explicitly, or the empty-class branch returns
		// cross-class results from the same server.
	}

	// RAG retrieval only surfaces user-accepted builds; builds that merely
	// passed quality gates are not seedable until the user accepts them.
	acceptedOnly := true

	// Prefer semantic ranking when the orchestrator stamped a query vector
	// and the library has embeddings; otherwise fall back to recency.
	if vec, ok := domain.QueryEmbeddingFromContext(ctx); ok && t.lib.SemanticEnabled() {
		sims, err := t.lib.FindSimilar(ctx, vec, buildlibrary.FindParams{Class: in.Class, Server: in.Server, Limit: in.Limit, MaxDistance: t.maxDistance, Accepted: &acceptedOnly})
		if err != nil {
			return nil, fmt.Errorf("library find similar: %w", err)
		}
		out, err := json.Marshal(sims)
		if err != nil {
			return nil, fmt.Errorf("encode get_similar_past_builds output: %w", err)
		}
		return out, nil
	}

	summaries, err := t.lib.Find(ctx, buildlibrary.FindParams{
		Class:    in.Class,
		Server:   in.Server,
		Limit:    in.Limit,
		Accepted: &acceptedOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("library find: %w", err)
	}
	out, err := json.Marshal(summaries)
	if err != nil {
		return nil, fmt.Errorf("encode get_similar_past_builds output: %w", err)
	}
	return out, nil
}
