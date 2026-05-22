// ShimPool; fixed-size pool of worker threads, each owning its own
// rocalc shim instance. Provides the only safe path for concurrent
// score() execution.
//
// Why this exists: rocalc + jsdom together hold their state in
// thread-global variables (`m_Item`, `n_A_STR`, etc. live on the jsdom
// window). Two concurrent score() calls in the SAME thread will
// interleave through that state and silently corrupt each other.
// Worker threads have isolated V8 heaps, so each pool worker holds an
// independent shim and concurrent requests served by different workers
// are naturally safe.
//
// Design choices:
//   - **Eager spawn at construction.** Each worker pays a ~1.5s
//     createShim() cost on its first request. Spawning eagerly means
//     that cost is paid at server boot, not on the first request a user
//     happens to land on a cold worker. The constructor returns
//     immediately; the workers warm up async.
//   - **No class affinity.** Round-robin / first-idle dispatch. The
//     in-thread `lastClass` cache is per-worker now, so back-to-back
//     same-class requests can land on different workers and re-pay the
//     ~250ms ClickJob. Currently acceptable; class affinity (sticky
//     LRU by req.class) is the optimization to revisit if telemetry
//     shows class thrashing under load.
//   - **Crash → respawn.** If a worker emits 'error' or exits unexpectedly,
//     the pool rejects the in-flight job for that worker and spawns a
//     replacement. The shim's own resetScoreShim() handles non-validation
//     errors that don't kill the worker.
//
// Wire protocol matches src/worker.ts; see that file for message shapes.

import { Worker } from "node:worker_threads";
import type { ScoreRequest, ScoreResponse } from "./types.ts";

const WORKER_URL = new URL("./worker.ts", import.meta.url);

// PoolWorker tracks one worker's lifecycle. busy is the assignment
// flag; currentJob is the id of the job the worker is currently
// processing (undefined when idle), used to fail-over the right job
// on a worker crash. recoveryAttempts counts crash-respawn cycles
// since the last successful warmup; the circuit breaker in
// handleWorkerCrash refuses to respawn past MAX_RECOVERY_ATTEMPTS so
// a persistently-crashing worker can't loop forever and keep ready()
// pinned at false.
interface PoolWorker {
  worker: Worker;
  busy: boolean;
  currentJob: number | undefined;
  recoveryAttempts: number;
}

// MAX_RECOVERY_ATTEMPTS caps the per-slot crash-respawn cycles before
// the circuit breaker trips. Three is enough to ride out transient
// failures (transient jsdom alloc spike, etc.) but small enough that a
// hard bug surfaces fast instead of looping silently.
const MAX_RECOVERY_ATTEMPTS = 3;

interface Pending {
  resolve: (r: ScoreResponse) => void;
  reject: (e: Error) => void;
}

// WorkerMessage mirrors src/worker.ts's outbound shape. Kept loose
// (validation: boolean rather than a discriminated union) to keep the
// switch in handleMessage tight.
interface WorkerMessage {
  id: number;
  ok: boolean;
  result?: ScoreResponse;
  message?: string;
  validation?: boolean;
}

// PoolValidationError surfaces the worker-side validation classification
// across the thread boundary. server.ts's isValidationError() recognizes
// it via the .validation flag so 400-vs-500 classification stays
// correct after the structured-clone round-trip.
export class PoolValidationError extends Error {
  readonly validation = true;

  constructor(message: string) {
    super(message);
    this.name = "PoolValidationError";
  }
}

// Waiter is one queued acquire(); both the resolve and reject sides are
// retained so terminate() can fail-fast queued requests instead of
// leaving them hanging forever.
interface Waiter {
  resolve: (slot: PoolWorker) => void;
  reject: (err: Error) => void;
}

