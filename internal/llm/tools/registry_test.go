package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/llm"
)

// stubTool is a minimal Tool for exercising the Registry without pulling
// real dependencies in. Each test constructs one with the behavior it
// wants; production tools live in their own *_tool.go files.
type stubTool struct {
	name string
	desc string
	fn   func(ctx context.Context, in json.RawMessage) (json.RawMessage, error)
}

func (s *stubTool) Definition() llm.Tool {
	return llm.Tool{Name: s.name, Description: s.desc, InputSchema: json.RawMessage(`{}`)}
}
func (s *stubTool) Execute(ctx context.Context, in json.RawMessage) (json.RawMessage, error) {
	return s.fn(ctx, in)
}

func TestRegistry_RegisterAndDispatch(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubTool{
		name: "echo",
		desc: "returns input unchanged",
		fn: func(_ context.Context, in json.RawMessage) (json.RawMessage, error) {
			return in, nil
		},
	})

	defs := r.Definitions()
	if len(defs) != 1 || defs[0].Name != "echo" {
		t.Errorf("Definitions: got %+v", defs)
	}

	out, err := r.Dispatch(context.Background(), "echo", json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"x":1}` {
		t.Errorf("dispatch output: got %s", out)
	}
}

func TestRegistry_DispatchUnknownTool(t *testing.T) {
	r := NewRegistry()
	_, err := r.Dispatch(context.Background(), "nope", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, ok := errors.AsType[*ErrUnknownTool](err); !ok {
		t.Errorf("expected *ErrUnknownTool, got %T: %v", err, err)
	}
}

func TestRegistry_DuplicateRegistrationPanics(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubTool{name: "dup", fn: func(context.Context, json.RawMessage) (json.RawMessage, error) { return nil, nil }})
	defer func() {
		if recover() == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	r.Register(&stubTool{name: "dup", fn: func(context.Context, json.RawMessage) (json.RawMessage, error) { return nil, nil }})
}

// markerTool tags its identity via desc so WithTool can verify it kept
// the right instance after the overlay swap.
type markerTool struct {
	name   string
	marker string
}

func (m *markerTool) Definition() llm.Tool {
	return llm.Tool{Name: m.name, Description: m.marker, InputSchema: json.RawMessage(`{}`)}
}
func (m *markerTool) Execute(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"marker":"` + m.marker + `"}`), nil
}

func TestWithTool_ReplacesNamedToolWithoutMutatingOriginal(t *testing.T) {
	alpha := &markerTool{name: "alpha", marker: "v1"}
	beta := &markerTool{name: "beta", marker: "v1"}
	betaV2 := &markerTool{name: "beta", marker: "v2"}

	r := NewRegistry()
	r.Register(alpha)
	r.Register(beta)

	overlay := r.WithTool(betaV2)

	// Original registry is untouched: its beta still answers v1.
	origOut, err := r.Dispatch(context.Background(), "beta", nil)
	if err != nil {
		t.Fatalf("original.Dispatch(beta): %v", err)
	}
	if string(origOut) != `{"marker":"v1"}` {
		t.Errorf("original registry was mutated; got %s", origOut)
	}

	// Overlay swaps beta and keeps alpha intact.
	overlayOut, err := overlay.Dispatch(context.Background(), "beta", nil)
	if err != nil {
		t.Fatalf("overlay.Dispatch(beta): %v", err)
	}
	if string(overlayOut) != `{"marker":"v2"}` {
		t.Errorf("overlay didn't replace beta; got %s", overlayOut)
	}

	if _, err := overlay.Dispatch(context.Background(), "alpha", nil); err != nil {
		t.Fatalf("overlay lost alpha: %v", err)
	}
}
