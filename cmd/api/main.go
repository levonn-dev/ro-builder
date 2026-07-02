// Command api runs the ro-builder HTTP service. The /generate path is
// asynchronous: POST enqueues a job and returns 202 + id; clients poll
// GET /generations/{id} for status and fetch GET /builds/{id} when the
// status is "completed".
//
// Required environment:
//
//	DATABASE_URL  - PostgreSQL connection string for the build library
//	               (e.g. postgres://user:pass@host:5432/robuilder?sslmode=disable).
//	               Holds the generations queue + saved trajectories.
//	               Required; the binary refuses to start without it.
//
// Optional environment:
//
//	LLM_API_KEY                     - Anthropic credential (or whichever
//	                                  provider is registered). Required
//	                                  for /generate; if unset, /generate
//	                                  returns 503 and /score still works.
//	ADDR                            - listen address (default ":8080")
//	SIDECAR_URL                     - calc-sidecar root (default "http://localhost:7401")
//	LLM_PROVIDER                    - LLM backend name (default "anthropic")
//	LLM_MODEL / LLM_BASE_URL / LLM_TIMEOUT_SECONDS - provider config
//	GENERATION_WORKERS              - worker goroutines (default 1)
//	GENERATION_QUEUE_CAP            - max queued+running (default 16)
//	GENERATION_POLL_INTERVAL        - worker fallback poll cadence (default 5s)
//	GENERATION_SHUTDOWN_TIMEOUT     - drain window on SIGTERM/SIGINT (default 5m)
//	GENERATION_MAX_ITERS            - orchestrator tool-use cap (default 60)
//	GENERATION_LEASE_TTL            - job lease duration (default 90s)
//	GENERATION_LEASE_SWEEP_INTERVAL - expired-lease sweep cadence (default 30s)
//	GENERATION_MAX_ATTEMPTS         - requeue cap; 0 disables retry (default 0)
//	EMBEDDING_BASE_URL              - OpenAI-compatible /v1 root; presence
//	                                  enables RAG over saved builds (pgvector).
//	                                  Unset = recency-only, no pgvector needed.
//	EMBEDDING_MODEL / EMBEDDING_DIM - embedder model id + vector dimension
//	                                  (required when EMBEDDING_BASE_URL is set)
//	EMBEDDING_API_KEY               - optional bearer for cloud embedders
//	EMBEDDING_SEED_MAX_DISTANCE     - Tier A proactive-seed ceiling (default 0.15)
//	EMBEDDING_SIMILAR_MAX_DISTANCE  - Tier B list floor for get_similar_past_builds (default 0.5)
//	EMBEDDING_HNSW_M / _EF_CONSTRUCTION / _EF_SEARCH - HNSW index tuning (0 = pgvector default)
//	LOG_LEVEL                       - debug|info|warn|error (default info)
//	LOG_FORMAT                      - text|json (default text)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/levonn-dev/ro-builder/configs"
	"github.com/levonn-dev/ro-builder/internal/api"
	"github.com/levonn-dev/ro-builder/internal/buildlibrary"
	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/embedding"
	"github.com/levonn-dev/ro-builder/internal/llm"
	"github.com/levonn-dev/ro-builder/internal/llm/tools"
	"github.com/levonn-dev/ro-builder/internal/logging"
	"github.com/levonn-dev/ro-builder/internal/orchestrator"
	"github.com/levonn-dev/ro-builder/internal/scoring"
	"github.com/levonn-dev/ro-builder/internal/workers"

	_ "github.com/levonn-dev/ro-builder/internal/llm/anthropic"
	_ "github.com/levonn-dev/ro-builder/internal/llm/openai"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ro-builder API:", err)
		os.Exit(1)
	}
}