export class ShimPool {
  private readonly workers: PoolWorker[] = [];
  private readonly waiting: Waiter[] = [];
  private readonly pending = new Map<number, Pending>();
  private nextId = 0;
  private shuttingDown = false;
  // Two independent gates compose into ready().
  //
  //  - _initialWarmupCompleted: flips true once the constructor's
  //    Promise.allSettled chain finishes AND at least one warmup
  //    fulfilled. The "at least one" rule accepts partial bootstrap:
  //    if 1 of N workers warmed, the pod can still serve; only a total
  //    warmup failure (e.g., missing vendor/rocalc) keeps /healthz at
  //    503. Total-failure pods would otherwise advertise ready=200 and
  //    500 every request; defeating the readiness probe.
  //
  //  - _recoveryInProgress: count of in-flight crash-recovery warmups.
  //    When >0, ready() flips to false. handleWorkerCrash spawns a
  //    cold replacement and increments; the warmup completion
  //    decrements. Without this gate, /healthz would stay 200 while a
  //    cold replacement was in the pool; the LB would route a request
  //    to the cold slot and pay ~1.5s createShim on what looked like a
  //    healthy pod.
  private _initialWarmupCompleted = false;
  private _recoveryInProgress = 0;
  private readyPromise: Promise<void>;

  constructor(size: number) {
    if (!Number.isInteger(size) || size < 1) {
      throw new Error(`ShimPool size must be a positive integer, got ${size}`);
    }
    for (let i = 0; i < size; i++) {
      this.spawnWorker();
    }
    // Warmup-on-construct: dispatch one no-op score per worker so each
    // pays its ~1.5s createShim cost at boot rather than on the first
    // user request. allSettled; a warmup failure should not crash the
    // pool; the user's first real request would fail the same way and
    // surface a meaningful error. Only flip _initialWarmupCompleted=true
    // when at least one warmup fulfilled; if every warmup rejected
    // (e.g., vendor/rocalc absent), the pod is dead and /healthz must
    // stay 503.
    this.readyPromise = Promise.allSettled(
      Array.from({ length: size }, () => this.run({})),
    ).then((results) => {
      if (this.shuttingDown) return;
      if (results.some((r) => r.status === "fulfilled")) {
        this._initialWarmupCompleted = true;
      }
    });
  }

  /** True once initial warmup has completed with at least one worker
   * fulfilling its first run, AND no crash-recovery warmup is in
   * flight. The K8s /healthz endpoint reads this; returning 200 with
   * a cold replacement in the pool would route real traffic to a
   * worker that needs to pay createShim() before responding. */
  ready(): boolean {
    if (this.shuttingDown) return false;
    return this._initialWarmupCompleted && this._recoveryInProgress === 0;
  }

  /** Awaitable form of ready(); resolves once warmup is complete (or
   * once terminate() has prevented warmup from completing). Useful for
   * tests; not used by the HTTP path. */
  awaitReady(): Promise<void> {
    return this.readyPromise;
  }

  /** Number of workers currently in the pool. May briefly differ from
   * the constructor size during a respawn after a crash. */
  size(): number {
    return this.workers.length;
  }

  /** Pending work the pool has accepted but not yet completed;
   * useful for tests asserting the pool drains cleanly. */
  pendingCount(): number {
    return this.pending.size;
  }

  /** Run one score request on any free worker. Resolves with the
   * worker's response, or rejects with the worker's error (preserving
   * the validation classification via PoolValidationError when the
   * shim flagged the failure as caller-correctable). */
  async run(req: ScoreRequest): Promise<ScoreResponse> {
    if (this.shuttingDown) {
      throw new Error("ShimPool: cannot run() after terminate()");
    }
    const slot = await this.acquire();
    // Terminate-vs-run interleaving has three windows:
    //
    //   1. acquire() yielded a microtask between busy=true and our
    //      resumption here. terminate() ran in full during that window
    //      (or just its sync portion before its await Promise.all).
    //   2. terminate is currently mid-await on worker.terminate(); its
    //      pending-rejection loop already ran and pending is empty.
    //   3. terminate is called *between this check and our pending.set
    //      below*; impossible without a yield, but we keep the second
    //      check inside the Promise constructor as a guard against a
    //      future refactor introducing an await in this block.
    //
    // For (1) and (2), bail synchronously: terminate's pending-iteration
    // has already passed, so a `pending.set(id, ...)` here would store an
    // entry nobody ever rejects, hanging the caller's awaited promise.
    if (this.shuttingDown) {
      this.release(slot);
      throw new Error(
        "ShimPool: terminated while request was acquiring a worker",
      );
    }
    const id = this.nextId++;
    slot.currentJob = id;
    return new Promise<ScoreResponse>((resolve, reject) => {
      // Window (3) guard; defensive only. If shuttingDown flipped to
      // true between the check above and this constructor body running
      // (which is currently impossible; no yield between them), reject
      // immediately instead of leaving a permanent pending entry.
      if (this.shuttingDown) {
        slot.currentJob = undefined;
        this.release(slot);
        reject(
          new Error("ShimPool: terminated while request was being dispatched"),
        );
        return;
      }
      this.pending.set(id, { resolve, reject });
      slot.worker.postMessage({ id, req });
    });
  }

