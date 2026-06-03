import { test } from "node:test";
import assert from "node:assert/strict";
import type { ShimSession } from "../../src/shim.ts";
import { createShim } from "../../src/shim.ts";

// Backend-agnostic contract tests. Assert the shape and invariants
// every backend must satisfy (e.g. method presence, shape of returned
// structs, non-degenerate value bounds) and not backend-specific numeric
// correctness, which belongs under test/backends/<name>/. Runs
// against whichever backend CALC_BACKEND selects.

test("createShim returns an object with all required methods", () => {
  const shim = createShim();
  const methods: Array<keyof ShimSession> = [
    "setClass",
    "setLevel",
    "setStats",
    "equip",
    "setSkills",
    "setBuffs",
    "setEnemy",
    "setEnemyInline",
    "readDerivedStats",
    "readCombatResults",
    "reset",
  ];
  for (const method of methods) {
    assert.equal(
      typeof shim[method],
      "function",
      `shim.${method} must be a function`,
    );
  }
});

test("readDerivedStats returns the full DerivedStats shape", () => {
  const shim = createShim();
  const derived = shim.readDerivedStats();
  // Shape only; numbers are backend-specific.
  assert.equal(typeof derived.hit, "number");
  assert.equal(typeof derived.flee, "number");
  assert.equal(typeof derived.cri, "number");
  assert.equal(typeof derived.atk.base, "number");
  assert.equal(typeof derived.atk.plus, "number");
  assert.equal(typeof derived.matk.min, "number");
  assert.equal(typeof derived.matk.max, "number");
  assert.equal(typeof derived.def.hard, "number");
  assert.equal(typeof derived.def.soft, "number");
  assert.equal(typeof derived.mdef.hard, "number");
  assert.equal(typeof derived.mdef.soft, "number");
  assert.equal(typeof derived.aspd, "number");
  assert.equal(typeof derived.maxHp, "number");
  assert.equal(typeof derived.maxSp, "number");
  assert.equal(typeof derived.statPointsRemaining, "number");
});

test("readDerivedStats satisfies the e2e bounds contract", () => {
  const shim = createShim();
  const derived = shim.readDerivedStats();
  assert.ok(derived.maxHp > 0, "maxHp must be positive");
  assert.ok(derived.aspd > 0, "aspd must be positive");
});

test("reset() returns derived stats to the post-create baseline", () => {
  const shim = createShim();
  const baseline = shim.readDerivedStats();
  shim.setStats({ str: 99, agi: 99, vit: 99, int: 99, dex: 99, luk: 99 });
  shim.reset();
  assert.deepEqual(
    shim.readDerivedStats(),
    baseline,
    "after reset, derived must equal post-create baseline",
  );
});

test("readCombatResults returns the full CombatResults shape", () => {
  const shim = createShim();
  const combat = shim.readCombatResults();
  // CombatDamage / CombatCrit / CombatIncoming fields are number | null; use key-presence
  // so the check works for both backends regardless of combat-sim state.
  assert.ok("ave" in combat.damage, "damage.ave present");
  assert.ok("rate" in combat.crit, "crit.rate present");
  assert.ok("ave" in combat.incoming, "incoming.ave present");
  // CombatEnemy fields are non-null strings.
  assert.equal(typeof combat.enemy.race, "string");
  assert.equal(typeof combat.enemy.element, "string");
  assert.equal(typeof combat.enemy.size, "string");
  assert.equal(typeof combat.enemy.type, "string");
});
