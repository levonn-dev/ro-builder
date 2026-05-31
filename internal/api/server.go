// Package api hosts the HTTP server that satisfies the OpenAPI contract
// at api/openapi.yaml. The contract is the source of truth: handlers
// are methods on Server that implement oapi.StrictServerInterface, and
// every wire shape is described in the spec. Regenerate the bindings
// with `task oapi:gen` after editing the spec.
//
// The strict-server pattern means handlers return typed response objects
// (one Go type per status code); the generated middleware wires them
// through to net/http with the correct status and content-type. Returning
// a Go error from a handler maps to a 500 via the default error handler;
// for any other status, return the corresponding typed response.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/levonn-dev/ro-builder/internal/api/oapi"
	"github.com/levonn-dev/ro-builder/internal/buildlibrary"
	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/logging"
	"github.com/levonn-dev/ro-builder/internal/orchestrator"
	"github.com/levonn-dev/ro-builder/internal/scoring"
)

// genericInternalError is the user-facing string returned in 500 responses
// from library-backed endpoints. The actual error is logged server-side via
// slog with the handler name and request id so operators can correlate, but
// the wire response carries no SQLite paths, schema names, or constraint
// details. Pair every use with a slog.Error call that captures the same
// request context.
const genericInternalError = "internal server error"

// Enqueuer is the seam between the API server and the generation
// worker pool. POST /generate calls Enqueue; the implementation
// inserts a row into generations + rings the doorbell.
type Enqueuer interface {
	Enqueue(ctx context.Context, request json.RawMessage) (string, error)
	IsShuttingDown() bool
	ShutdownTimeoutSeconds() int
}

// ErrQueueFull is returned by an Enqueuer when the generation queue is
// at GENERATION_QUEUE_CAP. The Generate handler maps it to HTTP 429.
var ErrQueueFull = errors.New("generation queue at capacity")

// userFacingGenerationFailure is the exact string returned in
// GenerationStatus.Error when a generation lands in status=failed.
// The spec defines this as a fixed contract; no interpolation, no
// operator detail. Edit this constant only if the spec changes.
const userFacingGenerationFailure = "We couldn't generate a build matching your request. Try a different description."

// Server is the concrete implementation of oapi.StrictServerInterface.
// Construct with NewServer and mount onto an http.ServeMux via Mount().
//
// Optional dependencies (Library, Enqueuer) gate optional endpoints;
// /generate is unreachable without an Enqueuer (returns 503), /builds
// and /generations endpoints return "not configured" 500s without a
// Library. main.go constructs the Enqueuer only when LLM_API_KEY is
// set; without it /generate returns 503 while /score remains available.
// BUILDLIBRARY_PATH is required at startup, so the Library is always
// present in normal runs.
type Server struct {
	scoring  *scoring.Client
	enqueuer Enqueuer              // optional; nil disables /generate
	library  *buildlibrary.Library // optional; nil disables /builds + /generations
	catalog  *catalog.Catalog
	profiles map[string]*domain.ServerProfile // optional; when set, /generate rejects unknown server keys with 400
}

// NewServer constructs the API server. scoring and catalog are required;
// enqueuer and library are optional. Panics if scoring or catalog is nil;
// these are load-bearing dependencies the handlers dereference on
// every request, so failing fast at construction beats a NPE later.
func NewServer(s *scoring.Client, cat *catalog.Catalog) *Server {
	if s == nil {
		panic("api.NewServer: scoring is required")
	}
	if cat == nil {
		panic("api.NewServer: catalog is required")
	}
	return &Server{scoring: s, catalog: cat}
}

func (s *Server) WithEnqueuer(e Enqueuer) *Server {
	s.enqueuer = e
	return s
}

func (s *Server) WithLibrary(lib *buildlibrary.Library) *Server {
	s.library = lib
	return s
}

// WithProfiles supplies the server-profile registry so the /generate
// handler can reject unknown server keys with HTTP 400 at the boundary
// instead of accepting the job and failing it asynchronously.
func (s *Server) WithProfiles(p map[string]*domain.ServerProfile) *Server {
	s.profiles = p
	return s
}

