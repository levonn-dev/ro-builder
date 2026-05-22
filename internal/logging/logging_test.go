package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestFrom_ReturnsDefaultWhenNoLogger(t *testing.T) {
	got := From(context.Background())
	if got == nil {
		t.Fatalf("From returned nil; want a default logger")
	}
}

func TestWith_AttachesAttrsToCtxLogger(t *testing.T) {
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	root := slog.New(h)
	ctx := WithLogger(context.Background(), root)
	ctx = With(ctx, slog.String("request_id", "abc"))

	From(ctx).Info("hello")

	if !strings.Contains(buf.String(), "request_id=abc") {
		t.Fatalf("expected request_id=abc in log output, got: %s", buf.String())
	}
}

func TestWith_PreservesPriorAttrs(t *testing.T) {
	var buf bytes.Buffer
	root := slog.New(slog.NewTextHandler(&buf, nil))
	ctx := WithLogger(context.Background(), root)
	ctx = With(ctx, slog.String("a", "1"))
	ctx = With(ctx, slog.String("b", "2"))
	From(ctx).Info("hi")
	out := buf.String()
	if !strings.Contains(out, "a=1") || !strings.Contains(out, "b=2") {
		t.Fatalf("expected both attrs in output, got: %s", out)
	}
}