  /** Tear down all workers. Pending and queued jobs are rejected;
   * subsequent run() calls reject immediately. Idempotent. */
  async terminate(): Promise<void> {
    if (this.shuttingDown) return;
    this.shuttingDown = true;
    const queueErr = new Error("ShimPool: terminated while job was queued");
    for (const w of this.waiting.splice(0)) {
      w.reject(queueErr);
    }
    const flightErr = new Error("ShimPool: terminated while job in flight");
    for (const [, p] of this.pending) {
      p.reject(flightErr);
    }
    this.pending.clear();
    await Promise.all(
      this.workers.map((s) => s.worker.terminate().catch(() => undefined)),
    );
    this.workers.splice(0);
  }

  private spawnWorker(): PoolWorker | undefined {
    // Belt-and-suspenders against the terminate-vs-crash interleaving:
    // if terminate() set shuttingDown=true between the caller's last
    // sync check and now (e.g., a crash event fired immediately before
    // terminate ran but spawned a replacement that races the splice),
    // refuse to spawn so we never end up with a zombie worker that
    // terminate()'s workers.map snapshot won't reach.
    if (this.shuttingDown) return undefined;

    const w = new Worker(WORKER_URL);
    const slot: PoolWorker = {
      worker: w,
      busy: false,
      currentJob: undefined,
      recoveryAttempts: 0,
    };

    w.on("message", (msg: WorkerMessage) => {
      this.handleMessage(slot, msg);
    });

    w.on("error", (err: unknown) => {
      // Uncaught exception escaping the worker's message handler.
      // The shim's own resetScoreShim() catches non-validation errors
      // inside score(), so this path is for genuinely fatal stuff;
      // out-of-memory, jsdom panic, etc. Treat as worker-dead.
      const e = err instanceof Error ? err : new Error(String(err));
      this.handleWorkerCrash(slot, e);
    });

    w.on("exit", (code) => {
      if (code === 0 || this.shuttingDown) return;
      // Node fires both 'error' then 'exit' on an uncaught worker exception.
      // If the 'error' handler already removed this slot, skip; a second
      // handleWorkerCrash would spawn an extra permanent worker.
      if (!this.workers.includes(slot)) return;
      this.handleWorkerCrash(
        slot,
        new Error(`worker exited unexpectedly with code ${code}`),
      );
    });

    this.workers.push(slot);
    return slot;
  }

  private handleMessage(slot: PoolWorker, msg: WorkerMessage): void {
    const id = msg.id;
    slot.currentJob = undefined;
    const job = this.pending.get(id);
    if (job) {
      this.pending.delete(id);
      if (msg.ok && msg.result !== undefined) {
        job.resolve(msg.result);
      } else {
        const message = msg.message ?? "worker error (no message)";
        const err = msg.validation
          ? new PoolValidationError(message)
          : new Error(message);
        job.reject(err);
      }
    }
    this.release(slot);
  }