// Mount installs the OpenAPI-derived routes onto mux. Pass-through for
// every operation defined in api/openapi.yaml; no other routes are
// installed by this call. *http.ServeMux satisfies oapi.ServeMux
// directly (HandleFunc + ServeHTTP).
//
// The custom error handlers ensure every failure response is a
// JSON ErrorBody; the strict server's default RequestErrorHandlerFunc
// uses http.Error which writes plain text, breaking the spec's
// "everything is JSON" promise.
func (s *Server) Mount(mux *http.ServeMux) {
	handler := oapi.NewStrictHandlerWithOptions(s, nil, oapi.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  jsonErrorHandler(http.StatusBadRequest),
		ResponseErrorHandlerFunc: jsonErrorHandler(http.StatusInternalServerError),
	})
	// Generated handler routes onto an inner mux; wrap the inner mux
	// with request-id + panic-recovery middleware before mounting on
	// the caller's mux at `/`. Order matters: requestIDMiddleware runs
	// first so the panic recovery's logger picks up the request_id
	// attribute from the ctx.
	inner := http.NewServeMux()
	oapi.HandlerFromMux(handler, inner)
	mux.Handle("/", requestIDMiddleware(panicRecoveryMiddleware(inner)))
}

// jsonErrorHandler returns a strict-server error handler that writes
// {"error": err.Error()} with the given status code and application/json
// content-type. Used for both request-decode failures (always 400) and
// response-write failures (treated as 500).
func jsonErrorHandler(status int) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, _ *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(oapi.ErrorBody{Error: err.Error()})
	}
}

// --- Score ---

func (s *Server) Score(ctx context.Context, req oapi.ScoreRequestObject) (oapi.ScoreResponseObject, error) {
	if req.Body == nil {
		return oapi.Score400JSONResponse{Error: "request body is required"}, nil
	}
	var (
		build    domain.Build
		scenario *domain.Scenario
	)
	if err := convertJSON(req.Body.Build, &build); err != nil {
		return oapi.Score400JSONResponse{Error: "build: " + err.Error()}, nil
	}
	if req.Body.Scenario != nil {
		scenario = &domain.Scenario{}
		if err := convertJSON(*req.Body.Scenario, scenario); err != nil {
			return oapi.Score400JSONResponse{Error: "scenario: " + err.Error()}, nil
		}
	}
	if err := build.Validate(); err != nil {
		return oapi.Score400JSONResponse{Error: err.Error()}, nil
	}

	scoreReq := build.ToScoreRequest(scenario, nil)
	resp, err := s.scoring.Score(ctx, scoreReq)
	if err != nil {
		return s.scoreErrorResponse(ctx, err)
	}

	var out oapi.ScoreResponse
	if err := convertJSON(resp, &out); err != nil {
		return nil, fmt.Errorf("encode score response: %w", err)
	}
	return oapi.Score200JSONResponse(out), nil
}

func (s *Server) scoreErrorResponse(ctx context.Context, err error) (oapi.ScoreResponseObject, error) {
	var sErr *scoring.Error
	if errors.As(err, &sErr) {
		if sErr.IsClient() {
			return oapi.Score400JSONResponse{Error: sErr.Message}, nil
		}
		// 5xx from the sidecar is upstream, not the caller's request.
		return oapi.Score502JSONResponse{Error: sErr.Message}, nil
	}
	// Non-scoring.Error is an internal failure (transport, decode, etc.).
	// Log the underlying error for operator correlation; the wire body
	// stays generic to avoid leaking Go type names, file paths, or
	// sidecar URLs into the client response.
	logging.From(ctx).Error("score: internal", slog.String("error", err.Error()))
	return oapi.Score500JSONResponse{Error: genericInternalError}, nil
}

// --- Generate ---

