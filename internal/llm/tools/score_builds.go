package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/llm"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// scoreBuildsMaxBatch bounds how many candidates one call scores. The calls
// run serially through the sidecar pool, so an unbounded batch could tie up
// a worker; a handful of distinct directions is the intended use.
const scoreBuildsMaxBatch = 8

const scoreBuildsSchema = `{
  "type": "object",
  "required": ["builds"],
  "properties": {
    "builds": {
      "type": "array",
      "description": "2-8 candidate builds to score together in one call, for comparing genuinely distinct directions (different weapon line, stat spread, gear archetype) in a single turn instead of a serial sweep. Each item mirrors score_build's input plus an optional label. Use this to PICK a direction; use singular score_build to converge the one build you chose.",
      "items": {
        "type": "object",
        "required": ["build"],
        "properties": {
          "label": {"type": "string", "description": "Optional tag echoed back in the result so you can tell candidates apart (e.g. 'agi-crit', 'two-hand-quicken')."},
          "build": ` + buildObjectSchema + `,
          "scenario": ` + scenarioObjectSchema + `
        }
      }
    }
  }
}`

// scoreBuildsTool scores several candidate builds in one call so the model
// can compare distinct directions in a single turn instead of a serial
// score_build sweep. It reuses scoreOne, so each candidate is scored
// exactly as score_build would score it.
type scoreBuildsTool struct {
	client *scoring.Client
	cat    *catalog.Catalog
}

// NewScoreBuilds constructs the batch scoring tool. Panics on nil client,
// matching the fail-fast convention of the other tool constructors. cat may
// be nil; it is only needed to resolve a candidate's active_buffs.
func NewScoreBuilds(client *scoring.Client, cat *catalog.Catalog) Tool {
	if client == nil {
		panic("tools.NewScoreBuilds: scoring client is required")
	}
	return &scoreBuildsTool{client: client, cat: cat}
}

func (s *scoreBuildsTool) Definition() llm.Tool {
	return llm.Tool{
		Name: "score_builds",
		Description: "Score several candidate builds in one call and get a result per candidate, for comparing distinct directions (weapon line, stat spread, gear) in one turn instead of a serial score_build sweep. " +
			"Each candidate is scored exactly like score_build (derived stats always; combat sim when a scenario target is given). A candidate that fails for its own reason (invalid build, or a bad id the calc rejects) reports its error in place without aborting the others; a calc-engine outage fails the whole call so you know the numbers are unavailable rather than seeing every candidate 'fail'. " +
			"Use this to choose a direction; once chosen, converge that single build with score_build. Never compute stat numbers yourself.",
		InputSchema: json.RawMessage(scoreBuildsSchema),
	}
}

// scoreBuildsItem is one candidate: a score_build input plus an optional
// label. Embeds scoreBuildInput so the per-candidate build/scenario shape
// can't drift from the singular score_build tool's input.
type scoreBuildsItem struct {
	scoreBuildInput
	Label string `json:"label,omitempty"`
}

type scoreBuildsInput struct {
	Builds []scoreBuildsItem `json:"builds"`
}

// scoreBuildsResult is one candidate's outcome. Exactly one of Score / Error
// is set. Label echoes the caller's tag; Index is the request position so
// results stay matchable even when Label is omitted.
type scoreBuildsResult struct {
	Label string                 `json:"label,omitempty"`
	Index int                    `json:"index"`
	Score *scoring.ScoreResponse `json:"score,omitempty"`
	Error string                 `json:"error,omitempty"`
}

func (s *scoreBuildsTool) Execute(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var in scoreBuildsInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("decode score_builds input: %w", err)
	}
	if len(in.Builds) == 0 {
		return nil, fmt.Errorf("score_builds requires at least one build")
	}
	if len(in.Builds) > scoreBuildsMaxBatch {
		return nil, fmt.Errorf("score_builds accepts at most %d candidates per call (got %d); split into multiple calls", scoreBuildsMaxBatch, len(in.Builds))
	}

	// Score candidates concurrently. The sidecar pool serializes internally
	// (SIDECAR_WORKERS threads, each with its own isolated shim), so firing
	// all candidates at once lets the pool fill every worker instead of
	// leaving them idle behind a serial loop. Results are written by index,
	// so output order is deterministic regardless of completion order.
	results := make([]scoreBuildsResult, len(in.Builds))
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		globalErr error
	)
	for i, item := range in.Builds {
		wg.Add(1)
		go func(i int, item scoreBuildsItem) {
			defer wg.Done()
			r := scoreBuildsResult{Label: item.Label, Index: i}
			resp, err := scoreOne(ctx, s.client, s.cat, item.Build, item.Scenario)
			if err != nil {
				// A candidate's own bad input (malformed build, or a 4xx the
				// sidecar rejected) is captured in place so one bad build
				// can't blank the comparison. A global failure (sidecar 5xx /
				// transport) is not candidate-specific — every candidate would
				// hit it — so record it and abort the batch with an is_error
				// instead of returning a comparison where every entry silently
				// "failed" and the model can't tell the engine is down.
				if !isCandidateError(err) {
					mu.Lock()
					if globalErr == nil {
						globalErr = err
					}
					mu.Unlock()
				}
				r.Error = err.Error()
			} else {
				r.Score = resp
			}
			results[i] = r
		}(i, item)
	}
	wg.Wait()
	if globalErr != nil {
		return nil, fmt.Errorf("score_builds: calc engine unavailable: %w", globalErr)
	}

	out, err := json.Marshal(struct {
		Results []scoreBuildsResult `json:"results"`
	}{Results: results})
	if err != nil {
		return nil, fmt.Errorf("encode score_builds output: %w", err)
	}
	return out, nil
}

// isCandidateError reports whether err is specific to one candidate's input
// rather than a global infrastructure failure. A malformed build
// (errBuildInvalid) or a sidecar 4xx (scoring.IsClientError) is the
// candidate's problem and is captured per-result; a 5xx or transport error
// is global and aborts the batch so the model gets an is_error signal.
func isCandidateError(err error) bool {
	return errors.Is(err, errBuildInvalid) || scoring.IsClientError(err)
}
