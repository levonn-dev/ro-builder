// Package logging carries a *slog.Logger on context so handlers,
// orchestrator, and worker goroutines all log with the same correlation
// fields (request_id, generation_id, worker_id) without threading the
// logger through every call.
package logging

import (
	"context"
	"log/slog"
)

type ctxKey struct{}

// WithLogger attaches a logger to ctx. Use once at request / job start;
// subsequent augmentations should go through With (which preserves the
// existing attrs).
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, logger)
}

// With returns a new ctx whose logger has the given attrs added.
// The original ctx's logger is not mutated.
func With(ctx context.Context, attrs ...slog.Attr) context.Context {
	logger := From(ctx)
	anyAttrs := make([]any, len(attrs))
	for i, a := range attrs {
		anyAttrs[i] = a
	}
	return WithLogger(ctx, logger.With(anyAttrs...))
}

// From returns the ctx logger, or slog.Default() if none was attached.
func From(ctx context.Context) *slog.Logger {
	if v, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok && v != nil {
		return v
	}
	return slog.Default()
}
