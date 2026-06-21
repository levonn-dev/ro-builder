import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Gunslinger self-buffs ride the rocalc job-buff bank (m_JobBuff[45]); all eight
// skills use the skill_slot driver. Gunslinger is an Expanded class (no trans split)
// and its GS_ skills are gun-class-exclusive, so nothing here is inherited from
// another onboarding. Guns are already mapped, so each buff is verified against a
// real weapon: a revolver (Garrison, iRO 13104) for the gun/revolver buffs and a
// gatling (Drifter, iRO 13157, W_GATLING) for Gatling Fever. Fields asserted per buff:
//   single_action       -> readDerivedStats().hit AND .aspd
//   snake_eye           -> readDerivedStats().hit
//   increasing_accuracy -> readDerivedStats().hit
//   madness_canceller   -> readCombatResults().damage.ave (+100 ATK)
//   adjustment          -> readDerivedStats().flee (and does NOT raise damage.ave)
//   chain_action        -> readCombatResults().damage.secondAve (double-damage proc)
//   gatling_fever       -> readDerivedStats().aspd (gatling only; flee drops)
//   flip_the_coin       -> readCombatResults().damage.ave (rocalc coin-damage model)
const REVOLVER = 13104; // Garrison
const GATLING = 13157; // Drifter (W_GATLING; NOT Destroyer 13160, which is a grenade launcher)

function gsShim(weaponId: number) {
  const s = createShim();
  s.setClass("gunslinger");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 50, agi: 90, vit: 30, int: 1, dex: 90, luk: 20 });
  s.equip("weapon", { id: weaponId });
  return s;
}

const NEUTRAL = {
  hp: 50000,
  atk_min: 100,
  atk_max: 200,
  def: 20,
  mdef: 10,
  race: "RC_DemiHuman",
  element: "Ele_Neutral",
  element_lv: 1,
  size: "Size_Medium",
  level: 80,
} as const;

type Buff = { name: string; level: number };

function hitWith(buffs: Buff[], weaponId: number): number {
  const s = gsShim(weaponId);
  if (buffs.length) s.setBuffs(buffs);
  return s.readDerivedStats().hit;
}

function aspdWith(buffs: Buff[], weaponId: number): number {
  const s = gsShim(weaponId);
  if (buffs.length) s.setBuffs(buffs);
  return s.readDerivedStats().aspd;
}

function fleeWith(buffs: Buff[], weaponId: number): number {
  const s = gsShim(weaponId);
  if (buffs.length) s.setBuffs(buffs);
  return s.readDerivedStats().flee;
}

function aveWith(buffs: Buff[], weaponId: number): number {
  const s = gsShim(weaponId);
  if (buffs.length) s.setBuffs(buffs);
  s.setEnemyInline(NEUTRAL);
  const ave = s.readCombatResults().damage.ave;
  assert.ok(ave != null, "damage.ave must be numeric");
  return ave as number;
}

function secondAveWith(buffs: Buff[], weaponId: number): number | null {
  const s = gsShim(weaponId);
  if (buffs.length) s.setBuffs(buffs);
  s.setEnemyInline(NEUTRAL);
  return s.readCombatResults().damage.secondAve;
}

// --- single_action: +HIT and +ASPD with a gun equipped ---
test("single_action raises hit (gun equipped)", () => {
  const base = hitWith([], REVOLVER);
  const got = hitWith([{ name: "single_action", level: 10 }], REVOLVER);
  assert.ok(got > base, `single_action should raise hit (${base} -> ${got})`);
});

test("single_action raises aspd (gun equipped)", () => {
  const base = aspdWith([], REVOLVER);
  const got = aspdWith([{ name: "single_action", level: 10 }], REVOLVER);
  assert.ok(got > base, `single_action should raise aspd (${base} -> ${got})`);
});

// --- snake_eye: +HIT with guns ---
test("snake_eye raises hit", () => {
  const base = hitWith([], REVOLVER);
  const got = hitWith([{ name: "snake_eye", level: 10 }], REVOLVER);
  assert.ok(got > base, `snake_eye should raise hit (${base} -> ${got})`);
});

