import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/backends/rocalc/index.ts";

// Orleans's Glove (iRO 2785) grants +DEX; a build wearing it must report
// effective DEX above the bare allocation.
test("rocalc totalStats reflects gear DEX bonus", () => {
  const shim = createShim();
  shim.setClass("high_wizard");
  shim.setLevel({ base: 99, job: 70 });
  shim.setStats({ str: 1, agi: 1, vit: 25, int: 99, dex: 99, luk: 1 });
  const before = shim.readDerivedStats().totalStats.dex;
  shim.equip("accessory1", { id: 2785, cards: [4064] }); // glove + Zerom (+DEX)
  const after = shim.readDerivedStats().totalStats.dex;
  // before > 99 because rocalc folds High Wizard's job-level DEX bonuses
  // into A_DEXp; that is correct behavior (job bonuses are part of the
  // effective stat). The only firm assertion is that gear raises it further.
  assert.ok(
    after > before,
    `expected DEX bonus from gear: before=${before} after=${after}`,
  );
});