func run() error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}

	logger := buildLogger()
	slog.SetDefault(logger)

	addr := envOr("ADDR", ":8080")
	sidecarURL := envOr("SIDECAR_URL", "http://localhost:7401")
	numWorkers := envInt("GENERATION_WORKERS", 1)
	queueCap := envInt("GENERATION_QUEUE_CAP", 16)
	if queueCap <= 0 {
		return fmt.Errorf("GENERATION_QUEUE_CAP must be > 0, got %d", queueCap)
	}
	pollInterval := envDuration("GENERATION_POLL_INTERVAL", 5*time.Second)
	shutdownTimeout := envDuration("GENERATION_SHUTDOWN_TIMEOUT", 5*time.Minute)
	maxIters := envInt("GENERATION_MAX_ITERS", 60)

	scoringClient := scoring.NewClient(sidecarURL, &http.Client{Timeout: 30 * time.Second})

	cat, err := catalog.Load()
	if err != nil {
		return fmt.Errorf("catalog load failed (run `task catalog` to regenerate): %w", err)
	}
	logger.Info("catalog loaded",
		slog.Int("items", cat.ItemCount()),
		slog.Int("mobs", cat.MobCount()),
		slog.Int("skills", cat.SkillCount()))

	profiles, err := configs.LoadAllServerProfiles()
	if err != nil {
		return fmt.Errorf("server profile load: %w", err)
	}
	profileKeys := make([]string, 0, len(profiles))
	for k := range profiles {
		profileKeys = append(profileKeys, k)
	}
	logger.Info("server profiles loaded", slog.Any("profiles", profileKeys))

	embCfg := embedding.LoadConfigFromEnv()
	if err := embCfg.Validate(); err != nil {
		return fmt.Errorf("embedding config: %w", err)
	}
	var embedder *embedding.Client
	modelID := embCfg.Model + "@" + strconv.Itoa(embCfg.Dim)
	if embCfg.Enabled() {
		embedder = embedding.New(embCfg)
		logger.Info("embeddings enabled", slog.String("model_id", modelID), slog.Int("dim", embedder.Dimensions()))
	} else {
		logger.Info("embeddings disabled (EMBEDDING_BASE_URL unset); retrieval is recency-only")
	}

	// Retrieval tuning knobs (semantic mode only). similarMaxDist is the
	// Tier B list floor for get_similar_past_builds; the HNSW knobs tune the
	// index (0 = pgvector default: m=16, ef_construction=64, ef_search=40).
	similarMaxDist := envFloat("EMBEDDING_SIMILAR_MAX_DISTANCE", 0.5)
	hnswM := envInt("EMBEDDING_HNSW_M", 0)
	hnswEfConstruction := envInt("EMBEDDING_HNSW_EF_CONSTRUCTION", 0)
	hnswEfSearch := envInt("EMBEDDING_HNSW_EF_SEARCH", 0)

	var openOpts []buildlibrary.Option
	if embCfg.Enabled() {
		openOpts = append(openOpts, buildlibrary.WithEmbedding(embCfg.Dim, modelID))
		if hnswM > 0 || hnswEfConstruction > 0 || hnswEfSearch > 0 {
			openOpts = append(openOpts, buildlibrary.WithHNSW(hnswM, hnswEfConstruction, hnswEfSearch))
		}
	}
	lib, err := buildlibrary.Open(context.Background(), dsn, openOpts...)
	if err != nil {
		return fmt.Errorf("buildlibrary open: %w", err)
	}
	defer func() { _ = lib.Close() }()
	if embCfg.Enabled() {
		if lib.SemanticEnabled() {
			logger.Info("semantic retrieval active", slog.String("model_id", modelID), slog.Int("dim", lib.EmbeddingDim()))
		} else {
			logger.Warn("semantic retrieval degraded to recency-only (EMBEDDING_BASE_URL set but the pgvector bootstrap did not complete; see earlier embedding errors)", slog.String("model_id", modelID))
		}
	}

	maxAttempts := envInt("GENERATION_MAX_ATTEMPTS", 0)
	rq, fl, err := lib.RecoverExpiredLeases(context.Background(), maxAttempts)
	if err != nil {
		return fmt.Errorf("recover expired leases at startup: %w", err)
	}
	if rq > 0 || fl > 0 {
		logger.Warn("recovered expired leases at startup",
			slog.Int("requeued", rq), slog.Int("failed", fl))
	}

	llmCfg := llm.LoadConfigFromEnv()
	var provider llm.Provider
	if llmCfg.APIKey != "" {
		p, err := llm.New(llmCfg)
		if err != nil {
			return fmt.Errorf("llm provider init: %w", err)
		}
		provider = p
	}
	logLLMConfig(logger, llmCfg, maxIters)

	server := api.NewServer(scoringClient, cat).
		WithLibrary(lib).
		WithProfiles(profiles)

	// /generate requires an LLM provider; without one, build the API
	// without an enqueuer so the handler returns 503. /score remains
	// fully operational.
	var pool *workers.Pool
	if provider != nil {
		registry := tools.NewRegistry()
		registry.Register(tools.NewScoreBuild(scoringClient, cat))
		registry.Register(tools.NewScoreBuilds(scoringClient, cat))
		registry.Register(tools.NewLookupItem(cat))
		registry.Register(tools.NewSearchItems(cat))
		registry.Register(tools.NewLookupMonster(cat))
		registry.Register(tools.NewLookupSkill(cat))
		registry.Register(tools.NewListClassSkills(cat))
		registry.Register(tools.NewListClassBuffs(cat))
		registry.Register(tools.NewGetSimilarPastBuilds(lib, similarMaxDist))
		registry.Register(tools.NewGetSavedBuild(lib))
		// submit_trajectory is intentionally NOT registered here; the
		// orchestrator constructs a per-request overlay version via
		// Registry.WithTool, wiring in the per-request Scoring / EvaluateGates /
		// Accept closures. See orchestrator.Generate.

		orch := orchestrator.New(provider, registry).
			WithProfiles(profiles).
			WithScoringClient(scoringClient).
			WithCatalog(cat).
			WithMaxIters(maxIters)
		if embedder != nil {
			orch = orch.WithEmbedder(embedder).WithSeeder(lib).
				WithSeedMaxDistance(envFloat("EMBEDDING_SEED_MAX_DISTANCE", 0.15))
		}

		leaseTTL := envDuration("GENERATION_LEASE_TTL", 90*time.Second)
		sweepInterval := envDuration("GENERATION_LEASE_SWEEP_INTERVAL", 30*time.Second)

		pool = workers.New(workers.Config{
			Library:       lib,
			Runner:        orchestratorRunner{orch: orch},
			Save:          makeSaveCallback(lib, cat, embedder),
			Workers:       numWorkers,
			PollInterval:  pollInterval,
			LeaseTTL:      leaseTTL,
			SweepInterval: sweepInterval,
			MaxAttempts:   maxAttempts,
		})
		pool.Start()

		enqueuer := &apiEnqueuer{lib: lib, pool: pool, cap: queueCap, shutdownTimeout: shutdownTimeout}
		server = server.WithEnqueuer(enqueuer)
	} else {
		logger.Warn("LLM_API_KEY unset; /generate disabled (returns 503), /score remains available")
	}

	readyzClient := &http.Client{Timeout: readyzCheckTimeout}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/readyz", readyzHandler(sidecarURL, readyzClient, lib))
	server.Mount(mux)

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	srvErr := make(chan error, 1)
	go func() {
		logger.Info("ro-builder API listening",
			slog.String("addr", addr),
			slog.String("sidecar", sidecarURL),
			slog.Int("workers", numWorkers),
			slog.Int("queue_cap", queueCap),
			slog.Duration("shutdown_timeout", shutdownTimeout))
		srvErr <- httpSrv.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-srvErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case sig := <-signals:
		logger.Info("shutdown signal received", slog.String("signal", sig.String()))
		// Drain workers first; GET endpoints stay live so clients can finish
		// polling their in-flight generations. After the pool fully drains,
		// close HTTP with a brief grace period for any tail-end requests.
		// Pool is nil when LLM_API_KEY was unset at startup.
		if pool != nil {
			if err := pool.Shutdown(shutdownTimeout); err != nil {
				logger.Warn("pool drain", slog.String("error", err.Error()))
			}
		}
		httpCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(httpCtx); err != nil {
			logger.Warn("http shutdown", slog.String("error", err.Error()))
		}
		return nil
	}
}

