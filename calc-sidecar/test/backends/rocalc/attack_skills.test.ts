import { test, before, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

const PORING = 1002; // iRO mob id; translates to rocalc m_Monster index 272

// The shim (jsdom + class load) is created once and reused; reset() preserves
// the class but clears pending attack skills and the last breakdown, so
// reset()+reconfigure in beforeEach gives every test a clean baseline without
// re-paying the ~4s createShim+setClass per test.
let shim: ReturnType<typeof createShim>;

// reset() rolls level / stats back to the class baseline; re-apply the bare TK
// build (no weapon) after each reset. The class itself survives reset().
function reconfig(): void {
  shim.setLevel({ base: 99, job: 70 });
  shim.setStats({ str: 90, agi: 80, vit: 40, int: 1, dex: 70, luk: 1 });
}

before(() => {
  shim = createShim();
  shim.setClass("taekwon_kid");
});

beforeEach(() => {
  shim.reset();
  reconfig();
});

test("primary attack skill drives top-level damage above auto-attack", () => {
  shim.setEnemy(PORING);
  const auto = shim.readCombatResults().damage.ave;

  shim.reset();
  reconfig();
  shim.setAttackSkills([{ name: "roundhouse", level: 7, primary: true }]);
  shim.setEnemy(PORING);
  const c = shim.readCombatResults();
  assert.ok(c.damage.ave !== null && auto !== null);
  assert.ok(
    c.damage.ave > auto,
    `roundhouse ${c.damage.ave} should exceed auto ${auto}`,
  );
  assert.equal(c.skills?.length, 1);
  assert.equal(c.skills?.[0].name, "roundhouse");
});

test("breakdown reports each declared skill with hit counts; primary drives top-level", () => {
  shim.setAttackSkills([
    { name: "tornado_kick", level: 7, primary: true },
    { name: "counter_kick", level: 7 },
  ]);
  shim.setEnemy(PORING);
  const c = shim.readCombatResults();
  const byName = Object.fromEntries((c.skills ?? []).map((k) => [k.name, k]));
  assert.ok(byName["tornado_kick"] && byName["counter_kick"]);
  assert.equal(byName["counter_kick"].hits, 3); // confirmed in Task 1
  // top-level reflects the primary (tornado_kick), not counter_kick
  assert.equal(c.damage.ave, byName["tornado_kick"].damage.ave);
});

test("absent attack_skills leaves auto-attack behavior unchanged (no skills field)", () => {
  shim.setEnemy(PORING);
  const c = shim.readCombatResults();
  assert.equal(c.skills, undefined);
});

test("unknown attack-skill name throws", () => {
  assert.throws(() =>
    shim.setAttackSkills([{ name: "fireball", level: 1, primary: true }]),
  );
});

test("zero primaries throws (top-level damage would be undefined)", () => {
  assert.throws(() =>
    shim.setAttackSkills([{ name: "tornado_kick", level: 7 }]),
  );
});

test("two primaries throws (ambiguous top-level damage)", () => {
  assert.throws(() =>
    shim.setAttackSkills([
      { name: "tornado_kick", level: 7, primary: true },
      { name: "roundhouse", level: 7, primary: true },
    ]),
  );
});

test("supportedAttackSkills returns the binding keys", () => {
  const names = shim.supportedAttackSkills();
  assert.ok(
    names && names.includes("tornado_kick") && names.includes("roundhouse"),
  );
});

const INLINE = {
  hp: 5000,
  atk_min: 100,
  atk_max: 200,
  def: 10,
  mdef: 5,
  race: "RC_Brute" as const,
  element: "Ele_Neutral" as const,
  element_lv: 1,
  size: "Size_Medium" as const,
  level: 50,
};

test("breakdown is computed against an inline mob (before m_Monster restore)", () => {
  shim.setAttackSkills([{ name: "roundhouse", level: 7, primary: true }]);
  shim.setEnemyInline(INLINE);
  const c = shim.readCombatResults();
  assert.equal(c.skills?.length, 1);
  assert.ok(c.skills?.[0].damage.ave !== null && c.skills[0].damage.ave > 0);
  assert.equal(c.damage.ave, c.skills?.[0].damage.ave); // primary drives top-level
});

test("reset clears a scored skill: next auto-attack request is unbuffed baseline", () => {
  shim.setEnemy(PORING);
  const auto = shim.readCombatResults().damage.ave;
  shim.setAttackSkills([{ name: "roundhouse", level: 7, primary: true }]);
  shim.setEnemy(PORING);
  assert.ok(shim.readCombatResults().damage.ave! > auto!);
  shim.reset();
  reconfig();
  shim.setEnemy(PORING);
  assert.equal(shim.readCombatResults().damage.ave, auto); // back to auto-attack
});
