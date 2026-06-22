import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Star Gladiator self-buffs ride the rocalc job-buff bank (m_JobBuff[42]); all ten
// use the skill_slot driver. The SG_ skills tested here are class-exclusive; the 3
// TK-inherited buffs (mild_wind / spurt / taekwon_ranker) are covered elsewhere.
// The build is barehanded -- the authentic SG auto-attack. Buffs are applied
// BEFORE the enemy is set (production order); union and solar_protection read inert
// otherwise. The three Wrath skills are enemy size+maxHP gated INSIDE rocalc:
//   sls_solar_wrath   -> damage.ave, Small monsters
//   sls_lunar_wrath   -> damage.ave, Medium monsters maxHP >= 6000
//   sls_stellar_wrath -> damage.ave, Large monsters maxHP >= 20000
// Non-Wrath:
//   sls_lunar_protection   -> readDerivedStats().flee
//   sls_stellar_protection -> readDerivedStats().aspd
//   sls_solar_protection   -> readCombatResults().incoming.ave (defensive, lowers)
//   sls_union              -> readCombatResults().damage.ave
//   sls_demon/knowledge/blessing -> inert (status; no scored field)

function sgShim() {
  const s = createShim();
  s.setClass("star_gladiator");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 90, agi: 30, vit: 60, int: 20, dex: 90, luk: 60 });
  return s; // barehanded
}

function enemy(size: string, hp: number) {
  return {
    hp,
    atk_min: 1500,
    atk_max: 2000,
    def: 20,
    mdef: 10,
    race: "RC_Brute",
    element: "Ele_Neutral",
    element_lv: 1,
    size,
    level: 90,
  };
}
const SMALL = enemy("Size_Small", 50000);
const MEDIUM = enemy("Size_Medium", 50000); // hp >= 6000
const LARGE = enemy("Size_Large", 50000); // hp >= 20000

type Buff = { name: string; level: number };

function aveVs(buffs: Buff[], e: ReturnType<typeof enemy>): number {
  const s = sgShim();
  if (buffs.length) s.setBuffs(buffs);
  s.setEnemyInline(e);
  const ave = s.readCombatResults().damage.ave;
  assert.ok(ave != null, "damage.ave must be numeric");
  return ave as number;
}
function incomingVs(buffs: Buff[], e: ReturnType<typeof enemy>): number {
  const s = sgShim();
  if (buffs.length) s.setBuffs(buffs);
  s.setEnemyInline(e);
  const inc = s.readCombatResults().incoming.ave;
  assert.ok(inc != null, "incoming.ave must be numeric");
  return inc as number;
}
function fleeWith(buffs: Buff[]): number {
  const s = sgShim();
  if (buffs.length) s.setBuffs(buffs);
  return s.readDerivedStats().flee;
}
function aspdWith(buffs: Buff[]): number {
  const s = sgShim();
  if (buffs.length) s.setBuffs(buffs);
  return s.readDerivedStats().aspd;
}

// --- Wrath: size-matched fires; wrong size stays inert ---
test("sls_solar_wrath raises damage.ave vs a Small enemy", () => {
  const base = aveVs([], SMALL);
  const got = aveVs([{ name: "sls_solar_wrath", level: 3 }], SMALL);
  assert.ok(
    got > base,
    `solar_wrath should raise ave vs Small (${base} -> ${got})`,
  );
});

test("sls_solar_wrath is inert vs a Medium enemy (size gate)", () => {
  const base = aveVs([], MEDIUM);
  const got = aveVs([{ name: "sls_solar_wrath", level: 3 }], MEDIUM);
  assert.equal(got, base, "solar_wrath must not fire outside Small monsters");
});

test("sls_lunar_wrath raises damage.ave vs a Medium enemy (hp >= 6000)", () => {
  const base = aveVs([], MEDIUM);
  const got = aveVs([{ name: "sls_lunar_wrath", level: 3 }], MEDIUM);
  assert.ok(
    got > base,
    `lunar_wrath should raise ave vs Medium (${base} -> ${got})`,
  );
});

