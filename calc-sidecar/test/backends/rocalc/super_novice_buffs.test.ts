import { test, before } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Super Novice: the No-Death Bonus (rocalc id 309 in m_JobBuff[20]): +10 to ALL
// stats, flat. A class-innate buff (no learnable skill anchor) -- the Go
// resolver gates it via ClassInnateBuffs(super_novice) and drives it at a
// fixed level 1. See super_novice_buffs.test.ts. The other Super-Novice bank
// entries are excluded: 253 Fury (no anchor, out of scope), 310 Undying
// (-1 penalty), 385 SN-Spirit (inert + external SL_SUPERNOVICE), 196 Steel
// Body (MO_STEELBODY not in the SN tree), 537/59 (carry-weight).

const DAGGER = 1207;

// createShim (jsdom + calc engine) and setClass are the expensive steps, so the
// class is set once in before() and the shim reused. reset() preserves the class
// but clears the buff banks and rolls back level / stats / equipment, so
// fresh() re-applies the build to give every test a clean, leak-free baseline.
let shim: ReturnType<typeof createShim>;

before(() => {
  shim = createShim();
  shim.setClass("super_novice");
});

function fresh() {
  shim.reset();
  shim.setLevel({ base: 99, job: 70 });
  shim.setStats({ str: 50, agi: 50, vit: 50, int: 50, dex: 50, luk: 50 });
  shim.equip("weapon", { id: DAGGER });
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

function read(buffs: Buff[]) {
  const s = fresh();
  if (buffs.length) s.setBuffs(buffs);
  s.setEnemyInline(ENEMY);
  const d = s.readDerivedStats();
  return {
    maxHp: d.maxHp as number,
    atkBase: (d.atk as { base: number }).base,
    defSoft: (d.def as { soft: number }).soft,
    hit: d.hit as number,
  };
}

test("no_death_bonus raises all stats (maxHp, atk, def)", () => {
  const base = read([]);
  const got = read([{ name: "no_death_bonus", level: 1 }]);
  assert.ok(got.maxHp > base.maxHp, `maxHp ${base.maxHp} -> ${got.maxHp}`);
  assert.ok(
    got.atkBase > base.atkBase,
    `atk.base ${base.atkBase} -> ${got.atkBase}`,
  );
  assert.ok(
    got.defSoft > base.defSoft,
    `def.soft ${base.defSoft} -> ${got.defSoft}`,
  );
});

test("inherited owls_eye drives on super_novice (skill_slot)", () => {
  const base = read([]);
  const got = read([{ name: "owls_eye", level: 10 }]);
  assert.ok(
    got.hit > base.hit,
    `owls_eye should raise hit via DEX (${base.hit} -> ${got.hit})`,
  );
});

test("inherited blessing drives on super_novice (support_slot)", () => {
  const base = read([]);
  const got = read([{ name: "blessing", level: 10 }]);
  // Blessing raises STR/INT/DEX -> hit (DEX) and atk (STR) rise.
  assert.ok(
    got.hit > base.hit || got.atkBase > base.atkBase,
    `blessing should raise a stat (hit ${base.hit}->${got.hit}, atk ${base.atkBase}->${got.atkBase})`,
  );
});

test("reset() clears no_death_bonus (no maxHp leak)", () => {
  const baseMaxHp = read([]).maxHp;
  const s = fresh();
  s.setBuffs([{ name: "no_death_bonus", level: 1 }]);
  s.readDerivedStats();
  s.reset();
  s.setClass("super_novice");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 50, agi: 50, vit: 50, int: 50, dex: 50, luk: 50 });
  s.equip("weapon", { id: DAGGER });
  assert.equal(
    s.readDerivedStats().maxHp,
    baseMaxHp,
    "reset must clear no_death_bonus",
  );
});
