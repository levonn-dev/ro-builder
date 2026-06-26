import { test, before } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

let shim: ReturnType<typeof createShim>;

before(() => {
  shim = createShim();
  shim.setClass("professor");
});

function profShim() {
  shim.reset();
  shim.setLevel({ base: 99, job: 50 });
  shim.setStats({ str: 70, agi: 40, vit: 40, int: 80, dex: 80, luk: 20 });
  return shim;
}
// Neutral medium target that also deals damage (for incoming/EHP checks).
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

// Each endow changes auto-attack damage vs a target whose element gives the
// endow a non-100% modifier (mirrors the Mild Wind / Aspersio tests).
const ENDOWS: [string, string, string][] = [
  ["flame_launcher", "fire", "Ele_Earth"],
  ["frost_weapon", "water", "Ele_Fire"],
  ["lightning_loader", "wind", "Ele_Water"],
  ["seismic_weapon", "earth", "Ele_Wind"],
];
for (const [name, element, targetEle] of ENDOWS) {
  test(`${name} endow changes damage vs ${targetEle}`, () => {
    const tgt = { ...TARGET, element: targetEle as typeof TARGET.element };
    const base = profShim();
    base.setEnemyInline(tgt);
    const plain = base.readCombatResults().damage.ave;
    const s = profShim();
    s.setBuffs([{ name, level: 5, element }]);
    s.setEnemyInline(tgt);
    const endowed = s.readCombatResults().damage.ave;
    assert.ok(plain != null && endowed != null, "damage.ave must be numeric");
    assert.notEqual(
      endowed,
      plain,
      `${name} should change damage vs ${targetEle}`,
    );
  });
}

test("dragonology raises derived MATK", () => {
  const base = profShim().readDerivedStats().matk.max;
  const s = profShim();
  s.setBuffs([{ name: "dragonology", level: 5 }]);
  assert.ok(
    s.readDerivedStats().matk.max > base,
    "dragonology should raise MATK",
  );
});

test("energy_coat lowers incoming physical damage", () => {
  const base = profShim();
  base.setEnemyInline(TARGET);
  const plain = base.readCombatResults().incoming.ave;
  const s = profShim();
  s.setBuffs([{ name: "energy_coat", level: 1 }]);
  s.setEnemyInline(TARGET);
  const reduced = s.readCombatResults().incoming.ave;
  assert.ok(plain != null && reduced != null, "incoming.ave must be numeric");
  assert.ok(
    (reduced as number) < (plain as number),
    `energy coat should lower incoming (${plain} -> ${reduced})`,
  );
});

test("spider_web doubles fire damage to the webbed target", () => {
  const fireOnly = profShim();
  fireOnly.setBuffs([{ name: "flame_launcher", level: 5, element: "fire" }]);
  fireOnly.setEnemyInline(TARGET);
  const fire = fireOnly.readCombatResults().damage.ave;
  const webbed = profShim();
  webbed.setBuffs([
    { name: "flame_launcher", level: 5, element: "fire" },
    { name: "spider_web", level: 1 },
  ]);
  webbed.setEnemyInline(TARGET);
  const doubled = webbed.readCombatResults().damage.ave;
  assert.ok(fire != null && doubled != null, "damage.ave must be numeric");
  assert.ok(
    (doubled as number) > (fire as number),
    `spider web should raise fire damage (${fire} -> ${doubled})`,
  );
});

test("mind_breaker applies cleanly (enemy MDEF debuff; derived-only in the auto-attack sim)", () => {
  const s = profShim();
  s.setBuffs([{ name: "mind_breaker", level: 5 }]);
  s.setEnemyInline(TARGET);
  assert.ok(
    s.readCombatResults().damage.ave != null,
    "build should still score with mind_breaker applied",
  );
});

test("study/foresight/double_casting apply cleanly (not scored in the auto-attack sim)", () => {
  for (const name of ["study", "foresight", "double_casting"]) {
    const s = profShim();
    s.setBuffs([{ name, level: 1 }]);
    s.setEnemyInline(TARGET);
    assert.ok(
      s.readCombatResults().damage.ave != null,
      `${name} should not break scoring`,
    );
  }
});
