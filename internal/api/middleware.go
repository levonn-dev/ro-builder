package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/levonn-dev/ro-builder/internal/logging"
)

type requestIDCtxKey struct{}

// requestIDFromContext returns the request id middleware attached to
// ctx via requestIDCtxKey, or "" if none.
func requestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDCtxKey{}).(string); ok {
		return v
	}
	return ""
}

// newRequestID returns a short hex token for request correlation. 64
// bits is sufficient; collisions across the volume any single API
// instance handles are astronomically rare.
func newRequestID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

// requestIDMiddleware generates a short hex id per request, stamps it
// on the ctx logger and on a typed context value, and logs the request
// line on exit with method, path, status, and duration.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		ctx := context.WithValue(r.Context(), requestIDCtxKey{}, id)
		ctx = logging.With(ctx, slog.String("request_id", id))

		start := time.Now()
		sw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r.WithContext(ctx))
		logging.From(ctx).Info("http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", sw.status),
			slog.Duration("duration", time.Since(start)),
		)
	})
}

// statusRecorder wraps an http.ResponseWriter to capture the status
// code written to it. Default is 200 because handlers that don't call
// WriteHeader still produce a 200 response. wroteHeader tracks whether
// WriteHeader has been called so the panic recovery middleware can tell
// "panicked before any response" from "panicked mid-stream."
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.wroteHeader = true
	s.ResponseWriter.WriteHeader(code)
}

// panicRecoveryMiddleware catches handler panics so the connection
// returns a clean 500 JSON body instead of net/http's default
// "log to stderr and silently drop the connection" behavior. Logs the
// panic value + stack via the ctx-attached request_id logger so the
// recovery is correlated with the request that triggered it.
func panicRecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if rec := recover(); rec != nil {
				logging.From(r.Context()).Error("handler panic",
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())))
				if !sw.wroteHeader {
					sw.Header().Set("Content-Type", "application/json")
					sw.WriteHeader(http.StatusInternalServerError)
					_, _ = sw.Write([]byte(`{"error":"` + genericInternalError + `"}`))
				}
			}
		}()
		next.ServeHTTP(sw, r)
	})
}