// Generate enqueues a build-generation job and returns its id. The
// LLM tool-use loop runs asynchronously in the worker pool; the caller
// polls GET /generations/{id} for status and eventually GET /builds/{id}
// for the result.
//
// Status codes:
//
//	202: job enqueued; response carries the id
//	400: request validation failed
//	429: queue at capacity
//	503: API is shutting down (Retry-After header set)
func (s *Server) Generate(ctx context.Context, req oapi.GenerateRequestObject) (oapi.GenerateResponseObject, error) {
	if s.enqueuer == nil {
		// No enqueuer means main.go was started without LLM_API_KEY.
		// Distinct from the shutting-down 503 below so operators (and
		// CI assertions) can tell the two cases apart.
		return oapi.Generate503JSONResponse{
			Body: oapi.ErrorBody{Error: "/generate is unavailable: LLM provider not configured (set LLM_API_KEY)"},
		}, nil
	}
	if s.enqueuer.IsShuttingDown() {
		return shutdown503Response(s.enqueuer.ShutdownTimeoutSeconds()), nil
	}
	if req.Body == nil {
		return oapi.Generate400JSONResponse{Error: "request body is required"}, nil
	}
	var genReq orchestrator.GenerateRequest
	if err := convertJSON(*req.Body, &genReq); err != nil {
		return oapi.Generate400JSONResponse{Error: "invalid request: " + err.Error()}, nil
	}
	if err := orchestrator.ValidateRequestWithProfiles(genReq, s.profiles); err != nil {
		return oapi.Generate400JSONResponse{Error: err.Error()}, nil
	}

	payload, err := json.Marshal(genReq)
	if err != nil {
		// Defensive: orchestrator.GenerateRequest is all primitive Go
		// fields today, so this branch is unreachable in practice. Kept
		// to keep the error path explicit so a future schema change that
		// introduces an unmarshalable field doesn't silently produce a
		// nil payload. 503 is the only typed internal-failure response
		// the OpenAPI spec defines for /generate; using it matches the
		// existing "all-other-enqueue-errors are internal → 503" pattern
		// below.
		logging.From(ctx).Error("Generate: marshal request",
			slog.String("error", err.Error()))
		return shutdown503Response(s.enqueuer.ShutdownTimeoutSeconds()), nil
	}
	id, err := s.enqueuer.Enqueue(ctx, payload)
	if err != nil {
		if errors.Is(err, ErrQueueFull) {
			return oapi.Generate429JSONResponse{Error: "queue full, retry later"}, nil
		}
		// All other enqueue errors are internal; 503 is the closest match.
		return shutdown503Response(s.enqueuer.ShutdownTimeoutSeconds()), nil
	}
	return oapi.Generate202JSONResponse{Id: id}, nil
}

// shutdown503Response returns a Generate503JSONResponse with the Retry-After
// header set when timeout > 0. The generated type carries the header natively.
func shutdown503Response(timeoutSeconds int) oapi.Generate503JSONResponse {
	resp := oapi.Generate503JSONResponse{
		Body: oapi.ErrorBody{Error: "API is shutting down; please retry."},
	}
	if timeoutSeconds > 0 {
		resp.Headers.RetryAfter = &timeoutSeconds
	}
	return resp
}

// --- GetGeneration ---

// GetGeneration returns the typed projection of one generations row:
// status, timestamps, plus build_id when completed or a generic error
// when failed. No trace, no operator-facing failure detail.
//
// Identity invariant: the build_id returned for a completed generation
// is the generation's own id. SaveAndComplete reuses the generation row's
// id as the saved_trajectories primary key (the FK in saved_trajectories
// references generations.id), so the two are by-design the same value.
// Callers GET /builds/{id} with this id and it resolves. If a future
// migration ever splits the two tables onto distinct ids, this assignment
// has to change in lockstep; see TestGenerationStatus_BuildIDResolves.
//
// Status codes:
//
//	200: row found; body is GenerationStatus
//	404: no generation with that id
//	500: library not configured or internal error
func (s *Server) GetGeneration(ctx context.Context, req oapi.GetGenerationRequestObject) (oapi.GetGenerationResponseObject, error) {
	if s.library == nil {
		return oapi.GetGeneration500JSONResponse{Error: "build library not configured"}, nil
	}
	g, err := s.library.GetGeneration(ctx, req.Id)
	if errors.Is(err, buildlibrary.ErrGenerationNotFound) {
		return oapi.GetGeneration404JSONResponse{Error: "no generation with id " + req.Id}, nil
	}
	if err != nil {
		logging.From(ctx).Error("GetGeneration: library read failed",
			slog.String("id", req.Id),
			slog.String("error", err.Error()))
		return oapi.GetGeneration500JSONResponse{Error: genericInternalError}, nil
	}
	resp := oapi.GenerationStatus{
		Id:        g.ID,
		Status:    oapi.GenerationStatusStatus(g.Status),
		CreatedAt: g.CreatedAt,
	}
	if g.StartedAt != nil {
		resp.StartedAt = g.StartedAt
	}
	if g.CompletedAt != nil {
		resp.CompletedAt = g.CompletedAt
	}
	if g.Status == buildlibrary.GenStatusCompleted {
		// build_id == generation id by the identity invariant in this
		// function's doc comment.
		bid := g.ID
		resp.BuildId = &bid
	}
	if g.Status == buildlibrary.GenStatusFailed {
		// The user never sees gate/operator detail. Always exactly this
		// string; see the spec's "User-visible error string" contract.
		msg := userFacingGenerationFailure
		resp.Error = &msg
	}
	return oapi.GetGeneration200JSONResponse(resp), nil
}

