// HTTP entry point for the calc sidecar.
//
// Single endpoint: POST /score. Body is the ScoreRequest schema in
// src/types.ts. Response is JSON: { derived, combat? } per ScoreResponse.
//
// Pure Node http (no Express dep); the routing surface is one path, one
// method, no middleware needed. Two error categories:
//
//   400: client-fixable (malformed JSON, unknown slot key, missing iRO id
//        in the rocalc mapping, refine on a non-refinable slot, item not
//        in current class's option list)
//   500: server bug or shim crash (rocalc panicked, jsdom complained)
//
// Listens on PORT (env var, default 7401). The Go-side sidecar client
// reads the same env var so dev/prod can share configuration.
//
// Concurrency: every score request is dispatched to a worker thread via
// the ShimPool; the main thread holds no rocalc state and never calls
// score() directly. Pool size comes from SIDECAR_WORKERS (default 2).
// One worker = serialized throughput, same as the old singleton; two or
// more = real parallelism on multi-core hosts. Tune up if telemetry
// shows queue depth growing under load.

import http from "node:http";
import type { IncomingMessage, ServerResponse } from "node:http";
import { ShimPool, isPoolValidationError } from "./src/pool.ts";
import type { ScoreRequest } from "./src/types.ts";

const DEFAULT_PORT = 7401;
const DEFAULT_WORKERS = 2;

// MAX_REQUEST_BYTES caps the inbound /score body. A ScoreRequest is
// small JSON (stats + equipment + maybe inline-mob stats); well under
// 10 KB in practice. 1 MB is a generous ceiling that still cuts off a
// malicious / runaway client streaming gigabytes into memory before
// JSON.parse ever sees the bytes.
const MAX_REQUEST_BYTES = 1024 * 1024;

// REQUEST_TIMEOUT_MS bounds /score-style requests. score() inside a
// warm worker resolves in tens of ms; the only path that legitimately
// stretches into the seconds is a cold-shim createShim (~1.5s) plus
// ClickJob (~250ms). 90s is a comfortable ceiling that still cuts off
// a wedged worker before clients give up. /healthz overrides with a
// much shorter timeout; see createServer.
const REQUEST_TIMEOUT_MS = 90_000;
const HEALTHZ_TIMEOUT_MS = 5_000;

class RequestTooLargeError extends Error {
  readonly tooLarge = true;
  constructor() {
    super("request body too large");
    this.name = "RequestTooLargeError";
  }
}

function readBody(req: IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    let total = 0;
    req.on("data", (c: Buffer) => {
      total += c.length;
      if (total > MAX_REQUEST_BYTES) {
        // Destroy the stream so the client stops sending and the
        // socket releases. The destroy() argument propagates as the
        // 'error' event below.
        req.destroy(new RequestTooLargeError());
        return;
      }
      chunks.push(c);
    });
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    req.on("error", reject);
  });
}

// classifyError decides 400 vs 500. Errors classified as caller-fixable
// inside the worker arrive here as PoolValidationError (or any Error
// with a `.validation === true` flag), preserving the original
// score()-side classifyError predicate across the worker boundary.
function classifyError(err: unknown): number {
  return isPoolValidationError(err) ? 400 : 500;
}

// resolveWorkerCount reads SIDECAR_WORKERS, falling back to DEFAULT_WORKERS
// when unset / unparseable / non-positive. Anything goofy logs once at
// boot rather than failing; the sidecar still works with the default.
function resolveWorkerCount(): number {
  const raw = process.env.SIDECAR_WORKERS;
  if (raw == null || raw === "") return DEFAULT_WORKERS;
  const n = parseInt(raw, 10);
  if (!Number.isInteger(n) || n < 1) {
    console.warn(
      `SIDECAR_WORKERS=${JSON.stringify(raw)} is not a positive integer; using default ${DEFAULT_WORKERS}`,
    );
    return DEFAULT_WORKERS;
  }
  return n;
}