// orchestratorRunner adapts *orchestrator.Orchestrator to the
// workers.Runner interface.
type orchestratorRunner struct {
	orch *orchestrator.Orchestrator
}

func (r orchestratorRunner) Run(ctx context.Context, req orchestrator.GenerateRequest) (*orchestrator.GenerateResult, error) {
	return r.orch.Generate(ctx, req)
}

// apiEnqueuer adapts buildlibrary + pool to the api.Enqueuer interface.
// Enforces the queue cap and rings the doorbell after a successful
// insert.
type apiEnqueuer struct {
	lib             *buildlibrary.Library
	pool            *workers.Pool
	cap             int
	shutdownTimeout time.Duration
}

func (e *apiEnqueuer) Enqueue(ctx context.Context, request json.RawMessage) (string, error) {
	id, err := e.lib.EnqueueIfUnderCap(ctx, e.cap, request)
	if err != nil {
		if errors.Is(err, buildlibrary.ErrQueueAtCapacity) {
			return "", api.ErrQueueFull
		}
		return "", err
	}
	e.pool.Notify()
	return id, nil
}

func (e *apiEnqueuer) IsShuttingDown() bool        { return e.pool.IsShuttingDown() }
func (e *apiEnqueuer) ShutdownTimeoutSeconds() int { return int(e.shutdownTimeout.Seconds()) }

