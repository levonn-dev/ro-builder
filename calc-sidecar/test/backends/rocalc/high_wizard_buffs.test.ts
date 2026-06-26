import { test, before } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// High Wizard self-buffs ride the rocalc job-buff bank (m_JobBuff[25]); all use the
// skill_slot driver. soul_drain is scored -- it raises maxSp (readDerivedStats().maxSp),
// ~+2%/level. mystical_amplification raises derived MATK (readDerivedStats().matk),
// not gate-scored in the auto-attack sim (the dragonology precedent). increase_sp_recovery
// is wired-but-inert (SP-regen rate; moves no scored field, including maxSp). Reset is
// verified via maxSp (the field soul_drain moves). Buffs apply before the enemy.

const ROD = 1601; // Rod (W_ROD)

// createShim (jsdom + calc engine) and setClass are the expensive steps, so the
// class is set once in before() and the shim reused. reset() preserves the class
// but clears the buff banks and rolls back level / stats / equipment, so
// fresh() re-applies the build to give every test a clean, leak-free baseline.
let shim: ReturnType<typeof createShim>;

before(() => {
  shim = createShim();
  shim.setClass("high_wizard");
});

function fresh() {
  shim.reset();
  shim.setLevel({ base: 99, job: 70 });
  shim.setStats({ str: 1, agi: 1, vit: 50, int: 99, dex: 90, luk: 1 });
  shim.equip("weapon", { id: ROD });
  return shim;
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

function read(buffs: Buff[]): { maxSp: number; matkMax: number; ave: number } {
  const s = fresh();
  if (buffs.length) s.setBuffs(buffs);
  s.setEnemyInline(ENEMY);
  const d = s.readDerivedStats();
  const ave = s.readCombatResults().damage.ave;
  const maxSp = d.maxSp;
  const matkMax = d.matk.max;
  assert.ok(
    maxSp != null && matkMax != null && ave != null,
    "maxSp, matk.max, and ave must be numeric",
  );
  return {
    maxSp: maxSp as number,
    matkMax: matkMax as number,
    ave: ave as number,
  };
}

test("soul_drain raises maxSp at lv10", () => {
  const base = read([]);
  const got = read([{ name: "soul_drain", level: 10 }]);
  assert.ok(
    got.maxSp > base.maxSp,
    `soul_drain lv10 should raise maxSp (${base.maxSp} -> ${got.maxSp})`,
  );
});

test("soul_drain scales with level (lv1 < lv10)", () => {
  const lo = read([{ name: "soul_drain", level: 1 }]);
  const hi = read([{ name: "soul_drain", level: 10 }]);
  assert.ok(
    hi.maxSp > lo.maxSp,
    `soul_drain lv10 should exceed lv1 (${lo.maxSp} -> ${hi.maxSp})`,
  );
});

test("mystical_amplification raises derived MATK at lv10", () => {
  const base = read([]);
  const got = read([{ name: "mystical_amplification", level: 10 }]);
  assert.ok(
    got.matkMax > base.matkMax,
    `mystical_amplification lv10 should raise matk.max (${base.matkMax} -> ${got.matkMax})`,
  );
});

test("increase_sp_recovery is wired but inert", () => {
  const base = read([]);
  const got = read([{ name: "increase_sp_recovery", level: 10 }]);
  assert.equal(
    got.maxSp,
    base.maxSp,
    "increase_sp_recovery must not move maxSp",
  );
  assert.equal(
    got.matkMax,
    base.matkMax,
    "increase_sp_recovery must not move matk",
  );
  assert.equal(
    got.ave,
    base.ave,
    "increase_sp_recovery must not move damage.ave",
  );
});

test("reset() clears soul_drain (no maxSp leak)", () => {
  const baseMaxSp = read([]).maxSp;
  const s = fresh();
  s.setBuffs([{ name: "soul_drain", level: 10 }]);
  s.readDerivedStats();
  s.reset();
  s.setClass("high_wizard");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 1, agi: 1, vit: 50, int: 99, dex: 90, luk: 1 });
  s.equip("weapon", { id: ROD });
  const after = s.readDerivedStats().maxSp;
  assert.equal(after, baseMaxSp, "reset must clear soul_drain (no maxSp leak)");
});
