import { describe, test } from "node:test";
import assert from "node:assert/strict";
import {
  isPoolValidationError,
  PoolValidationError,
  ShimPool,
} from "../../../src/pool.ts";

// Pool tests that are rocalc-specific: validation errors that only the
// rocalc backend throws (e.g. "unknown class"). The stub's setClass
// silently accepts any string, so these tests only make sense against
// the rocalc backend.

describe("ShimPool: rocalc-specific validation", () => {
  test("caller validation error propagates as PoolValidationError", async () => {
    const pool = new ShimPool(1);
    try {
      // Unknown class is the canonical pre-mutation validation error:
      // rocalc's setClass looks up the class name in m_Job and throws
      // "unknown class" if it's not found. The pool must surface that as
      // a PoolValidationError (not a generic Error or unhandled rejection)
      // so callers can distinguish "bad input" from "pool broken".
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
      assert.equal(
        typeof ok.derived.hit,
        "number",
        "post-validation-error request returned a numeric hit",
      );
    } finally {
      await pool.terminate();
    }
  });
});