  private handleWorkerCrash(slot: PoolWorker, err: Error): void {
    // Fail the job that was on this worker, if any.
    if (slot.currentJob !== undefined) {
      const job = this.pending.get(slot.currentJob);
      if (job) {
        this.pending.delete(slot.currentJob);
        job.reject(
          new Error(`shim worker crashed mid-request: ${err.message}`),
        );
      }
      slot.currentJob = undefined;
    }
    // Remove the dead slot and spawn a replacement so the pool's
    // effective parallelism returns to its configured size. Increment
    // _recoveryInProgress so ready() returns false while the cold
    // replacement is warming; otherwise the LB would route requests
    // to a worker that hasn't paid createShim yet. The decrement runs
    // in finally(), so a failed warmup still releases the gate.
    const idx = this.workers.indexOf(slot);
    if (idx >= 0) this.workers.splice(idx, 1);
    if (this.shuttingDown) return;
    // Circuit breaker: if this slot has already burned through
    // MAX_RECOVERY_ATTEMPTS respawn cycles without a successful
    // warmup, we're stuck in a crash loop. Spawning another worker
    // just keeps _recoveryInProgress pinned above zero and ready()
    // pinned at false; the pod looks dead to the LB forever even
    // though surviving workers could still serve requests. Stop
    // respawning, log loudly, and let the pool run degraded.
    const nextAttempts = slot.recoveryAttempts + 1;
    if (nextAttempts > MAX_RECOVERY_ATTEMPTS) {
      console.error(
        `ShimPool: slot exhausted ${MAX_RECOVERY_ATTEMPTS} recovery attempts; ` +
          `marking permanently failed. Pool now running with ${this.workers.length} ` +
          `worker(s). Last error: ${err.message}`,
      );
      return;
    }
    const newSlot = this.spawnWorker();
    if (!newSlot) return;
    newSlot.recoveryAttempts = nextAttempts;
    this._recoveryInProgress++;
    void this.warmupSlot(newSlot).finally(() => {
      this._recoveryInProgress--;
    });
  }

  // warmupSlot dispatches a no-op score directly to slot, bypassing
  // acquire(). Used by handleWorkerCrash to guarantee the warmup lands
  // on the cold replacement instead of any other idle worker; run()
  // would round-robin and might warm an already-warm slot. Returns
  // when the slot's first response arrives (fulfilled or rejected) or
  // when terminate() rejects the in-flight warmup.
  //
  // Resets slot.recoveryAttempts to 0 on a fulfilled response so a
  // worker that crashed once but recovered cleanly doesn't burn through
  // its breaker quota on the next crash. Logs rejected warmups via
  // console.error before settling; without that, a persistent warmup
  // failure (vendor/rocalc missing, mapping.json malformed) would be
  // silently swallowed and only surface as "ready() never flips true".
  private warmupSlot(slot: PoolWorker): Promise<void> {
    return new Promise<void>((resolve) => {
      if (this.shuttingDown) {
        resolve();
        return;
      }
      slot.busy = true;
      const id = this.nextId++;
      slot.currentJob = id;
      const onResolve = (): void => {
        slot.recoveryAttempts = 0;
        resolve();
      };
      const onReject = (err: Error): void => {
        console.error(
          `ShimPool: warmup rejected for slot (attempt ${slot.recoveryAttempts}): ${err.message}`,
        );
        resolve();
      };
      this.pending.set(id, { resolve: onResolve, reject: onReject });
      slot.worker.postMessage({ id, req: {} });
    });
  }

  private acquire(): Promise<PoolWorker> {
    for (const slot of this.workers) {
      if (!slot.busy) {
        slot.busy = true;
        return Promise.resolve(slot);
      }
    }
    return new Promise<PoolWorker>((resolve, reject) => {
      this.waiting.push({ resolve, reject });
    });
  }

  private release(slot: PoolWorker): void {
    slot.busy = false;
    const next = this.waiting.shift();
    if (next) {
      slot.busy = true;
      next.resolve(slot);
    }
  }
}

/** isPoolValidationError lets the HTTP layer recognize worker-side
 * validation failures without importing PoolValidationError directly
 * everywhere. Mirrors src/score.ts's isValidationError() predicate. */
export function isPoolValidationError(e: unknown): boolean {
  return (
    e instanceof PoolValidationError ||
    (e instanceof Error && (e as { validation?: boolean }).validation === true)
  );
}
