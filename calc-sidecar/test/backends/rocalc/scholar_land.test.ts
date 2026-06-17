import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

function profShim() {
  const s = createShim();
  s.setClass("professor");
  s.setLevel({ base: 99, job: 50 });
  s.setStats({ str: 80, agi: 40, vit: 40, int: 60, dex: 80, luk: 20 });
  return s;
}
const TARGET = {
  hp: 50000,
  atk_min: 100,
  atk_max: 200,
  def: 20,
  mdef: 10,
  race: "RC_Brute" as const,
  element: "Ele_Neutral" as const,
  element_lv: 1 as const,
  size: "Size_Medium" as const,
  level: 80,
};
const FIRE = { name: "flame_launcher", level: 5, element: "fire" };

test("volcano amplifies fire (flame_launcher) damage", () => {
  const endowOnly = profShim();
  endowOnly.setBuffs([FIRE]);
  endowOnly.setEnemyInline(TARGET);
  const fire = endowOnly.readCombatResults().damage.ave;
  const withLand = profShim();
  withLand.setBuffs([FIRE, { name: "volcano", level: 5 }]);
  withLand.setEnemyInline(TARGET);
  const amped = withLand.readCombatResults().damage.ave;
  assert.ok(fire != null && amped != null, "damage.ave must be numeric");
  assert.ok(
    (amped as number) > (fire as number),
    `volcano should amplify fire (${fire} -> ${amped})`,
  );
});

test("volcano level scales the amplification (lv1 < lv5)", () => {
  const lo = profShim();
  lo.setBuffs([FIRE, { name: "volcano", level: 1 }]);
  lo.setEnemyInline(TARGET);
  const aLo = lo.readCombatResults().damage.ave;
  const hi = profShim();
  hi.setBuffs([FIRE, { name: "volcano", level: 5 }]);
  hi.setEnemyInline(TARGET);
  const aHi = hi.readCombatResults().damage.ave;
  assert.ok(aLo != null && aHi != null, "damage.ave must be numeric");
  assert.ok(
    (aHi as number) > (aLo as number),
    `volcano lv5 should beat lv1 (${aLo} -> ${aHi})`,
  );
});

test("reset() clears the land bank (no Volcano leak across requests)", () => {
  // Baseline: fire endow only.
  const ref = profShim();
  ref.setBuffs([FIRE]);
  ref.setEnemyInline(TARGET);
  const fireOnly = ref.readCombatResults().damage.ave;

  // Same session: apply Volcano (changes damage), then reset and re-apply only
  // the endow. The damage must return exactly to the fire-only baseline.
  const s = profShim();
  s.setBuffs([FIRE, { name: "volcano", level: 5 }]);
  s.setEnemyInline(TARGET);
  assert.notEqual(
    s.readCombatResults().damage.ave,
    fireOnly,
    "volcano should change damage first",
  );

  s.reset();
  s.setClass("professor");
  s.setLevel({ base: 99, job: 50 });
  s.setStats({ str: 80, agi: 40, vit: 40, int: 60, dex: 80, luk: 20 });
  s.setBuffs([FIRE]);
  s.setEnemyInline(TARGET);
  assert.equal(
    s.readCombatResults().damage.ave,
    fireOnly,
    "after reset, volcano must be gone",
  );
});
