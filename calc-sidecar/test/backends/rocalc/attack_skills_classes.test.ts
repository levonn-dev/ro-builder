import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Per-class attack-skill coverage beyond the Taekwon kicks. Each case spins its
// own shim (distinct class). Assertions are relational (skill > auto) and
// structural (hit count, breakdown name) rather than exact damage, so they stay
// stable across rocalc data refreshes. Baseline numbers from the 2026-06-26
// probe are in the comments for reference.

const PORING = 1002;

interface Build {
  cls: string;
  base: number;
  job: number;
  stats: {
    str: number;
    agi: number;
    vit: number;
    int: number;
    dex: number;
    luk: number;
  };
}

function scoreSkill(b: Build, name: string, level: number) {
  const shim = createShim();
  shim.setClass(b.cls);
  shim.setLevel({ base: b.base, job: b.job });
  shim.setStats(b.stats);
  shim.setEnemy(PORING);
  const auto = shim.readCombatResults().damage.ave;

  shim.reset();
  shim.setLevel({ base: b.base, job: b.job });
  shim.setStats(b.stats);
  shim.setAttackSkills([{ name, level, primary: true }]);
  shim.setEnemy(PORING);
  const c = shim.readCombatResults();
  return { auto, results: c, skill: c.skills?.[0] };
}

const SINX: Build = {
  cls: "assassin_cross",
  base: 99,
  job: 70,
  stats: { str: 99, agi: 60, vit: 1, int: 1, dex: 60, luk: 1 },
};
const KNIGHT: Build = {
  cls: "knight",
  base: 99,
  job: 50,
  stats: { str: 99, agi: 1, vit: 40, int: 1, dex: 60, luk: 1 },
};
const WIZARD: Build = {
  cls: "wizard",
  base: 99,
  job: 50,
  stats: { str: 1, agi: 1, vit: 1, int: 99, dex: 90, luk: 1 },
};
const MONK: Build = {
  cls: "monk",
  base: 99,
  job: 50,
  stats: { str: 90, agi: 1, vit: 40, int: 40, dex: 60, luk: 1 },
};

test("assassin_cross Sonic Blow: 8-hit melee far above auto-attack (probe ~1776 vs auto 222)", () => {
  const { auto, results, skill } = scoreSkill(SINX, "sonic_blow", 10);
  assert.ok(skill && auto !== null);
  assert.equal(skill.name, "sonic_blow");
  assert.equal(skill.hits, 8);
  assert.ok(
    skill.damage.ave! > auto,
    `sonic_blow ${skill.damage.ave} should exceed auto ${auto}`,
  );
  // primary drives the top-level damage cell
  assert.equal(results.damage.ave, skill.damage.ave);
});

test("knight Bowling Bash exceeds auto-attack (probe ~2208 vs auto 220)", () => {
  const { auto, skill } = scoreSkill(KNIGHT, "bowling_bash", 10);
  assert.ok(skill && auto !== null);
  assert.equal(skill.name, "bowling_bash");
  assert.ok(
    skill.damage.ave! > auto,
    `bowling_bash ${skill.damage.ave} should exceed auto ${auto}`,
  );
});

test("wizard Lightning Bolt: magic damage flows through A_ActiveSkill (probe ~7710 vs auto 20)", () => {
  const { auto, skill } = scoreSkill(WIZARD, "lightning_bolt", 10);
  assert.ok(skill && auto !== null);
  assert.equal(skill.name, "lightning_bolt");
  // The capability guard: magic must compute a real MATK-scaled number, not
  // silently fall back to the tiny barehanded auto-attack.
  assert.ok(
    skill.damage.ave! > auto,
    `lightning_bolt ${skill.damage.ave} should exceed auto ${auto}`,
  );
  assert.ok(
    skill.damage.ave! > 1000,
    `lightning_bolt ${skill.damage.ave} should be MATK-scaled (>1000)`,
  );
});

test("monk Asura Strike scores off a near-full SP pool (probe ~15573 vs auto 190)", () => {
  const { auto, skill } = scoreSkill(MONK, "asura_strike", 5);
  assert.ok(skill && auto !== null);
  assert.equal(skill.name, "asura_strike");
  // Asura's Remaining-SP sub-parameter defaults to maxSP-1. This also guards
  // the SkillSubNum fix: if the standalone stub collision regressed, Asura
  // would read SP 0 and collapse to ~2719. >5000 catches that.
  assert.ok(
    skill.damage.ave! > 5000,
    `asura_strike ${skill.damage.ave} should reflect a full SP pool (>5000)`,
  );
});
