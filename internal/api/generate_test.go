package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/levonn-dev/ro-builder/internal/buildlibrary"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// newTestServer constructs a *Server with non-nil required deps for
// tests that don't exercise /score or catalog-backed endpoints.
// NewServer panics if either is nil; tests that need real behavior
// swap in their own client / catalog.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	return NewServer(scoring.NewClient("http://localhost:0", nil), loadCatalog(t))
}

type fakeEnqueuer struct {
	id       string
	err      error
	shutting bool
	timeout  int
	got      json.RawMessage
}

func (f *fakeEnqueuer) Enqueue(_ context.Context, request json.RawMessage) (string, error) {
	f.got = request
	if f.err != nil {
		return "", f.err
	}
	return f.id, nil
}
func (f *fakeEnqueuer) IsShuttingDown() bool        { return f.shutting }
func (f *fakeEnqueuer) ShutdownTimeoutSeconds() int { return f.timeout }

func postJSON(t *testing.T, srv *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	srv.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func getJSON(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	srv.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestGenerate_Returns202AndEnqueues(t *testing.T) {
	enq := &fakeEnqueuer{id: "abc123"}
	srv := newTestServer(t).WithEnqueuer(enq)
	rec := postJSON(t, srv, "/generate", `{"class":"novice"}`)
	if rec.Code != 202 {
		t.Fatalf("status: got %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	if body.Id != "abc123" {
		t.Fatalf("id: got %q, want abc123", body.Id)
	}
	if !strings.Contains(string(enq.got), `"class":"novice"`) {
		t.Fatalf("enqueued payload: %s", string(enq.got))
	}
}

func TestGenerate_429WhenQueueFull(t *testing.T) {
	enq := &fakeEnqueuer{err: ErrQueueFull}
	srv := newTestServer(t).WithEnqueuer(enq)
	rec := postJSON(t, srv, "/generate", `{"class":"novice"}`)
	if rec.Code != 429 {
		t.Fatalf("status: got %d, want 429", rec.Code)
	}
}

func TestGenerate_503WhenShuttingDown(t *testing.T) {
	enq := &fakeEnqueuer{shutting: true, timeout: 300}
	srv := newTestServer(t).WithEnqueuer(enq)
	rec := postJSON(t, srv, "/generate", `{"class":"novice"}`)
	if rec.Code != 503 {
		t.Fatalf("status: got %d, want 503", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "300" {
		t.Fatalf("Retry-After: got %q, want 300", got)
	}
}

func TestGenerate_503WhenNoEnqueuer(t *testing.T) {
	srv := newTestServer(t)
	rec := postJSON(t, srv, "/generate", `{"class":"novice"}`)
	if rec.Code != 503 {
		t.Fatalf("status: got %d, want 503", rec.Code)
	}
}

func TestGenerate_400OnValidationError(t *testing.T) {
	enq := &fakeEnqueuer{id: "x"}
	srv := newTestServer(t).WithEnqueuer(enq)
	rec := postJSON(t, srv, "/generate", `{"class":"novice","mode":"renewal"}`)
	if rec.Code != 400 {
		t.Fatalf("status: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestGenerate_400OnBadJSON verifies the framework-level JSON decoder
// returns 400 on malformed input before the handler runs. The handler's
// own decode branch (convertJSON in Generate) is dead in practice
// because the strict-server middleware pre-decodes; but kept defensively.
func TestGenerate_400OnBadJSON(t *testing.T) {
	enq := &fakeEnqueuer{id: "x"}
	srv := newTestServer(t).WithEnqueuer(enq)
	rec := postJSON(t, srv, "/generate", `{not json`)
	if rec.Code != 400 {
		t.Fatalf("status: got %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGenerationStatus_ReturnsCompletedWithBuildID(t *testing.T) {
	lib := openTempLibrary(t)
	ctx := context.Background()
	id, err := lib.Enqueue(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := lib.MarkCompleted(ctx, id); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	srv := newTestServer(t).WithLibrary(lib)
	rec := getJSON(t, srv, "/generations/"+id)
	if rec.Code != 200 {
		t.Fatalf("status: got %d; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Id      string  `json:"id"`
		Status  string  `json:"status"`
		BuildId *string `json:"build_id"`
		Error   *string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "completed" {
		t.Fatalf("status: got %q, want completed", body.Status)
	}
	if body.BuildId == nil || *body.BuildId != id {
		t.Fatalf("build_id: got %v, want %s", body.BuildId, id)
	}
	if body.Error != nil {
		t.Fatalf("error should be absent on completed, got %q", *body.Error)
	}
}

func TestGenerationStatus_ReturnsFailedWithGenericError(t *testing.T) {
	lib := openTempLibrary(t)
	ctx := context.Background()
	id, err := lib.Enqueue(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := lib.MarkFailed(ctx, id, buildlibrary.FailureMaxItersExhausted, "60 iters", nil); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	srv := newTestServer(t).WithLibrary(lib)
	rec := getJSON(t, srv, "/generations/"+id)
	if rec.Code != 200 {
		t.Fatalf("status: got %d; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Id      string  `json:"id"`
		Status  string  `json:"status"`
		BuildId *string `json:"build_id"`
		Error   *string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "failed" {
		t.Fatalf("status: got %q, want failed", body.Status)
	}
	if body.Error == nil || *body.Error != userFacingGenerationFailure {
		t.Fatalf("error: got %v, want exactly %q", body.Error, userFacingGenerationFailure)
	}
	// Operator detail (60 iters, max_iters_exhausted) MUST NOT leak.
	if strings.Contains(*body.Error, "max_iters") || strings.Contains(*body.Error, "60 iters") {
		t.Fatalf("operator detail leaked into user-facing error: %q", *body.Error)
	}
}

func TestGenerationStatus_NotFound(t *testing.T) {
	lib := openTempLibrary(t)
	srv := newTestServer(t).WithLibrary(lib)
	rec := getJSON(t, srv, "/generations/nonexistent")
	if rec.Code != 404 {
		t.Fatalf("status: got %d, want 404", rec.Code)
	}
}

// TestGenerationStatus_BuildIDResolves locks down the identity invariant
// in GetGeneration's doc comment: the build_id returned for a completed
// generation must resolve via GET /builds/{id}. Today they're equal by
// design (SaveAndComplete reuses the generation row's id as the saved
// trajectory's primary key); if that ever changes, this test fails and
// the assignment in GetGeneration has to change in lockstep.
func TestGenerationStatus_BuildIDResolves(t *testing.T) {
	lib := openTempLibrary(t)
	cat := loadCatalog(t)
	id := seedSavedBuild(t, lib)
	srv := buildsServer(t, lib, cat)

	genResp, err := http.Get(srv.URL + "/generations/" + id)
	if err != nil {
		t.Fatalf("GET /generations: %v", err)
	}
	defer genResp.Body.Close()
	if genResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /generations status: got %d, want 200", genResp.StatusCode)
	}
	var genBody struct {
		Status  string  `json:"status"`
		BuildId *string `json:"build_id"`
	}
	if err := json.NewDecoder(genResp.Body).Decode(&genBody); err != nil {
		t.Fatalf("decode /generations: %v", err)
	}
	if genBody.Status != "completed" {
		t.Fatalf("status: got %q, want completed", genBody.Status)
	}
	if genBody.BuildId == nil {
		t.Fatal("build_id missing on completed generation")
	}

	buildResp, err := http.Get(srv.URL + "/builds/" + *genBody.BuildId)
	if err != nil {
		t.Fatalf("GET /builds: %v", err)
	}
	defer buildResp.Body.Close()
	if buildResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /builds/%s status: got %d, want 200; build_id from /generations did not resolve",
			*genBody.BuildId, buildResp.StatusCode)
	}
}
