package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/levonn-dev/ro-builder/internal/buildlibrary"
	"github.com/levonn-dev/ro-builder/internal/llm"
)

const getSavedBuildSchema = `{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "The saved trajectory id to load in full (from get_similar_past_builds results)."}
  },
  "required": ["id"]
}`

type getSavedBuildTool struct{ lib *buildlibrary.Library }

// NewGetSavedBuild constructs the get_saved_build tool. lib is required.
func NewGetSavedBuild(lib *buildlibrary.Library) Tool { return &getSavedBuildTool{lib: lib} }

func (t *getSavedBuildTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "get_saved_build",
		Description: "Load the FULL saved trajectory (stats, skills, gear, leveling targets, scoring) for a given id. " +
			"Use after get_similar_past_builds returns a promising summary you want to study or adapt. " +
			"A reference point, not a template; design the active build from the current request and diverge when the reasoning supports it.",
		InputSchema: json.RawMessage(getSavedBuildSchema),
	}
}

type getSavedBuildInput struct {
	ID string `json:"id"`
}

func (t *getSavedBuildTool) Execute(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var in getSavedBuildInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("decode get_saved_build input: %w", err)
	}
	if in.ID == "" {
		return nil, errors.New("get_saved_build: id is required")
	}
	st, err := t.lib.Get(ctx, in.ID)
	if errors.Is(err, buildlibrary.ErrNotFound) {
		return json.Marshal(map[string]string{"error": "no saved build with id " + in.ID})
	}
	if err != nil {
		return nil, fmt.Errorf("get saved build: %w", err)
	}
	// The RAG surface only exposes user-accepted builds. A build that passed
	// gates but has not been accepted is treated as absent here, matching the
	// retrieval tools that never surfaced its id in the first place.
	if st.AcceptedAt == nil {
		return json.Marshal(map[string]string{"error": "no saved build with id " + in.ID})
	}
	return json.Marshal(st)
}
