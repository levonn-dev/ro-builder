import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim, CALC_VERSION } from "../../../src/shim.ts";

// Smokes for the stub backend. These assert exactly the contract the
// stub claims to satisfy: they're tied to the stub's specific fiction
// (CALC_VERSION value, numeric baseline). Run with CALC_BACKEND=stub;
// the smokes also confirm that selection (registry working).

test("CALC_VERSION reflects the stub backend", () => {
  assert.equal(CALC_VERSION, "stub-v1");
});

test("createShim() does not require vendor/ to exist", () => {
  const shim = createShim();
  assert.ok(shim, "createShim returned a session");
});

test("inputs are not validated by the stub", () => {
  const shim = createShim();
  assert.doesNotThrow(() => shim.setEnemy(999999));
  assert.doesNotThrow(() => shim.setClass("not_a_class"));
});

test("reset() restores baseline derived stats", () => {
  const shim = createShim();
  const before = shim.readDerivedStats();
  shim.setStats({ str: 99, agi: 99, vit: 99, int: 99, dex: 99, luk: 99 });
  shim.reset();
  const after = shim.readDerivedStats();
  assert.deepEqual(
    after,
    before,
    "derived after reset equals before any mutation",
  );
});