// --- increasing_accuracy: +20 HIT / +4 DEX / +4 AGI ---
test("increasing_accuracy raises hit", () => {
  const base = hitWith([], REVOLVER);
  const got = hitWith([{ name: "increasing_accuracy", level: 1 }], REVOLVER);
  assert.ok(
    got > base,
    `increasing_accuracy should raise hit (${base} -> ${got})`,
  );
});

// --- madness_canceller (Last Stand): +100 ATK -> damage.ave ---
test("madness_canceller raises damage.ave", () => {
  const base = aveWith([], REVOLVER);
  const got = aveWith([{ name: "madness_canceller", level: 1 }], REVOLVER);
  assert.ok(
    got > base,
    `madness_canceller should raise damage.ave (${base} -> ${got})`,
  );
});

// --- adjustment (Gunslinger's Panic): +FLEE; must NOT raise damage.ave ---
test("adjustment raises flee", () => {
  const base = fleeWith([], REVOLVER);
  const got = fleeWith([{ name: "adjustment", level: 1 }], REVOLVER);
  assert.ok(got > base, `adjustment should raise flee (${base} -> ${got})`);
});

test("adjustment does not raise damage.ave (lowers HIT and ranged damage)", () => {
  const base = aveWith([], REVOLVER);
  const got = aveWith([{ name: "adjustment", level: 1 }], REVOLVER);
  assert.ok(
    got <= base,
    `adjustment must not raise damage.ave (${base} -> ${got})`,
  );
});

// --- chain_action: double-damage proc -> damage.secondAve ---
// User-confirmed on rocalc.com: the combat-sim DPS moves and a separate "Chain Action
// damage (chance)" proc row appears. If the shim surfaces the proc on a different
// combat field than secondAve, assert that field instead -- it is not inert.
test("chain_action produces a second-attack proc (damage.secondAve)", () => {
  const got = secondAveWith([{ name: "chain_action", level: 10 }], REVOLVER);
  assert.ok(
    got != null && got > 0,
    `chain_action should produce a positive secondAve, got ${got}`,
  );
});

// --- gatling_fever: greatly +ASPD, gatling-type weapon only (W_GATLING) ---
test("gatling_fever raises aspd (gatling equipped)", () => {
  const base = aspdWith([], GATLING);
  const got = aspdWith([{ name: "gatling_fever", level: 10 }], GATLING);
  assert.ok(got > base, `gatling_fever should raise aspd (${base} -> ${got})`);
});

// --- gatling_fever is gatling-only: no ASPD on a revolver (W_GATLING gate) ---
test("gatling_fever does NOT raise aspd on a revolver (W_GATLING restriction)", () => {
  const base = aspdWith([], REVOLVER);
  const got = aspdWith([{ name: "gatling_fever", level: 10 }], REVOLVER);
  assert.equal(
    got,
    base,
    "gatling_fever must not raise aspd outside W_GATLING",
  );
});

// --- flip_the_coin: rocalc models the coin-damage bonus -> damage.ave ---
test("flip_the_coin raises damage.ave (coin-damage model)", () => {
  const base = aveWith([], REVOLVER);
  const got = aveWith([{ name: "flip_the_coin", level: 5 }], REVOLVER);
  assert.ok(
    got > base,
    `flip_the_coin should raise damage.ave (${base} -> ${got})`,
  );
});

// --- reset() isolation ---
test("reset() clears Gunslinger buff banks (no hit leak)", () => {
  const s = gsShim(REVOLVER);
  const plain = s.readDerivedStats().hit;
  s.setBuffs([{ name: "single_action", level: 10 }]);
  s.readDerivedStats();
  s.reset();
  s.setClass("gunslinger");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 50, agi: 90, vit: 30, int: 1, dex: 90, luk: 20 });
  s.equip("weapon", { id: REVOLVER });
  const after = s.readDerivedStats().hit;
  assert.equal(after, plain, "reset must clear single_action (no hit leak)");
});