// makeSaveCallback returns a workers.SaveCallback that persists a
// successful generation to the saved_trajectories table keyed to the
// generation's id. When embedder is non-nil the request+answer document is
// embedded best-effort: a failure is logged and the save continues without a
// vector rather than blocking persistence.
func makeSaveCallback(lib *buildlibrary.Library, cat *catalog.Catalog, embedder *embedding.Client) workers.SaveCallback {
	return func(ctx context.Context, id, owner string, req orchestrator.GenerateRequest, res *orchestrator.GenerateResult) error {
		if res == nil || res.Primary == nil {
			return errors.New("nil result; nothing to save")
		}
		in := buildlibrary.SaveInput{
			ID:             id,
			Owner:          owner,
			Class:          req.Class,
			Server:         req.Server,
			Playstyle:      req.Playstyle,
			Mode:           req.Mode,
			Description:    req.Description,
			Primary:        *res.Primary,
			Alternatives:   res.Alternatives,
			FinalText:      res.Final,
			CalcVersion:    extractCalcVersion(res.Primary),
			CatalogVersion: cat.Version(),
		}
		if embedder != nil && lib.SemanticEnabled() {
			doc := strings.TrimSpace(req.Playstyle + "\n" + req.Description + "\n" + res.Final)
			if vecs, err := embedder.Embed(ctx, []string{doc}); err != nil {
				logging.From(ctx).Warn("save: document embed failed; storing without vector", slog.Any("err", err))
			} else if len(vecs) > 0 {
				in.Embedding = vecs[0]
				logging.From(ctx).Info("save: document embedded",
					slog.Int("dim", len(vecs[0])), slog.Int("doc_chars", len(doc)))
			}
		}
		if _, err := lib.SaveAndComplete(ctx, in); err != nil {
			return err
		}
		logging.From(ctx).Info("build saved to library",
			slog.String("id", id),
			slog.Bool("embedded", in.Embedding != nil),
			slog.Int("embedding_dim", len(in.Embedding)),
			slog.String("class", req.Class),
			slog.String("server", req.Server),
			slog.Int("snapshots", len(res.Primary.Snapshots)))
		return nil
	}
}

// extractCalcVersion scans the trajectory's snapshots for the first
// non-empty CalcVersion stamp.
func extractCalcVersion(t *domain.Trajectory) string {
	if t == nil {
		return ""
	}
	for _, s := range t.Snapshots {
		if s.Score != nil && s.Score.CalcVersion != "" {
			return s.Score.CalcVersion
		}
	}
	return ""
}

// logLLMConfig emits the resolved LLM configuration at startup. The API
// key is never logged, only its presence, so the line is safe in any
// log aggregator. Empty / zero fields are surfaced as "(default)" so an
// operator reading the log can tell at a glance whether they're hitting
// cloud Anthropic, a local OpenAI-compatible server, or something
// proxied. Defaults documented in internal/llm/config.go.
func logLLMConfig(logger *slog.Logger, c llm.Config, maxIters int) {
	provider := c.Provider
	if provider == "" {
		provider = "anthropic (default)"
	}
	model := c.Model
	if model == "" {
		model = "(provider default)"
	}
	baseURL := c.BaseURL
	if baseURL == "" {
		baseURL = "(provider default)"
	}
	maxTokens := "(provider default)"
	if c.MaxTokens > 0 {
		maxTokens = strconv.Itoa(c.MaxTokens)
	}
	logger.Info("llm configured",
		slog.String("provider", provider),
		slog.String("model", model),
		slog.String("base_url", baseURL),
		slog.Bool("api_key_set", c.APIKey != ""),
		slog.Duration("timeout", c.Timeout),
		slog.String("max_tokens", maxTokens),
		slog.Int("max_iters", maxIters),
		slog.Any("registered_providers", llm.ListProviders()))
}

func buildLogger() *slog.Logger {
	level := envLevel("LOG_LEVEL", slog.LevelInfo)
	format := envOr("LOG_FORMAT", "text")
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	}
	return slog.New(handler)
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func envInt(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return d
}

func envDuration(k string, d time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if dd, err := time.ParseDuration(v); err == nil {
			return dd
		}
	}
	return d
}

func envFloat(k string, def float64) float64 {
	if v := os.Getenv(k); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envLevel(k string, d slog.Level) slog.Level {
	switch envOr(k, "") {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	return d
}
