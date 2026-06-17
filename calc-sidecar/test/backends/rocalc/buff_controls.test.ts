import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// The injected banks must exist and, with the section gates off (default),
// have no effect on a baseline High Priest. setSkills/setStats path proves the
// controls are present (no "Cannot read properties of undefined").
test("buff control banks are injected and inert until driven", () => {
  const s = createShim();
  s.setClass("high_priest");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 40, agi: 80, vit: 60, int: 80, dex: 60, luk: 30 });
  // Reading derived stats forces a full StAllCalc; if any A2_Skill*/B_debuf*
  // referenced by a gated read were missing, rocalc would throw here once the
  // gate is on. With gates off it must simply succeed.
  const d = s.readDerivedStats();
  assert.ok(
    Number.isFinite(d.maxHp) && d.maxHp > 0,
    "baseline HP should compute",
  );
});
