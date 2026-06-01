import { test } from "node:test";
import assert from "node:assert/strict";
import { applyEquipment, ScoreValidationError } from "../../src/score.ts";
import type { ShimSession } from "../../src/shim.ts";
import type { SlotKey, EquipSpec } from "../../src/types.ts";

// Backend-agnostic: drives applyEquipment with a fake shim so it runs
// vendor-free. applyEquipment must report EVERY item it can't apply in one
// error (not just the first), so the model fixes all bad gear in a single
// resubmit instead of one item per round-trip.

test("applyEquipment aggregates every bad slot into one validation error", () => {
  const badSlots = new Set<string>(["weapon", "accessory2"]);
  const applied: string[] = [];
  const fake: Pick<ShimSession, "equip"> = {
    equip(slot, _spec) {
      if (badSlots.has(slot)) {
        throw new ScoreValidationError(`item iRO 999 not usable in ${slot}`);
      }
      applied.push(slot);
    },
  };

  assert.throws(
    () =>
      applyEquipment(fake, {
        weapon: { id: 1 },
        armor: { id: 2 },
        accessory2: { id: 3 },
      }),
    (e: unknown) => {
      assert.ok(e instanceof ScoreValidationError);
      assert.match(e.message, /weapon:/);
      assert.match(e.message, /accessory2:/);
      return true;
    },
  );
  // The good slot was still attempted; the loop does not stop at the first bad.
  assert.deepEqual(applied, ["armor"]);
});

test("applyEquipment aggregates an unknown slot key alongside item errors", () => {
  // A typo'd slot key the model might emit ("headtop") plus a genuinely
  // unusable item must BOTH surface in one error, not just the slot key.
  const fake: Pick<ShimSession, "equip"> = {
    equip(slot, _spec) {
      if (slot === "armor") {
        throw new ScoreValidationError(`item iRO 999 not usable in ${slot}`);
      }
    },
  };

  assert.throws(
    () =>
      applyEquipment(fake, {
        headtop: { id: 1 },
        armor: { id: 2 },
      } as unknown as Partial<Record<SlotKey, EquipSpec>>),
    (e: unknown) => {
      assert.ok(e instanceof ScoreValidationError);
      assert.match(e.message, /headtop: unknown equipment slot/);
      assert.match(e.message, /armor:/);
      return true;
    },
  );
});

test("applyEquipment is a no-op when every slot applies", () => {
  const fake: Pick<ShimSession, "equip"> = { equip() {} };
  assert.doesNotThrow(() =>
    applyEquipment(fake, { weapon: { id: 1 }, armor: { id: 2 } }),
  );
});

test("applyEquipment rethrows a non-validation error immediately", () => {
  const seen: string[] = [];
  const fake: Pick<ShimSession, "equip"> = {
    equip(slot) {
      seen.push(slot);
      if (slot === "weapon") throw new TypeError("rocalc internal boom");
    },
  };
  assert.throws(
    () => applyEquipment(fake, { weapon: { id: 1 }, armor: { id: 2 } }),
    (e: unknown) => e instanceof TypeError,
  );
  // Bailed at the corrupting slot; did not continue to armor.
  assert.deepEqual(seen, ["weapon"]);
});
