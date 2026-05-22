// Package workers runs the worker goroutine pool that consumes queued
// generations from the build library, drives the orchestrator's LLM
// tool-use loop, and writes results back. Owns the doorbell channel
// the API POST handler rings after enqueueing a new job, plus a
// fallback poll for defense-in-depth.
package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/levonn-dev/ro-builder/internal/buildlibrary"
	"github.com/levonn-dev/ro-builder/internal/logging"
	"github.com/levonn-dev/ro-builder/internal/orchestrator"
)

// Runner is the function the pool calls to actually generate. In
// production this is orchestrator.Orchestrator.Generate; tests inject
// a fake.
type Runner interface {
	Run(ctx context.Context, req orchestrator.GenerateRequest) (*orchestrator.GenerateResult, error)
}

// SaveCallback is invoked after a successful Generate to persist the
// trajectory to saved_trajectories. nil disables saving; useful in
// tests; production wires it.
type SaveCallback func(ctx context.Context, id string, req orchestrator.GenerateRequest, res *orchestrator.GenerateResult) error

type Config struct {
	Library      *buildlibrary.Library
	Runner       Runner
	Save         SaveCallback
	Workers      int           // default 1
	PollInterval time.Duration // default 5s
}

type Pool struct {
	cfg Config
	// doorbell is buffered with capacity 1 so Notify() can drop a
	// signal before any worker has scheduled into its select. Workers
	// consume it on first iteration; the buffer makes startup ordering
	// irrelevant. Do NOT make this unbuffered; the non-blocking send
	// in Notify() would silently drop signals when no receiver is
	// ready, and workers would only pick up jobs via the fallback poll.
	doorbell     chan struct{}
	pollCtx      context.Context
	cancelPoll   context.CancelFunc
	jobCtx       context.Context
	cancelJob    context.CancelFunc
	wg           sync.WaitGroup
	shuttingDown chan struct{}
}

func New(cfg Config) *Pool {
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	pollCtx, cancelPoll := context.WithCancel(context.Background())
	jobCtx, cancelJob := context.WithCancel(context.Background())
	return &Pool{
		cfg:          cfg,
		doorbell:     make(chan struct{}, 1),
		pollCtx:      pollCtx,
		cancelPoll:   cancelPoll,
		jobCtx:       jobCtx,
		cancelJob:    cancelJob,
		shuttingDown: make(chan struct{}),
	}
}

func (p *Pool) Start() {
	for i := 0; i < p.cfg.Workers; i++ {
		p.wg.Add(1)
		go p.runWorker(i)
	}
	p.Notify()
}

// Notify rings the doorbell. Used by the POST handler after enqueueing
// and by the cascade from workers that just claimed a job.
func (p *Pool) Notify() {
	select {
	case p.doorbell <- struct{}{}:
	default:
	}
}

// IsShuttingDown returns true once Shutdown has begun. POST handler
// uses this to return 503 instead of enqueueing.
func (p *Pool) IsShuttingDown() bool {
	select {
	case <-p.shuttingDown:
		return true
	default:
		return false
	}
}

// Shutdown stops claiming new work and waits up to timeout for in-flight
// jobs to finish. After timeout, the job context is cancelled and the
// in-flight LLM/sidecar calls abort.
func (p *Pool) Shutdown(timeout time.Duration) error {
	close(p.shuttingDown)
	p.cancelPoll()

	done := make(chan struct{})
	go func() { p.wg.Wait(); close(done) }()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		p.cancelJob()
		<-done
		return fmt.Errorf("shutdown timeout exceeded after %s; in-flight jobs aborted", timeout)
	}
}

func (p *Pool) runWorker(id int) {
	defer p.wg.Done()
	logger := slog.Default().With(slog.Int("worker_id", id))
	for {
		select {
		case <-p.pollCtx.Done():
			return
		case <-p.doorbell:
		case <-time.After(p.cfg.PollInterval):
		}

		g, err := p.cfg.Library.ClaimNext(p.jobCtx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			logger.Warn("claim failed", slog.String("error", err.Error()))
			// Short backoff so sustained SQLite lock contention doesn't
			// busy-spin and flood the log with one warn per attempt.
			time.Sleep(250 * time.Millisecond)
			continue
		}
		if g == nil {
			continue
		}
		p.Notify() // cascade; wake another idle worker
		p.runJob(logger, g)
	}
}

