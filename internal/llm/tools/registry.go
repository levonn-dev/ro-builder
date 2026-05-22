// Package tools is the LLM-facing tool registry. The orchestrator uses it
// to (a) list tool definitions for inclusion in each Complete() call and
// (b) dispatch tool_use blocks the LLM emits back to executable Go code.
//
// Adding a tool is one new file: implement Tool, register it in the
// orchestrator's wiring. The interface intentionally narrow; Definition
// for the schema, Execute for the dispatch; so test-doubles are trivial.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/levonn-dev/ro-builder/internal/llm"
)

// Tool is one capability the LLM can invoke. Implementations should be
// stateless across calls (any state lives in their dependencies, e.g. a
// scoring.Client passed in at construction).
type Tool interface {
	// Definition returns the LLM-facing description: name, description,
	// JSON Schema for inputs. The provider passes this through to the
	// model verbatim, so write descriptions for the model not for humans.
	Definition() llm.Tool

	// Execute runs the tool with the LLM-provided input and returns a
	// JSON-encoded result. Errors are wrapped by the orchestrator into
	// tool_result blocks with is_error=true so the model can recover.
	Execute(ctx context.Context, input json.RawMessage) (json.RawMessage, error)
}

// Registry maps tool name → Tool. Construction is by NewRegistry +
// Register; Definitions and Dispatch are the orchestrator's read interface.
type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool. Panics on name collision so wiring bugs surface
// at process start rather than as silent overwrites at request time.
func (r *Registry) Register(t Tool) {
	name := t.Definition().Name
	if name == "" {
		panic("tools.Register: tool with empty name")
	}
	if _, exists := r.tools[name]; exists {
		panic(fmt.Sprintf("tools.Register: duplicate name %q", name))
	}
	r.tools[name] = t
}

// Definitions returns every registered tool's LLM definition, sorted by
// name. Stable order matters: the Anthropic provider tags the last tool
// with cache_control, so a varying tail would shift the cache breakpoint
// across iterations and force misses.
func (r *Registry) Definitions() []llm.Tool {
	out := make([]llm.Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t.Definition())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Dispatch invokes the named tool with the given input. Returns
// ErrUnknownTool if the name isn't registered (the orchestrator surfaces
// this as a tool_result with is_error so the model can correct course).
func (r *Registry) Dispatch(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	t, ok := r.tools[name]
	if !ok {
		return nil, &ErrUnknownTool{Name: name}
	}
	return t.Execute(ctx, input)
}

// WithTool returns a registry whose tool set is r's tools, except any
// tool with the same Name as t is replaced by t. The original r is not
// mutated. Used by the orchestrator to substitute a per-request
// submit_trajectory tool (wired with per-request scoring/gates/accept
// closures) without disturbing the shared registry.
func (r *Registry) WithTool(t Tool) *Registry {
	out := NewRegistry()
	name := t.Definition().Name
	for k, existing := range r.tools {
		if k == name {
			continue
		}
		out.tools[k] = existing
	}
	out.tools[name] = t
	return out
}

// ErrUnknownTool surfaces the "LLM called a tool we don't have" case
// distinctly so the orchestrator can format an actionable error message.
type ErrUnknownTool struct {
	Name string
}

func (e *ErrUnknownTool) Error() string {
	return fmt.Sprintf("unknown tool: %q", e.Name)
}
