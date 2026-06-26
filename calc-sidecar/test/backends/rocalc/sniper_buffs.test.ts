import { test, before } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Sniper with a Hunter Bow equipped (iRO 1718). Bow ATK scales with DEX, so the
// DEX/ATK steroids move scored damage; HIT/flee steroids move readDerivedStats.
// rocalc scores bows without an arrow (damage is lower than in-game, but the
// buffed-vs-unbuffed comparison is valid).
//
// The shim (jsdom + class load) is the expensive part, so it is created once in
// before() and reused. reset() preserves the class but clears the buff / debuff
// / land / music banks, so fresh() (reset + reconfigure) gives every test a
// clean, leak-free baseline without re-paying the ~4s createShim+setClass.
let shim: ReturnType<typeof createShim>;
before(() => {
  shim = createShim();
  shim.setClass("sniper");
});
function fresh() {
  shim.reset();
  shim.setLevel({ base: 99, job: 70 });
  shim.setStats({ str: 1, agi: 90, vit: 40, int: 1, dex: 99, luk: 40 });
  shim.equip("weapon", { id: 1718 });
  return shim;
}

const TARGET = {
  hp: 50000,
  atk_min: 300,
  atk_max: 500,
  def: 20,
  mdef: 10,
  race: "RC_Brute" as const,
  element: "Ele_Neutral" as const,
  element_lv: 1 as const,
  size: "Size_Medium" as const,
  level: 80,
};

test("owls_eye raises auto-attack damage (DEX -> bow ATK)", () => {
  const base = fresh();
  base.setEnemyInline(TARGET);
  const plain = base.readCombatResults().damage.ave;
  const s = fresh();
  s.setBuffs([{ name: "owls_eye", level: 10 }]);
  s.setEnemyInline(TARGET);
  const got = s.readCombatResults().damage.ave;
  assert.ok(plain != null && got != null, "damage.ave must be numeric");
  assert.ok(
    (got as number) > (plain as number),
    `owls_eye should raise damage (${plain} -> ${got})`,
  );
});

test("owls_eye and vultures_eye scale with level (5 < 10)", () => {
  // vultures_eye is +1 HIT/level directly; owls_eye is +1 DEX/level which feeds
  // HIT indirectly. DerivedStats exposes no raw dex, so both are checked via raw
  // HIT (readDerivedStats, uncapped) -- it rises monotonically with level either
  // way. combat.hit caps at 100 so is not used here.
  for (const name of ["owls_eye", "vultures_eye"]) {
    fresh();
    shim.setBuffs([{ name, level: 5 }]);
    const loHit = shim.readDerivedStats().hit;
    fresh();
    shim.setBuffs([{ name, level: 10 }]);
    const hiHit = shim.readDerivedStats().hit;
    assert.ok(hiHit > loHit, `${name} level 10 HIT should exceed level 5`);
  }
});

test("vultures_eye raises HIT", () => {
  const base = fresh().readDerivedStats().hit;
  const s = fresh();
  s.setBuffs([{ name: "vultures_eye", level: 10 }]);
  assert.ok(s.readDerivedStats().hit > base, "vultures_eye should raise HIT");
});

test("improve_concentration raises auto-attack damage (AGI/DEX steroid)", () => {
  const base = fresh();
  base.setEnemyInline(TARGET);
  const plain = base.readCombatResults().damage.ave;
  const s = fresh();
  s.setBuffs([{ name: "improve_concentration", level: 10 }]);
  s.setEnemyInline(TARGET);
  const got = s.readCombatResults().damage.ave;
  assert.ok(plain != null && got != null, "damage.ave must be numeric");
  assert.ok(
    (got as number) > (plain as number),
    `improve_concentration should raise damage (${plain} -> ${got})`,
  );
});

test("true_sight raises auto-attack damage (all stats + ATK)", () => {
  const base = fresh();
  base.setEnemyInline(TARGET);
  const plain = base.readCombatResults().damage.ave;
  const s = fresh();
  s.setBuffs([{ name: "true_sight", level: 10 }]);
  s.setEnemyInline(TARGET);
  const got = s.readCombatResults().damage.ave;
  assert.ok(plain != null && got != null, "damage.ave must be numeric");
  assert.ok(
    (got as number) > (plain as number),
    `true_sight should raise damage (${plain} -> ${got})`,
  );
});

test("wind_walk raises flee", () => {
  const base = fresh().readDerivedStats().flee;
  const s = fresh();
  s.setBuffs([{ name: "wind_walk", level: 10 }]);
  assert.ok(s.readDerivedStats().flee > base, "wind_walk should raise flee");
});

test("beast_bane raises damage vs a Brute target", () => {
  // Beast Bane only adds ATK vs Brute/Insect; TARGET is RC_Brute.
  const base = fresh();
  base.setEnemyInline(TARGET);
  const plain = base.readCombatResults().damage.ave;
  const s = fresh();
  s.setBuffs([{ name: "beast_bane", level: 10 }]);
  s.setEnemyInline(TARGET);
  const got = s.readCombatResults().damage.ave;
  assert.ok(plain != null && got != null, "damage.ave must be numeric");
  assert.ok(
    (got as number) > (plain as number),
    `beast_bane should raise damage vs Brute (${plain} -> ${got})`,
  );
});

test("steel_crow applies cleanly and leaves auto-attack damage unchanged", () => {
  // Steel Crow only boosts Blitz Beat damage; the auto-attack sim does not score
  // Blitz Beat, so driving it is a no-op here. Contract: the rocalc id resolves
  // and the build still scores, unchanged.
  const base = fresh();
  base.setEnemyInline(TARGET);
  const plain = base.readCombatResults().damage.ave;
  const s = fresh();
  s.setBuffs([{ name: "steel_crow", level: 10 }]);
  s.setEnemyInline(TARGET);
  const got = s.readCombatResults().damage.ave;
  assert.ok(got != null, "steel_crow should not break scoring");
  assert.equal(got, plain, "steel_crow should not change auto-attack damage");
});

test("reset() clears the Sniper buff bank (no leak across requests)", () => {
  const s = fresh();
  s.setEnemyInline(TARGET);
  const plain = s.readCombatResults().damage.ave;
  s.setBuffs([{ name: "true_sight", level: 10 }]);
  s.readCombatResults();
  s.reset();
  s.setClass("sniper");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 1, agi: 90, vit: 40, int: 1, dex: 99, luk: 40 });
  s.equip("weapon", { id: 1718 });
  s.setEnemyInline(TARGET);
  const after = s.readCombatResults().damage.ave;
  assert.equal(after, plain, "reset must clear true_sight (no leak)");
});
