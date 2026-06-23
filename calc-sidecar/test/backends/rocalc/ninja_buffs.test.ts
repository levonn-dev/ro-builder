import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Ninja self-buffs ride the rocalc job-buff bank (m_JobBuff[44] = [537,406,393,404]);
// all three use the skill_slot driver. Ninja is an Expanded class -- nothing is
// inherited. The test rig is a Huuma Wing Shuriken (the iconic Ninja throwing
// weapon), which serves all three buffs: ninja_aura raises damage.ave even on a
// high-ATK weapon, and the two inert buffs stay inert even with a throwing weapon.
//   ninja_aura       -> readCombatResults().damage.ave  (+STR/INT; scored)
//   ninja_mastery    -> inert (SP recovery; no scored field)
//   throwing_mastery -> inert (throw-skill damage the auto-attack sim does not compute)
// Buffs apply before the enemy is set (production order). ninja_aura at lv1 rounds
// away (+1 ATK), so the "raises" assertion uses lv5.

const HUUMA = 13300; // Huuma Wing Shuriken (W_HUUMA)

function njShim() {
  const s = createShim();
  s.setClass("ninja");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 90, agi: 60, vit: 40, int: 60, dex: 90, luk: 40 });
  s.equip("weapon", { id: HUUMA });
  return s;
}

const ENEMY = {
  hp: 50000,
  atk_min: 1500,
  atk_max: 2000,
  def: 20,
  mdef: 10,
  race: "RC_Brute",
  element: "Ele_Neutral",
  element_lv: 1,
  size: "Size_Medium",
  level: 90,
};

type Buff = { name: string; level: number };

function aveWith(buffs: Buff[]): number {
  const s = njShim();
  if (buffs.length) s.setBuffs(buffs);
  s.setEnemyInline(ENEMY);
  const ave = s.readCombatResults().damage.ave;
  assert.ok(ave != null, "damage.ave must be numeric");
  return ave as number;
}

test("ninja_aura raises damage.ave at lv5", () => {
  const base = aveWith([]);
  const got = aveWith([{ name: "ninja_aura", level: 5 }]);
  assert.ok(got > base, `ninja_aura lv5 should raise damage.ave (${base} -> ${got})`);
});

test("ninja_aura scales with level (lv1 < lv5)", () => {
  const lo = aveWith([{ name: "ninja_aura", level: 1 }]);
  const hi = aveWith([{ name: "ninja_aura", level: 5 }]);
  assert.ok(hi > lo, `ninja_aura lv5 should exceed lv1 (${lo} -> ${hi})`);
});

test("throwing_mastery is wired but inert (even with a Huuma equipped)", () => {
  const base = aveWith([]);
  const got = aveWith([{ name: "throwing_mastery", level: 10 }]);
  assert.equal(got, base, "throwing_mastery must not move damage.ave (boosts throw skills only)");
});

test("ninja_mastery is wired but inert", () => {
  const base = aveWith([]);
  const got = aveWith([{ name: "ninja_mastery", level: 10 }]);
  assert.equal(got, base, "ninja_mastery must not move damage.ave (SP recovery only)");
});

test("reset() clears Ninja buffs (no atk.base leak)", () => {
  const s = njShim();
  const plain = s.readDerivedStats().atk.base;
  s.setBuffs([{ name: "ninja_aura", level: 5 }]);
  s.readDerivedStats();
  s.reset();
  s.setClass("ninja");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 90, agi: 60, vit: 40, int: 60, dex: 90, luk: 40 });
  s.equip("weapon", { id: HUUMA });
  const after = s.readDerivedStats().atk.base;
  assert.equal(after, plain, "reset must clear ninja_aura (no atk leak)");
});
