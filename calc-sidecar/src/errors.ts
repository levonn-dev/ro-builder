// ScoreValidationError marks an error as a caller-fixable validation
// failure; unknown slot, unmapped iRO id, refine out of range, unknown
// class, etc. These are thrown BEFORE any form / rocalc-internal mutation,
// so the shim's internal state stays intact and the cached shim can keep
// serving subsequent requests.
//
// The `validation: true` brand survives a structured-clone round-trip
// across the worker boundary (the class identity does not), so
// src/worker.ts hand-packs this flag onto its outbound error message and
// src/pool.ts rebuilds a PoolValidationError on receipt. score.ts's
// isValidationError() recognizes either the typed class or the
// duck-typed flag, which keeps the cross-thread classification correct.
//
// Lives in its own file (not score.ts) to avoid an import cycle:
// score.ts → shim.ts → backends/rocalc/index.ts. Both score.ts and the
// rocalc backend throw this; both reach it via this leaf module.
export class ScoreValidationError extends Error {
  readonly validation = true;
  constructor(message: string) {
    super(message);
    this.name = "ScoreValidationError";
  }
}