func (p *Pool) runJob(logger *slog.Logger, g *buildlibrary.Generation) {
	jobLog := logger.With(slog.String("generation_id", g.ID))
	jobCtx := logging.WithLogger(p.jobCtx, jobLog)

	// A panic in Runner.Run (LLM client, sidecar decode, nil-map deref)
	// otherwise leaves the row in 'running' forever. Recover, mark the
	// generation failed, and re-log the panic with its stack trace.
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		stack := debug.Stack()
		_ = p.cfg.Library.MarkFailed(context.Background(), g.ID,
			buildlibrary.FailureProviderError, "panic: "+fmt.Sprint(r), nil)
		jobLog.Error("panic in runJob",
			slog.Any("panic", r),
			slog.String("stack", string(stack)))
	}()

	var req orchestrator.GenerateRequest
	if err := json.Unmarshal(g.RequestJSON, &req); err != nil {
		_ = p.cfg.Library.MarkFailed(context.Background(), g.ID,
			buildlibrary.FailureValidationLate, "request_json malformed: "+err.Error(), nil)
		jobLog.Error("decode request_json", slog.String("error", err.Error()))
		return
	}

	jobLog.Info("job claimed")
	res, err := p.cfg.Runner.Run(jobCtx, req)
	if err != nil {
		reason := buildlibrary.FailureProviderError
		detail := err.Error()
		// *ValidationError is checked before *FailureError because the
		// orchestrator may return it from pre-loop boundary checks
		// (mode/playstyle/unknown server); without this branch it falls
		// through to provider_error, which is wrong.
		if verr, ok := errors.AsType[*orchestrator.ValidationError](err); ok {
			reason = buildlibrary.FailureValidationLate
			detail = verr.Error()
		} else if fe, ok := errors.AsType[*orchestrator.FailureError](err); ok {
			reason = buildlibrary.FailureReason(fe.Reason)
			detail = fe.Detail
		}
		if errors.Is(err, context.Canceled) && p.IsShuttingDown() {
			reason = buildlibrary.FailureShutdownInterrupt
			detail = "shutdown drain timeout exceeded"
		}
		// Use a non-cancelled ctx for the cleanup write; the jobCtx
		// may already be done.
		if mErr := p.cfg.Library.MarkFailed(context.Background(), g.ID, reason, detail, traceJSON(res)); mErr != nil && !errors.Is(mErr, buildlibrary.ErrAlreadyTerminal) {
			jobLog.Error("mark failed", slog.String("error", mErr.Error()))
		}
		jobLog.Warn("job failed", slog.String("reason", string(reason)), slog.String("detail", detail))
		return
	}

	if p.cfg.Save != nil {
		// Detached ctx for the cleanup write; jobCtx may already be done
		// on shutdown drain, and a cancelled Save would mark a completed
		// generation failed.
		//
		// Save is expected to call SaveAndComplete, which atomically writes
		// saved_trajectories AND marks the generation completed. Do not call
		// MarkCompleted separately; the transaction already did it.
		if err := p.cfg.Save(context.Background(), g.ID, req, res); err != nil {
			if mErr := p.cfg.Library.MarkFailed(context.Background(), g.ID,
				buildlibrary.FailureValidationLate, "save failed: "+err.Error(), traceJSON(res)); mErr != nil && !errors.Is(mErr, buildlibrary.ErrAlreadyTerminal) {
				jobLog.Error("mark failed (save error path)", slog.String("error", mErr.Error()))
			}
			jobLog.Error("save failed", slog.String("error", err.Error()))
			return
		}
		jobLog.Info("job completed")
		return
	}

	// Save is nil (tests or no-persist mode): mark completed directly.
	if err := p.cfg.Library.MarkCompleted(context.Background(), g.ID); err != nil {
		if !errors.Is(err, buildlibrary.ErrAlreadyTerminal) {
			jobLog.Error("mark completed", slog.String("error", err.Error()))
		}
		return
	}
	jobLog.Info("job completed")
}

// traceJSON marshals the trace (if any) on a result so MarkFailed can
// store it for forensics. Returns nil on no result or on marshal error.
func traceJSON(res *orchestrator.GenerateResult) json.RawMessage {
	if res == nil || len(res.Trace) == 0 {
		return nil
	}
	b, err := json.Marshal(res.Trace)
	if err != nil {
		return nil
	}
	return b
}