// --- ListBuilds ---

func (s *Server) ListBuilds(ctx context.Context, req oapi.ListBuildsRequestObject) (oapi.ListBuildsResponseObject, error) {
	if s.library == nil {
		return oapi.ListBuilds500JSONResponse{Error: "build library not configured"}, nil
	}
	p := buildlibrary.FindParams{}
	if req.Params.Class != nil {
		p.Class = *req.Params.Class
	}
	if req.Params.Server != nil {
		p.Server = *req.Params.Server
	}
	if req.Params.Limit != nil {
		// Spec already enforces minimum=1 / maximum=50 via the generated
		// param parser; passing through is enough.
		p.Limit = *req.Params.Limit
	}

	builds, err := s.library.Find(ctx, p)
	if err != nil {
		logging.From(ctx).Error("ListBuilds: library find failed",
			slog.String("class", p.Class),
			slog.String("server", p.Server),
			slog.Int("limit", p.Limit),
			slog.String("error", err.Error()))
		return oapi.ListBuilds500JSONResponse{Error: genericInternalError}, nil
	}

	out := oapi.ListBuildsResponse{Builds: make([]oapi.BuildSummary, 0, len(builds))}
	for _, b := range builds {
		var summary oapi.BuildSummary
		if err := convertJSON(b, &summary); err != nil {
			return nil, fmt.Errorf("encode summary %s: %w", b.ID, err)
		}
		out.Builds = append(out.Builds, summary)
	}
	return oapi.ListBuilds200JSONResponse(out), nil
}

// --- GetBuild ---

func (s *Server) GetBuild(ctx context.Context, req oapi.GetBuildRequestObject) (oapi.GetBuildResponseObject, error) {
	if s.library == nil {
		return oapi.GetBuild500JSONResponse{Error: "build library not configured"}, nil
	}
	if req.Id == "" {
		return oapi.GetBuild400JSONResponse{Error: "id is required"}, nil
	}

	st, err := s.library.Get(ctx, req.Id)
	if errors.Is(err, buildlibrary.ErrNotFound) {
		return oapi.GetBuild404JSONResponse{Error: "no trajectory with id " + req.Id}, nil
	}
	if err != nil {
		logging.From(ctx).Error("GetBuild: library read failed",
			slog.String("id", req.Id),
			slog.String("error", err.Error()))
		return oapi.GetBuild500JSONResponse{Error: genericInternalError}, nil
	}

	enriched := enrichSavedTrajectory(st, s.catalog)
	var out oapi.EnrichedBuild
	if err := convertJSON(enriched, &out); err != nil {
		return nil, fmt.Errorf("encode enriched build: %w", err)
	}
	return oapi.GetBuild200JSONResponse(out), nil
}

// convertJSON moves data between two JSON-shape-compatible Go types
// (e.g., scoring.ScoreResponse and oapi.ScoreResponse) by round-tripping
// through json.Marshal/Unmarshal. The spec was designed so each oapi
// type matches the JSON tags of its domain counterpart exactly; if a
// field shape diverges, this conversion fails loudly rather than
// silently dropping data.
func convertJSON(in, out any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	return nil
}
