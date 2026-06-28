import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Regression guard for the SkillSubNum control. rocalc injects a per-skill
// sub-parameter <select>/<input> into the AASkill container (Storm Gust hit
// count, Venom Splasher Poison React level, Charge Attack enemy distance, ...).
// The synthetic form seeds the SkillSubNum name INSIDE AASkill so a live
// form-name getter exists without colliding with the injected control. Before
// that fix, select-type sub-param skills threw "Cannot read properties of
// undefined (reading 'options')". These cases drive such skills end to end and
// assert they produce a real number.

const PORING = 1002;

function score(
  cls: string,
  base: number,
  job: number,
  stats: any,
  name: string,
) {
  const shim = createShim();
  shim.setClass(cls);
  shim.setLevel({ base, job });
  shim.setStats(stats);
  shim.setAttackSkills([{ name, level: 10, primary: true }]);
  shim.setEnemy(PORING);
  return shim.readCombatResults().skills?.[0];
}

const WIZARD = { str: 1, agi: 1, vit: 1, int: 99, dex: 90, luk: 1 };
const SINX = { str: 99, agi: 60, vit: 1, int: 1, dex: 60, luk: 1 };
const KNIGHT = { str: 99, agi: 1, vit: 40, int: 1, dex: 60, luk: 1 };

test("wizard Storm Gust (select-type SkillSubNum) computes instead of crashing", () => {
  const k = score("wizard", 99, 50, WIZARD, "storm_gust");
  assert.ok(
    k && k.damage.ave !== null && k.damage.ave > 0,
    `storm_gust=${k?.damage.ave}`,
  );
  assert.ok(
    k!.uncertainty,
    "storm_gust should carry the hit-count uncertainty note",
  );
});

test("assassin_cross Venom Splasher (select-type SkillSubNum) computes", () => {
  const k = score("assassin_cross", 99, 70, SINX, "venom_splasher");
  assert.ok(
    k && k.damage.ave !== null && k.damage.ave > 0,
    `venom_splasher=${k?.damage.ave}`,
  );
});

test("knight Charge Attack (select-type SkillSubNum) computes", () => {
  const k = score("knight", 99, 50, KNIGHT, "charge_attack");
  assert.ok(
    k && k.damage.ave !== null && k.damage.ave > 0,
    `charge_attack=${k?.damage.ave}`,
  );
});
