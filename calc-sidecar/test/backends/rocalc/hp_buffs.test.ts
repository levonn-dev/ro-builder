import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

function hpShim() {
  const s = createShim();
  s.setClass("high_priest");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 60, agi: 40, vit: 60, int: 80, dex: 60, luk: 20 });
  return s;
}
// Neutral medium target so element/size mods are 1x; mace-less HP still hits.
const TARGET = {
  hp: 50000,
  atk_min: 100,
  atk_max: 200,
  def: 10,
  mdef: 10,
  race: "RC_Brute" as const,
  element: "Ele_Neutral" as const,
  element_lv: 1 as const,
  size: "Size_Medium" as const,
  level: 80,
};

test("impositio_manus raises derived ATK", () => {
  const base = hpShim().readDerivedStats().atk.base;
  const s = hpShim();
  s.setBuffs([{ name: "impositio_manus", level: 5 }]);
  assert.ok(
    s.readDerivedStats().atk.base > base,
    "Impositio should raise ATK base",
  );
});

test("gloria raises crit (LUK +30)", () => {
  const base = hpShim().readDerivedStats().cri;
  const s = hpShim();
  s.setBuffs([{ name: "gloria", level: 1 }]);
  assert.ok(s.readDerivedStats().cri > base, "Gloria should raise crit");
});

test("angelus raises soft DEF", () => {
  const base = hpShim().readDerivedStats().def.soft;
  const s = hpShim();
  s.setBuffs([{ name: "angelus", level: 10 }]);
  assert.ok(
    s.readDerivedStats().def.soft > base,
    "Angelus should raise soft DEF",
  );
});

test("assumptio halves incoming combat damage (50% flat reduction, not a stat bonus)", () => {
  // Assumptio reduces all incoming physical damage by 50%. Rocalc models this
  // in the combat sim (B_MinAtk/B_AveAtk/B_MaxAtk), not in the derived DEF/MDEF
  // cells. There is no stat change to observe -- the reduction is applied inside
  // BattleCalc when the checkbox is set.
  const base = hpShim();
  base.setEnemyInline(TARGET);
  const baseCombat = base.readCombatResults().incoming;

  const s = hpShim();
  s.setBuffs([{ name: "assumptio", level: 5 }]);
  s.setEnemyInline(TARGET);
  const buffedCombat = s.readCombatResults().incoming;

  assert.ok(
    baseCombat.ave != null && buffedCombat.ave != null,
    "incoming.ave must be numeric",
  );
  assert.ok(
    (buffedCombat.ave as number) < (baseCombat.ave as number),
    `Assumptio should reduce incoming damage (${baseCombat.ave} -> ${buffedCombat.ave})`,
  );
});

test("blessing + increase_agi raise combat damage (feed the sim, not derived HIT)", () => {
  const base = hpShim();
  base.setEnemyInline(TARGET);
  const baseDmg = base.readCombatResults().damage.ave;
  const s = hpShim();
  s.setBuffs([
    { name: "blessing", level: 10 },
    { name: "increase_agi", level: 10 },
  ]);
  s.setEnemyInline(TARGET);
  const buffed = s.readCombatResults().damage.ave;
  assert.ok(baseDmg != null && buffed != null, "damage.ave must be numeric");
  assert.ok(
    (buffed as number) > (baseDmg as number),
    `blessing+agi should raise combat damage (${baseDmg} -> ${buffed})`,
  );
});

test("aspersio holy endow changes damage vs an undead target", () => {
  const undead = {
    ...TARGET,
    race: "RC_Undead" as const,
    element: "Ele_Undead" as const,
  };
  const base = hpShim();
  base.setEnemyInline(undead);
  const plain = base.readCombatResults().damage.ave;
  const s = hpShim();
  s.setBuffs([{ name: "aspersio", level: 5, element: "holy" }]);
  s.setEnemyInline(undead);
  const endowed = s.readCombatResults().damage.ave;
  assert.ok(plain != null && endowed != null, "damage.ave must be numeric");
  assert.notEqual(endowed, plain, "holy endow should change damage vs undead");
});
