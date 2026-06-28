import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Regression guard for spirit-sphere attack skills. Finger Offensive and Ki
// Explosion (Excruciating Palm) explode spirit spheres and read 0 unless the
// support-bank control A2_Skill12 (# of Spirit Spheres) is driven > 0. The
// binding carries a sphere count; computeSkillBreakdown sets A2_Skill12 + the
// support section gate while the skill is active, and clears it for others.

const PORING = 1002;
const MONK = { str: 90, agi: 1, vit: 40, int: 40, dex: 60, luk: 1 };

function monkShim() {
  const s = createShim();
  s.setClass("monk");
  s.setLevel({ base: 99, job: 50 });
  s.setStats(MONK);
  return s;
}

test("Finger Offensive deals sphere damage (0 without spheres)", () => {
  const s = monkShim();
  s.setAttackSkills([{ name: "finger_offensive", level: 5, primary: true }]);
  s.setEnemy(PORING);
  const k = s.readCombatResults().skills?.[0];
  assert.ok(
    k && k.damage.ave !== null && k.damage.ave > 0,
    `finger_offensive=${k?.damage.ave}`,
  );
});

test("Ki Explosion (Excruciating Palm) deals sphere damage", () => {
  const s = monkShim();
  s.setAttackSkills([{ name: "excruciating_palm", level: 1, primary: true }]);
  s.setEnemy(PORING);
  const k = s.readCombatResults().skills?.[0];
  assert.ok(
    k && k.damage.ave !== null && k.damage.ave > 0,
    `excruciating_palm=${k?.damage.ave}`,
  );
});

test("spheres do not leak to a non-sphere skill in the same breakdown", () => {
  // chain_combo (no spheres) scored alongside finger_offensive (5 spheres).
  const withFinger = monkShim();
  withFinger.setAttackSkills([
    { name: "finger_offensive", level: 5 },
    { name: "chain_combo", level: 5, primary: true },
  ]);
  withFinger.setEnemy(PORING);
  const mixed = Object.fromEntries(
    (withFinger.readCombatResults().skills ?? []).map((k) => [
      k.name,
      k.damage.ave,
    ]),
  );

  // chain_combo scored alone must match its value when scored next to a sphere skill.
  const alone = monkShim();
  alone.setAttackSkills([{ name: "chain_combo", level: 5, primary: true }]);
  alone.setEnemy(PORING);
  const chainAlone = alone.readCombatResults().skills?.[0].damage.ave;

  assert.equal(
    mixed["chain_combo"],
    chainAlone,
    "chain_combo must not inherit spheres",
  );
});
