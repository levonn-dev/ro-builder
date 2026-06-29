import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/backends/stub/index.ts";

test("stub readDerivedStats echoes allocated stats as totalStats", () => {
  const shim = createShim();
  shim.setStats({ str: 1, agi: 1, vit: 9, int: 34, dex: 40, luk: 1 });
  const d = shim.readDerivedStats();
  assert.deepEqual(d.totalStats, { str: 1, agi: 1, vit: 9, int: 34, dex: 40, luk: 1 });
});
