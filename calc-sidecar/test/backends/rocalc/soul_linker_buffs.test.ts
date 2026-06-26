import { test, before } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Soul Linker self-buffs ride the rocalc job-buff bank (m_JobBuff[43]); all use
// the skill_slot driver. kaina is the only scored buff -- it raises maxSp
// (readDerivedStats().maxSp), +30/level. The 4 shared TaeKwon skills
// (tumbling/peaceful_break/happy_break/kihop) are wired-but-inert: they move no
// scored field. Reset is verified via maxSp (the field kaina moves). Buffs apply
// before the enemy (production order).

const ROD = 1601; // Rod (W_ROD)

let shim: ReturnType<typeof createShim>;

before(() => {
  shim = createShim();
  shim.setClass("soul_linker");
});

function slShim() {
  shim.reset();
  shim.setLevel({ base: 99, job: 70 });
  shim.setStats({ str: 40, agi: 50, vit: 50, int: 80, dex: 70, luk: 30 });
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

function read(buffs: Buff[]): { maxSp: number; ave: number } {
  const s = slShim();
  if (buffs.length) s.setBuffs(buffs);
  s.setEnemyInline(ENEMY);
  const maxSp = s.readDerivedStats().maxSp;
  const ave = s.readCombatResults().damage.ave;
  assert.ok(maxSp != null && ave != null, "maxSp and ave must be numeric");
  return { maxSp: maxSp as number, ave: ave as number };
}

test("kaina raises maxSp at lv7", () => {
  const base = read([]);
  const got = read([{ name: "kaina", level: 7 }]);
  assert.ok(
    got.maxSp > base.maxSp,
    `kaina lv7 should raise maxSp (${base.maxSp} -> ${got.maxSp})`,
  );
});

test("kaina scales with level (lv1 < lv7)", () => {
  const lo = read([{ name: "kaina", level: 1 }]);
  const hi = read([{ name: "kaina", level: 7 }]);
  assert.ok(
    hi.maxSp > lo.maxSp,
    `kaina lv7 should exceed lv1 (${lo.maxSp} -> ${hi.maxSp})`,
  );
});

for (const [name, level] of [
  ["tumbling", 1],
  ["peaceful_break", 10],
  ["happy_break", 10],
  ["kihop", 5],
] as [string, number][]) {
  test(`${name} is wired but inert`, () => {
    const base = read([]);
    const got = read([{ name, level }]);
    assert.equal(got.maxSp, base.maxSp, `${name} must not move maxSp`);
    assert.equal(got.ave, base.ave, `${name} must not move damage.ave`);
  });
}

test("reset() clears kaina (no maxSp leak)", () => {
  const baseMaxSp = read([]).maxSp;
  const s = slShim();
  s.setBuffs([{ name: "kaina", level: 7 }]);
  s.readDerivedStats();
  s.reset();
  s.setClass("soul_linker");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 40, agi: 50, vit: 50, int: 80, dex: 70, luk: 30 });
  s.equip("weapon", { id: ROD });
  const after = s.readDerivedStats().maxSp;
  assert.equal(after, baseMaxSp, "reset must clear kaina (no maxSp leak)");
});
