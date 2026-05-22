package api

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/logging"
)

func TestRequestIDMiddleware_AttachesIDToCtx(t *testing.T) {
	var seenID string
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seenID = requestIDFromContext(r.Context())
	})
	wrapped := requestIDMiddleware(handler)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if seenID == "" {
		t.Fatalf("request_id not attached to ctx")
	}
}

func TestRequestIDMiddleware_LoggerCarriesID(t *testing.T) {
	// Capture log output via a per-test handler attached to ctx.
	var buf bytes.Buffer
	root := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		logging.From(r.Context()).Info("hello-from-handler")
	})

	// Wrap handler so the middleware can read a pre-seeded ctx logger.
	wrapped := requestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := logging.WithLogger(r.Context(), root)
		handler.ServeHTTP(w, r.WithContext(ctx))
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	wrapped.ServeHTTP(httptest.NewRecorder(), req)

	// The middleware should have augmented the ctx logger with
	// request_id before the inner handler ran; but it ran BEFORE we
	// re-seeded the logger above. So this test specifically verifies
	// the augmentation is on the OUTER ctx; the inner test (above)
	// already verified request_id is in the ctx-value.
	// Easier assertion: re-seed logger first then run middleware.
	buf.Reset()
	// Approach: replace the root logger on the slog default and run.
	prev := slog.Default()
	slog.SetDefault(root)
	t.Cleanup(func() { slog.SetDefault(prev) })

	wrapped2 := requestIDMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		logging.From(r.Context()).Info("inner")
	}))
	wrapped2.ServeHTTP(httptest.NewRecorder(), req)
	if !strings.Contains(buf.String(), "request_id=") {
		t.Fatalf("expected request_id in log output, got: %s", buf.String())
	}
	_ = context.Background()
}

func TestRequestIDMiddleware_LogsRequestLine(t *testing.T) {
	var buf bytes.Buffer
	root := slog.New(slog.NewTextHandler(&buf, nil))
	prev := slog.Default()
	slog.SetDefault(root)
	t.Cleanup(func() { slog.SetDefault(prev) })

	wrapped := requestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(204)
	}))
	req := httptest.NewRequest(http.MethodGet, "/abc", nil)
	wrapped.ServeHTTP(httptest.NewRecorder(), req)
	out := buf.String()
	for _, want := range []string{"method=GET", "path=/abc", "status=204"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in log output: %s", want, out)
		}
	}
}

func TestPanicRecoveryMiddleware_Returns500JSON(t *testing.T) {
	var buf bytes.Buffer
	root := slog.New(slog.NewTextHandler(&buf, nil))
	prev := slog.Default()
	slog.SetDefault(root)
	t.Cleanup(func() { slog.SetDefault(prev) })

	wrapped := panicRecoveryMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	}))
	req := httptest.NewRequest(http.MethodGet, "/explode", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content-type: got %q, want application/json", got)
	}
	if !strings.Contains(rec.Body.String(), genericInternalError) {
		t.Errorf("body should carry generic error string; got %s", rec.Body.String())
	}
	if !strings.Contains(buf.String(), "handler panic") {
		t.Errorf("expected panic log line; got %s", buf.String())
	}
}