export interface ServerOptions {
  /** Override the worker pool. When provided, the caller owns the pool's
   * lifecycle; the server will NOT terminate it on close. Used by tests
   * that share one warmed-up pool across many test cases. */
  pool?: ShimPool;
}

export function createServer(opts: ServerOptions = {}): http.Server {
  const pool = opts.pool ?? new ShimPool(resolveWorkerCount());
  const ownsPool = opts.pool === undefined;

  const server = http.createServer(
    async (req: IncomingMessage, res: ServerResponse) => {
      res.setHeader("content-type", "application/json");

      if (req.method === "GET" && req.url === "/healthz") {
        // /healthz should be fast; no rocalc work, just a pool flag
        // read. A long timeout here would let a stuck readiness probe
        // pin a connection open for ~90s; tighten to a short ceiling.
        req.setTimeout(HEALTHZ_TIMEOUT_MS);
        if (pool.ready()) {
          res.statusCode = 200;
          res.end(JSON.stringify({ status: "ok" }));
        } else {
          res.statusCode = 503;
          res.end(JSON.stringify({ status: "warming up" }));
        }
        return;
      }

      if (req.url === "/healthz") {
        res.statusCode = 405;
        res.setHeader("Allow", "GET");
        res.end(JSON.stringify({ error: "method not allowed" }));
        return;
      }

      if (req.method !== "POST" || req.url !== "/score") {
        res.statusCode = 404;
        res.end(JSON.stringify({ error: "not found" }));
        return;
      }

      // /score gets the long per-request timeout so a cold worker
      // (createShim + ClickJob, ~1.75s) can finish without the socket
      // being yanked out from under it. Set BEFORE readBody so the
      // ceiling covers the upload window too.
      req.setTimeout(REQUEST_TIMEOUT_MS);

      let body: ScoreRequest;
      try {
        const raw = await readBody(req);
        body = raw === "" ? {} : (JSON.parse(raw) as ScoreRequest);
      } catch (e) {
        // readBody's stream destroy propagates RequestTooLargeError as
        // the 'error' event; surface it as 413 rather than the
        // generic 400 we use for JSON-parse failures.
        if (e instanceof Error && (e as RequestTooLargeError).tooLarge) {
          res.statusCode = 413;
          res.end(JSON.stringify({ error: "request body too large" }));
          return;
        }
        res.statusCode = 400;
        res.end(
          JSON.stringify({ error: "invalid JSON: " + (e as Error).message }),
        );
        return;
      }

      try {
        const result = await pool.run(body);
        res.end(JSON.stringify(result));
      } catch (e) {
        res.statusCode = classifyError(e);
        res.end(
          JSON.stringify({ error: e instanceof Error ? e.message : String(e) }),
        );
      }
    },
  );

  if (ownsPool) {
    // Tear the pool down when the http.Server is closed so node can
    // exit cleanly. Pool.terminate is idempotent so this is safe even
    // if the caller manually terminated the pool first.
    server.on("close", () => {
      pool.terminate().catch((err) => {
        console.error("ShimPool terminate error during server close:", err);
      });
    });
  }

  return server;
}

// Start when run directly (node server.ts). Stays quiet when imported by
// tests so the test suite can spin up its own server on an ephemeral port.
//
// PORT=0 binds to an OS-assigned free port; used by the Go-side integration
// test, which reads the actual port off the banner line below. The banner
// always prints the post-bind port, never the input value.
if (import.meta.url === `file://${process.argv[1]}`) {
  const port = parseInt(process.env.PORT || String(DEFAULT_PORT), 10);
  const workers = resolveWorkerCount();
  const server = createServer();
  server.listen(port, () => {
    const addr = server.address();
    const realPort = typeof addr === "object" && addr ? addr.port : port;
    console.log(`calc-sidecar listening on :${realPort} (workers=${workers})`);
  });
}
