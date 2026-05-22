// Worker-thread entry point.
//
// Each worker owns its own jsdom + rocalc shim (created lazily on the
// first score() call inside this thread). Worker threads share no JS
// memory with the main thread or with each other; `m_Item`, `m_Monster`,
// `n_A_STR`, and every other rocalc global lives in the worker's isolated
// V8 isolate, so concurrent score() calls across workers cannot corrupt
// each other's state regardless of what the calc does internally.
//
// Wire protocol over the parentPort MessageChannel:
//
//   Inbound:  { id: number, req: ScoreRequest }
//   Outbound: { id: number, ok: true,  result: ScoreResponse }
//          |  { id: number, ok: false, message: string, validation: boolean }
//
// `id` round-trips so the pool can correlate responses with the
// promises waiting on them. `validation` propagates the
// isValidationError predicate across the thread boundary so the HTTP
// layer's 400-vs-500 classification stays correct.
//
// The shim's own resetScoreShim() inside score() handles the
// "non-validation error → reset shim" recovery; that logic is unchanged
// per worker. A worker only needs to be respawned if the entire thread
// dies (uncaught exception escaping the message handler, OOM, etc.);
// the pool listens for 'error' / 'exit' to handle those cases.

import { parentPort } from "node:worker_threads";
import { score, isValidationError } from "./score.ts";
import type { ScoreRequest, ScoreResponse } from "./types.ts";

if (!parentPort) {
  throw new Error(
    "worker.ts must be loaded as a Node worker thread (no parentPort available)",
  );
}

// Test-only crash backdoor; pool.test.ts uses this to verify
// crash-recovery semantics (the breaker-after-N-failures path in
// pool.ts can't be observed without a way to actually kill a worker).
// Production callers never set this field; the ScoreRequest contract
// in types.ts does not advertise it.
interface JobMessage {
  id: number;
  req: ScoreRequest & { __testCrash?: boolean };
}

interface OkResponse {
  id: number;
  ok: true;
  result: ScoreResponse;
}

interface ErrResponse {
  id: number;
  ok: false;
  message: string;
  validation: boolean;
}

parentPort.on("message", (msg: JobMessage) => {
  // Test-only crash backdoor (see JobMessage). Exits the worker with
  // a non-zero code so the pool's 'exit' handler treats it as a crash
  // and runs handleWorkerCrash. Production callers never set this.
  if (msg.req && msg.req.__testCrash === true) {
    process.exit(1);
  }
  let out: OkResponse | ErrResponse;
  try {
    const result = score(msg.req);
    out = { id: msg.id, ok: true, result };
  } catch (e) {
    out = {
      id: msg.id,
      ok: false,
      message: e instanceof Error ? e.message : String(e),
      validation: isValidationError(e),
    };
  }
  // postMessage uses structured clone; ScoreResponse is plain JSON-ish
  // data so the round-trip is lossless. Errors don't structured-clone
  // reliably (stack frames, custom properties get lost), so we hand-pack
  // the relevant fields above instead of letting the worker throw.
  parentPort!.postMessage(out);
});