test("sls_lunar_wrath is inert vs a Small enemy (size gate)", () => {
  const base = aveVs([], SMALL);
  const got = aveVs([{ name: "sls_lunar_wrath", level: 3 }], SMALL);
  assert.equal(got, base, "lunar_wrath must not fire outside Medium monsters");
});

test("sls_stellar_wrath raises damage.ave vs a Large enemy (hp >= 20000)", () => {
  const base = aveVs([], LARGE);
  const got = aveVs([{ name: "sls_stellar_wrath", level: 3 }], LARGE);
  assert.ok(
    got > base,
    `stellar_wrath should raise ave vs Large (${base} -> ${got})`,
  );
});

test("sls_stellar_wrath is inert vs a Medium enemy (size gate)", () => {
  const base = aveVs([], MEDIUM);
  const got = aveVs([{ name: "sls_stellar_wrath", level: 3 }], MEDIUM);
  assert.equal(got, base, "stellar_wrath must not fire outside Large monsters");
});

// --- Protection / Union ---
test("sls_lunar_protection raises flee", () => {
  const base = fleeWith([]);
  const got = fleeWith([{ name: "sls_lunar_protection", level: 4 }]);
  assert.ok(
    got > base,
    `lunar_protection should raise flee (${base} -> ${got})`,
  );
});

test("sls_stellar_protection raises aspd", () => {
  const base = aspdWith([]);
  const got = aspdWith([{ name: "sls_stellar_protection", level: 4 }]);
  assert.ok(
    got > base,
    `stellar_protection should raise aspd (${base} -> ${got})`,
  );
});

test("sls_solar_protection lowers incoming damage (defensive)", () => {
  const base = incomingVs([], MEDIUM);
  const got = incomingVs([{ name: "sls_solar_protection", level: 4 }], MEDIUM);
  assert.ok(
    got < base,
    `solar_protection should lower incoming (${base} -> ${got})`,
  );
});

test("sls_union raises damage.ave", () => {
  const base = aveVs([], MEDIUM);
  const got = aveVs([{ name: "sls_union", level: 1 }], MEDIUM);
  assert.ok(got > base, `union should raise ave (${base} -> ${got})`);
});

// --- status no-ops: bound controls rocalc exposes but the sim does not score ---
for (const name of ["sls_demon", "sls_knowledge", "sls_blessing"]) {
  test(`${name} is wired and leaves damage.ave unchanged`, () => {
    const base = aveVs([], MEDIUM);
    const got = aveVs([{ name, level: 5 }], MEDIUM);
    assert.equal(got, base, `${name} must not move damage.ave (inert)`);
  });
}

// --- scaling: a Wrath at level 1 < level 3 (size-matched) ---
test("sls_lunar_wrath scales with level", () => {
  const lo = aveVs([{ name: "sls_lunar_wrath", level: 1 }], MEDIUM);
  const hi = aveVs([{ name: "sls_lunar_wrath", level: 3 }], MEDIUM);
  assert.ok(hi > lo, `lunar_wrath lv3 should exceed lv1 (${lo} -> ${hi})`);
});

// --- reset() isolation: no buff leak across requests ---
test("reset() clears Star Gladiator buffs (no flee leak)", () => {
  const s = sgShim();
  const plain = s.readDerivedStats().flee;
  s.setBuffs([{ name: "sls_lunar_protection", level: 4 }]);
  s.readDerivedStats();
  s.reset();
  s.setClass("star_gladiator");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 90, agi: 30, vit: 60, int: 20, dex: 90, luk: 60 });
  const after = s.readDerivedStats().flee;
  assert.equal(
    after,
    plain,
    "reset must clear sls_lunar_protection (no flee leak)",
  );
});
