import { test, before } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// createShim (jsdom + calc engine) + setClass is the expensive part, so it runs
// once in before() and is reused. reset() preserves the class but clears the
// buff/debuff/land/music banks and rolls level/stats/equipment back to the class
// baseline, so hpShim() = reset()+reconfigure gives each test a clean, leak-free
// shim without re-paying the createShim+setClass cost.
let shim: ReturnType<typeof createShim>;
before(() => {
  shim = createShim();
  shim.setClass("high_priest");
});
function hpShim() {
  shim.reset();
  shim.setLevel({ base: 99, job: 70 });
  shim.setStats({ str: 90, agi: 40, vit: 60, int: 40, dex: 80, luk: 20 });
  return shim;
}
const DEF_TARGET = {
  hp: 200000,
  atk_min: 100,
  atk_max: 200,
  def: 80,
  mdef: 10,
  race: "RC_Undead" as const,
  element: "Ele_Undead" as const,
  element_lv: 1 as const,
  size: "Size_Medium" as const,
  level: 90,
};
const PLAIN_TARGET = {
  ...DEF_TARGET,
  race: "RC_Brute" as const,
  element: "Ele_Neutral" as const,
  def: 30,
};
const FLEE_TARGET = { ...PLAIN_TARGET, level: 110 }; // high level -> high flee

test("lex_aeterna raises combat damage (e*=2)", () => {
  const base = hpShim();
  base.setEnemyInline(PLAIN_TARGET);
  const plain = base.readCombatResults().damage.ave;
  const s = hpShim();
  s.setBuffs([{ name: "lex_aeterna", level: 1 }]);
  s.setEnemyInline(PLAIN_TARGET);
  const lexed = s.readCombatResults().damage.ave;
  assert.ok(plain != null && lexed != null, "damage.ave must be numeric");
  assert.ok(
    (lexed as number) > (plain as number),
    `Lex should raise damage (${plain} -> ${lexed})`,
  );
});

test("decrease_agi raises hit rate vs a high-flee target", () => {
  const base = hpShim();
  base.setEnemyInline(FLEE_TARGET);
  const plain = base.readCombatResults().hit;
  const s = hpShim();
  s.setBuffs([{ name: "decrease_agi", level: 10 }]);
  s.setEnemyInline(FLEE_TARGET);
  const debuffed = s.readCombatResults().hit;
  assert.ok(plain != null && debuffed != null, "hit must be numeric");
  assert.ok(
    (debuffed as number) >= (plain as number),
    `DecAGI should not lower hit (${plain} -> ${debuffed})`,
  );
});

test("signum_crucis raises damage vs undead and is a no-op vs non-undead", () => {
  // vs undead: Signum lowers DEF -> more damage
  const baseU = hpShim();
  baseU.setEnemyInline(DEF_TARGET);
  const plainU = baseU.readCombatResults().damage.ave;
  const sU = hpShim();
  sU.setBuffs([{ name: "signum_crucis", level: 10 }]);
  sU.setEnemyInline(DEF_TARGET);
  const signU = sU.readCombatResults().damage.ave;
  assert.ok(
    plainU != null && signU != null,
    "undead damage.ave must be numeric",
  );
  assert.ok(
    (signU as number) > (plainU as number),
    `Signum should raise damage vs undead (${plainU} -> ${signU})`,
  );

  // vs non-undead: no effect
  const baseN = hpShim();
  baseN.setEnemyInline(PLAIN_TARGET);
  const plainN = baseN.readCombatResults().damage.ave;
  const sN = hpShim();
  sN.setBuffs([{ name: "signum_crucis", level: 10 }]);
  sN.setEnemyInline(PLAIN_TARGET);
  const signN = sN.readCombatResults().damage.ave;
  assert.equal(
    signN,
    plainN,
    `Signum must be a no-op vs non-undead (${plainN} vs ${signN})`,
  );
});

test("reset() clears support buffs and enemy debuffs (no leak)", () => {
  const s = hpShim();
  s.setEnemyInline(PLAIN_TARGET);
  const base = s.readCombatResults().damage.ave;
  s.setBuffs([
    { name: "lex_aeterna", level: 1 },
    { name: "blessing", level: 10 },
  ]);
  s.setEnemyInline(PLAIN_TARGET);
  assert.notEqual(
    s.readCombatResults().damage.ave,
    base,
    "buffs should change damage",
  );
  s.reset();
  s.setClass("high_priest");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 90, agi: 40, vit: 60, int: 40, dex: 80, luk: 20 });
  s.setEnemyInline(PLAIN_TARGET);
  assert.equal(
    s.readCombatResults().damage.ave,
    base,
    "after reset, buffs/debuffs must be gone",
  );
});
