// Command api runs the ro-builder HTTP service. The /generate path is
// asynchronous: POST enqueues a job and returns 202 + id; clients poll
// GET /generations/{id} for status and fetch GET /builds/{id} when the
// status is "completed".
//
// Required environment:
//
//	BUILDLIBRARY_PATH  - SQLite file path for the build library. The DB
//	                     holds the generations queue + saved trajectories.
//	                     Required; the binary refuses to start without it.
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
	"syscall"
	"time"

	"github.com/levonn-dev/ro-builder/configs"
	"github.com/levonn-dev/ro-builder/internal/api"
	"github.com/levonn-dev/ro-builder/internal/buildlibrary"
	"github.com/levonn-dev/ro-builder/internal/catalog"
	"github.com/levonn-dev/ro-builder/internal/domain"
	"github.com/levonn-dev/ro-builder/internal/llm"
	"github.com/levonn-dev/ro-builder/internal/llm/tools"
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
	libPath := os.Getenv("BUILDLIBRARY_PATH")
	if libPath == "" {
		return errors.New("BUILDLIBRARY_PATH is required")
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

	lib, err := buildlibrary.Open(libPath)
	if err != nil {
		return fmt.Errorf("buildlibrary open at %s: %w", libPath, err)
	}
	defer func() { _ = lib.Close() }()
	n, err := lib.RecoverOrphans(context.Background())
	if err != nil {
		return fmt.Errorf("recover orphans at startup: %w", err)
	}
	if n > 0 {
		logger.Warn("recovered orphaned generations", slog.Int("count", n))
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
		registry.Register(tools.NewScoreBuild(scoringClient))
		registry.Register(tools.NewLookupItem(cat))
		registry.Register(tools.NewSearchItems(cat))
		registry.Register(tools.NewLookupMonster(cat))
		registry.Register(tools.NewLookupSkill(cat))
		registry.Register(tools.NewListClassSkills(cat))
		registry.Register(tools.NewGetSimilarPastBuilds(lib))
		// submit_trajectory is intentionally NOT registered here; the
		// orchestrator constructs a per-request overlay version via
		// Registry.WithTool, wiring in the per-request Scoring / EvaluateGates /
		// Accept closures. See orchestrator.Generate.

		orch := orchestrator.New(provider, registry).
			WithProfiles(profiles).
			WithScoringClient(scoringClient).
			WithCatalog(cat).
			WithMaxIters(maxIters)

		pool = workers.New(workers.Config{
			Library:      lib,
			Runner:       orchestratorRunner{orch: orch},
			Save:         makeSaveCallback(lib, cat),
			Workers:      numWorkers,
			PollInterval: pollInterval,
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
// generation's id.
func makeSaveCallback(lib *buildlibrary.Library, cat *catalog.Catalog) workers.SaveCallback {
	return func(ctx context.Context, id string, req orchestrator.GenerateRequest, res *orchestrator.GenerateResult) error {
		if res == nil || res.Primary == nil {
			return errors.New("nil result; nothing to save")
		}
		in := buildlibrary.SaveInput{
			ID:             id,
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
		_, err := lib.SaveAndComplete(ctx, in)
		return err
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
