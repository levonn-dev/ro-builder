import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Per-class attack-skill coverage beyond the Taekwon kicks. Each case spins its
// own shim (distinct class). Assertions are relational (skill > auto) and
// structural (hit count, breakdown name) rather than exact damage, so they stay
// stable across rocalc data refreshes. Baseline numbers from the 2026-06-26
// probe are in the comments for reference.

const PORING = 1002;
const ZOMBIE = 1015; // Undead-race target for the Priest exorcism skills.

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

function scoreSkill(b: Build, name: string, level: number, enemy = PORING) {
  const shim = createShim();
  shim.setClass(b.cls);
  shim.setLevel({ base: b.base, job: b.job });
  shim.setStats(b.stats);
  shim.setEnemy(enemy);
  const auto = shim.readCombatResults().damage.ave;

  shim.reset();
  shim.setLevel({ base: b.base, job: b.job });
  shim.setStats(b.stats);
  shim.setAttackSkills([{ name, level, primary: true }]);
  shim.setEnemy(enemy);
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

const GUNSLINGER: Build = {
  cls: "gunslinger",
  base: 99,
  job: 50,
  // Test stats, not a real build: STR gives the barehanded ATK floor (no gun
  // equipped here) so the skill clearly clears auto-attack; DEX/LUK feed guns.
  stats: { str: 80, agi: 60, vit: 1, int: 1, dex: 99, luk: 60 },
};
const NINJA: Build = {
  cls: "ninja",
  base: 99,
  job: 50,
  stats: { str: 1, agi: 1, vit: 1, int: 99, dex: 90, luk: 1 },
};
const SOUL_LINKER: Build = {
  cls: "soul_linker",
  base: 99,
  job: 50,
  stats: { str: 1, agi: 1, vit: 1, int: 99, dex: 90, luk: 1 },
};
const WHITESMITH: Build = {
  cls: "whitesmith",
  base: 99,
  job: 70,
  stats: { str: 99, agi: 1, vit: 40, int: 1, dex: 60, luk: 1 },
};
const PRIEST: Build = {
  cls: "priest",
  base: 99,
  job: 50,
  stats: { str: 1, agi: 1, vit: 1, int: 99, dex: 90, luk: 1 },
};

test("gunslinger Desperado computes as a gun skill above auto-attack (probe ~3261)", () => {
  const { auto, skill } = scoreSkill(GUNSLINGER, "desperado", 10);
  assert.ok(skill && auto !== null);
  assert.equal(skill.name, "desperado");
  assert.ok(
    skill.damage.ave! > auto,
    `desperado ${skill.damage.ave} should exceed auto ${auto}`,
  );
});

test("ninja Crimson Fire Petal: a fuzzy-mapped spell computes MATK damage (probe ~1210)", () => {
  const { auto, skill } = scoreSkill(NINJA, "crimson_fire_petal", 10);
  assert.ok(skill && auto !== null);
  assert.equal(skill.name, "crimson_fire_petal");
  // Guards the Ninja name remap (rocalc's old names vs the catalog's renewal
  // names): the binding must hit a real MATK-scaled spell, not auto-fallback.
  assert.ok(
    skill.damage.ave! > 1000,
    `crimson_fire_petal ${skill.damage.ave} should be MATK-scaled (>1000)`,
  );
});

test("soul_linker Esma is a strong MATK bolt above auto (probe ~4460)", () => {
  const { auto, skill } = scoreSkill(SOUL_LINKER, "esma", 10);
  assert.ok(skill && auto !== null);
  assert.equal(skill.name, "esma");
  assert.ok(
    skill.damage.ave! > 1000,
    `esma ${skill.damage.ave} should be MATK-scaled (>1000)`,
  );
});

test("whitesmith Cart Termination scores without the sub-parameter crash (probe ~2251)", () => {
  // Cart Termination carries a cart-weight sub-parameter; this guards that the
  // SkillSubNum machinery feeds it a default instead of throwing.
  const { auto, skill } = scoreSkill(WHITESMITH, "cart_termination", 10);
  assert.ok(skill && auto !== null);
  assert.equal(skill.name, "cart_termination");
  assert.ok(
    skill.damage.ave! > auto,
    `cart_termination ${skill.damage.ave} should exceed auto ${auto}`,
  );
});

test("priest Magnus Exorcismus damages Undead but is zero against non-Undead", () => {
  const vsUndead = scoreSkill(PRIEST, "magnus_exorcismus", 10, ZOMBIE);
  assert.ok(vsUndead.skill && vsUndead.auto !== null);
  assert.ok(
    vsUndead.skill.damage.ave! > 1000,
    `magnus vs Zombie ${vsUndead.skill.damage.ave} should be a real holy nuke (>1000)`,
  );
  // The defining behavior: zero against a non-Undead, non-Demon target.
  const vsPoring = scoreSkill(PRIEST, "magnus_exorcismus", 10, PORING);
  assert.equal(
    vsPoring.skill?.damage.ave,
    0,
    `magnus vs Poring should be 0, got ${vsPoring.skill?.damage.ave}`,
  );
});

test("star_gladiator Warmth (Heat) models a fast DoT: time-to-kill far below auto", () => {
  // Heat's per-hit damage mirrors auto-attack, but its hit interval is far
  // shorter, so battleTimeSec (driven by the primary skill) drops sharply. That
  // timing -- not per-hit damage -- is the modeled signal. Also guards the
  // no-pushback binding choice: the pushback Heat variant returns a null TTK.
  const shim = createShim();
  shim.setClass("star_gladiator");
  const stats = { str: 90, agi: 60, vit: 40, int: 1, dex: 60, luk: 1 };
  shim.setLevel({ base: 99, job: 50 });
  shim.setStats(stats);
  shim.setEnemy(PORING);
  const autoTtk = shim.readCombatResults().battleTimeSec;

  shim.reset();
  shim.setLevel({ base: 99, job: 50 });
  shim.setStats(stats);
  shim.setAttackSkills([{ name: "sun_warmth", level: 3, primary: true }]);
  shim.setEnemy(PORING);
  const c = shim.readCombatResults();
  assert.equal(c.skills?.[0]?.name, "sun_warmth");
  assert.ok(
    autoTtk !== null && c.battleTimeSec !== null,
    "both time-to-kill values should be non-null (no-pushback Heat variant)",
  );
  assert.ok(
    c.battleTimeSec! < autoTtk!,
    `warmth ttk ${c.battleTimeSec} should be below auto ttk ${autoTtk}`,
  );
});
