import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Champion self-buffs all live in the rocalc job-buff bank (m_JobBuff[29], which
// is identical to Monk's [15]), so each rides the skill_slot driver. The test
// weapon is a Knuckle (iRO 1807 Fist). The scored metric differs by buff:
// +ATK / mastery steroids move readCombatResults().damage.ave; Triple Attack's
// proc lands in damage.secondAve (the second-attack field, per the contract);
// Fury's crit lands in readDerivedStats().cri; Dodge moves readDerivedStats()
// .flee. Demon Bane is +ATK vs Undead element / Demon race, so it is exercised
// against an Ele_Undead target (and shown inert vs a Neutral DemiHuman).
const KNUCKLE = 1807; // Fist (knuckle)

function chShim() {
  const s = createShim();
  s.setClass("champion");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 90, agi: 70, vit: 40, int: 1, dex: 60, luk: 20 });
  s.equip("weapon", { id: KNUCKLE });
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
const UNDEAD_ELE = { ...NEUTRAL, element: "Ele_Undead" } as const;

function damageVs(
  target: typeof NEUTRAL | typeof UNDEAD_ELE,
  buffName: string,
  level: number,
): number {
  const s = chShim();
  s.setBuffs([{ name: buffName, level }]);
  s.setEnemyInline(target);
  const ave = s.readCombatResults().damage.ave;
  assert.ok(ave != null, `${buffName}: damage.ave must be numeric`);
  return ave as number;
}

function baseDamageVs(target: typeof NEUTRAL | typeof UNDEAD_ELE): number {
  const s = chShim();
  s.setEnemyInline(target);
  const ave = s.readCombatResults().damage.ave;
  assert.ok(ave != null, "base damage.ave must be numeric");
  return ave as number;
}

function secondAveWith(buffs: { name: string; level: number }[]): number {
  const s = chShim();
  if (buffs.length) s.setBuffs(buffs);
  s.setEnemyInline(NEUTRAL);
  const v = s.readCombatResults().damage.secondAve;
  assert.ok(v != null, "secondAve must be numeric");
  return v as number;
}

// --- +ATK / mastery steroid (damage.ave) ---
test("iron_fists raises knuckle damage (+fist mastery)", () => {
  const plain = baseDamageVs(NEUTRAL);
  const got = damageVs(NEUTRAL, "iron_fists", 10);
  assert.ok(got > plain, `iron_fists should raise damage (${plain} -> ${got})`);
});

test("iron_fists scales with level (5 < 10)", () => {
  const lo = damageVs(NEUTRAL, "iron_fists", 5);
  const hi = damageVs(NEUTRAL, "iron_fists", 10);
  assert.ok(
    hi > lo,
    `iron_fists level 10 should exceed level 5 (${lo} -> ${hi})`,
  );
});

// --- Triple Attack: the 3-hit proc lands in damage.secondAve, not damage.ave ---
test("triple_attack raises second-attack (proc) damage (secondAve)", () => {
  const base = secondAveWith([]);
  const got = secondAveWith([{ name: "triple_attack", level: 10 }]);
  assert.ok(
    got > base,
    `triple_attack should raise secondAve (${base} -> ${got})`,
  );
});

// --- Fury (Critical Explosion): raises crit rate (readDerivedStats().cri) ---
test("fury raises crit rate (cri)", () => {
  const s = chShim();
  const base = s.readDerivedStats().cri;
  s.setBuffs([{ name: "fury", level: 5 }]);
  const got = s.readDerivedStats().cri;
  assert.ok(got > base, `fury should raise cri (${base} -> ${got})`);
});

// --- Demon Bane: +ATK vs Undead element / Demon race (damage.ave vs Undead) ---
test("demon_bane raises damage vs an Undead-element target", () => {
  const plain = baseDamageVs(UNDEAD_ELE);
  const got = damageVs(UNDEAD_ELE, "demon_bane", 10);
  assert.ok(
    got > plain,
    `demon_bane should raise damage vs Undead (${plain} -> ${got})`,
  );
});

test("demon_bane does NOT change damage vs a Neutral DemiHuman target", () => {
  const plain = baseDamageVs(NEUTRAL);
  const got = damageVs(NEUTRAL, "demon_bane", 10);
  assert.equal(
    got,
    plain,
    "demon_bane only applies vs Undead element / Demon race",
  );
});

// --- Dodge: flee steroid (readDerivedStats().flee) ---
test("dodge raises flee", () => {
  const s = chShim();
  const base = s.readDerivedStats().flee;
  s.setBuffs([{ name: "dodge", level: 10 }]);
  const got = s.readDerivedStats().flee;
  assert.ok(got > base, `dodge should raise flee (${base} -> ${got})`);
});

// --- status no-ops (the auto-attack offense sim does not read these) ---
for (const buff of ["spiritual_cadence", "divine_protection"]) {
  test(`${buff} applies cleanly and leaves knuckle damage unchanged`, () => {
    const plain = baseDamageVs(NEUTRAL);
    const got = damageVs(NEUTRAL, buff, 1);
    assert.equal(got, plain, `${buff} should not change auto-attack damage`);
  });
}

// --- mental_strength: defensive (Steel Body); may LOWER damage via the ASPD
// penalty. Contract: it resolves and the build still scores (non-null, > 0),
// not a no-op equality or a rise. ---
test("mental_strength applies cleanly and the build still scores", () => {
  const got = damageVs(NEUTRAL, "mental_strength", 5);
  assert.ok(got > 0, `mental_strength build must still score (got ${got})`);
});

// --- reset() isolation ---
test("reset() clears the Champion buff banks (no leak across requests)", () => {
  const s = chShim();
  s.setEnemyInline(NEUTRAL);
  const plain = s.readCombatResults().damage.ave;
  s.setBuffs([{ name: "iron_fists", level: 10 }]);
  s.readCombatResults();
  s.reset();
  s.setClass("champion");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 90, agi: 70, vit: 40, int: 1, dex: 60, luk: 20 });
  s.equip("weapon", { id: KNUCKLE });
  s.setEnemyInline(NEUTRAL);
  const after = s.readCombatResults().damage.ave;
  assert.equal(after, plain, "reset must clear iron_fists (no leak)");
});
