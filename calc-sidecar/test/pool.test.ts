import { describe, test } from "node:test";
import assert from "node:assert/strict";
import {
  isPoolValidationError,
  PoolValidationError,
  ShimPool,
} from "../src/pool.ts";

// Pool tests run real worker threads with real shims; each test pays
// the per-worker ~1.5s cold start. Kept narrow on purpose: we cover
// pool semantics (parallel dispatch, queueing, error preservation,
// crash recovery), not shim correctness; that's covered by
// shim.test.ts and score.test.ts in the main thread.

describe("ShimPool", () => {
  test("rejects invalid sizes", () => {
    assert.throws(() => new ShimPool(0), /positive integer/);
    assert.throws(() => new ShimPool(-1), /positive integer/);
    assert.throws(() => new ShimPool(1.5), /positive integer/);
  });

  test("serves one request through a single worker", async () => {
    const pool = new ShimPool(1);
    try {
      const r = await pool.run({});
      assert.equal(r.derived.hit, 2, "default Novice hit");
      assert.equal(pool.pendingCount(), 0);
    } finally {
      await pool.terminate();
    }
  });

  test("parallel dispatch; 4 requests through 2 workers all complete", async () => {
    const pool = new ShimPool(2);
    try {
      const responses = await Promise.all([
        pool.run({ stats: { str: 1, agi: 1, vit: 1, int: 1, dex: 1, luk: 1 } }),
        pool.run({
          stats: { str: 1, agi: 1, vit: 1, int: 1, dex: 99, luk: 1 },
        }),
        pool.run({
          stats: { str: 1, agi: 99, vit: 1, int: 1, dex: 1, luk: 1 },
        }),
        pool.run({
          stats: { str: 99, agi: 1, vit: 1, int: 1, dex: 1, luk: 1 },
        }),
      ]);
      // Sanity-check the per-stat differentiation came through; proves
      // each request really did execute against its own input rather
      // than collapsing into a shared state somewhere.
      const [base, dex, agi, str] = responses;
      assert.equal(base.derived.hit, 2);
      assert.equal(dex.derived.hit, 100, "dex 99 → HIT 100");
      assert.equal(agi.derived.flee, 100, "agi 99 → FLEE 100");
      // Pre-re ATK base = level + STR + floor((STR/10)^2). Novice lvl 1
      // with STR 99: 1 + 99 + 81 = 181 base; minus 1 for some
      // rocalc-specific subtraction = 180. Just verify it scaled up
      // well past the baseline rather than locking the exact number.
      assert.ok(
        str.derived.atk.base > 100,
        `str 99 should drive ATK base well above the baseline; got ${str.derived.atk.base}`,
      );
      assert.equal(pool.pendingCount(), 0);
    } finally {
      await pool.terminate();
    }
  });

  test("queueing; more requests than workers serializes the surplus", async () => {
    // 1 worker, 3 simultaneous requests. All three should resolve;
    // pendingCount should peak at 1 (the in-flight one) since queued
    // waiters don't count as pending until they're assigned a worker.
    const pool = new ShimPool(1);
    try {
      const inflight = [pool.run({}), pool.run({}), pool.run({})];
      const results = await Promise.all(inflight);
      assert.equal(results.length, 3);
      for (const r of results) {
        assert.equal(r.derived.hit, 2);
      }
      assert.equal(pool.pendingCount(), 0);
    } finally {
      await pool.terminate();
    }
  });

  test("caller validation error propagates as PoolValidationError", async () => {
    const pool = new ShimPool(1);
    try {
      // Unknown class is the canonical pre-mutation validation error.
      await assert.rejects(
        () => pool.run({ class: "definitely_not_a_class" }),
        (err: unknown) => {
          assert.ok(
            err instanceof PoolValidationError,
            "PoolValidationError type",
          );
          assert.ok(
            isPoolValidationError(err),
            "isPoolValidationError predicate",
          );
          assert.match((err as Error).message, /unknown class/);
          return true;
        },
      );
      // And the worker isn't poisoned; a follow-up valid request still works.
      const ok = await pool.run({});
      assert.equal(ok.derived.hit, 2);
    } finally {
      await pool.terminate();
    }
  });

  test("terminate() rejects queued jobs and is idempotent", async () => {
    // Use a fresh (cold) pool so the very first request triggers the
    // ~1.5s createShim path on the worker. The second request is
    // guaranteed to still be queued by the time we call terminate.
    // (We can't make the same guarantee about the *in-flight* request
    // without instrumenting the worker; a warmed shim can complete
    // a no-op request in <50ms, racing terminate. The contract we
    // care about is "queued requests get rejected promptly"; in-flight
    // requests either complete normally or get rejected, both fine.)
    const pool = new ShimPool(1);
    const inflight = pool.run({}); // pays cold-start cost in the worker
    const queued = pool.run({});

    // Attach rejection handler BEFORE terminate so Node's unhandled-
    // rejection detector doesn't fire in the gap between rejection
    // and our await.
    const queuedCheck = assert.rejects(queued, /terminate/);
    // Tolerate either outcome on inflight; we prove "terminate
    // doesn't leak hanging promises" via queuedCheck.
    const inflightSettled = inflight.then(
      () => "resolved" as const,
      () => "rejected" as const,
    );

    await pool.terminate();
    await queuedCheck;
    assert.match(await inflightSettled, /^(resolved|rejected)$/);

    // Idempotent.
    await pool.terminate();
    // run() after terminate rejects with a clear "post-terminate" error.
    await assert.rejects(() => pool.run({}), /terminate/);
  });

  test("size() reflects active worker count", async () => {
    const pool = new ShimPool(3);
    assert.equal(pool.size(), 3);
    await pool.terminate();
  });

  test("ready() is false immediately after construction, true after warmup", async () => {
    const pool = new ShimPool(1);
    try {
      assert.equal(pool.ready(), false, "ready should be false before warmup");
      await pool.awaitReady();
      assert.equal(pool.ready(), true, "ready should be true after warmup");
    } finally {
      await pool.terminate();
    }
  });

  test("awaitReady resolves after all N workers have warmed up", async () => {
    const pool = new ShimPool(2);
    try {
      assert.equal(pool.ready(), false);
      await pool.awaitReady();
      assert.equal(pool.ready(), true);
      // After warmup, a normal run should be fast (no createShim cost).
      const t0 = Date.now();
      await pool.run({});
      const elapsed = Date.now() - t0;
      assert.ok(
        elapsed < 500,
        `post-warmup run should be quick; took ${elapsed}ms`,
      );
    } finally {
      await pool.terminate();
    }
  });

  test("ready() stays false if pool is terminated before warmup completes", async () => {
    const pool = new ShimPool(1);
    // Terminate before awaiting warmup. ready() must not lie about a
    // pool that's never going to serve a request.
    await pool.terminate();
    assert.equal(pool.ready(), false);
  });

  // Crash recovery: a worker that exits abnormally must be replaced and
  // the pool must keep serving subsequent requests. Uses the
  // __testCrash backdoor on worker.ts (process.exit(1) inside the worker)
  // to actually kill the worker; without that, score()'s own try/catch
  // would swallow exceptions and the 'exit' / 'error' events in pool.ts
  // would never fire.
  test("worker crash respawns and pool serves subsequent requests", async () => {
    const pool = new ShimPool(1);
    try {
      // Warm up first so the cold-start cost doesn't race the crash.
      await pool.awaitReady();
      assert.equal(pool.ready(), true, "pool ready before crash");
      assert.equal(pool.size(), 1, "one worker present");

      // Force the worker to exit(1) via the test backdoor. The pool's
      // 'exit' handler should reject this in-flight request, splice the
      // dead slot, spawn a replacement, and warm it.
      await assert.rejects(
        // Cast around the public ScoreRequest type; __testCrash is a
        // worker-internal backdoor (see src/worker.ts) not advertised
        // in the contract.
        () => pool.run({ __testCrash: true } as Record<string, unknown>),
        /crashed mid-request|terminate/,
      );

      // Wait for the replacement worker to finish warming. ready() flips
      // back to true once _recoveryInProgress drops to 0. Poll briefly;
      // should clear within a few seconds (one createShim() cost).
      const deadline = Date.now() + 10_000;
      while (!pool.ready() && Date.now() < deadline) {
        await new Promise((r) => setTimeout(r, 100));
      }
      assert.equal(pool.ready(), true, "pool ready again after recovery");
      assert.equal(pool.size(), 1, "replacement worker spawned");

      // Subsequent request goes through the replacement worker.
      const r = await pool.run({});
      assert.equal(
        r.derived.hit,
        2,
        "post-crash request returns default Novice",
      );
    } finally {
      await pool.terminate();
    }
  });
});
