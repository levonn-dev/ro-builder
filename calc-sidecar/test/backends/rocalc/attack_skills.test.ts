import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

const PORING = 1002; // iRO mob id; translates to rocalc m_Monster index 272

function tkBare(s: ReturnType<typeof createShim>) {
  s.setClass("taekwon_kid");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 90, agi: 80, vit: 40, int: 1, dex: 70, luk: 1 });
}

test("primary attack skill drives top-level damage above auto-attack", () => {
  const s = createShim();
  tkBare(s);
  s.setEnemy(PORING);
  const auto = s.readCombatResults().damage.ave;

  s.reset();
  tkBare(s);
  s.setAttackSkills([{ name: "roundhouse", level: 7, primary: true }]);
  s.setEnemy(PORING);
  const c = s.readCombatResults();
  assert.ok(c.damage.ave !== null && auto !== null);
  assert.ok(
    c.damage.ave > auto,
    `roundhouse ${c.damage.ave} should exceed auto ${auto}`,
  );
  assert.equal(c.skills?.length, 1);
  assert.equal(c.skills?.[0].name, "roundhouse");
});

test("breakdown reports each declared skill with hit counts; primary drives top-level", () => {
  const s = createShim();
  tkBare(s);
  s.setAttackSkills([
    { name: "tornado_kick", level: 7, primary: true },
    { name: "counter_kick", level: 7 },
  ]);
  s.setEnemy(PORING);
  const c = s.readCombatResults();
  const byName = Object.fromEntries((c.skills ?? []).map((k) => [k.name, k]));
  assert.ok(byName["tornado_kick"] && byName["counter_kick"]);
  assert.equal(byName["counter_kick"].hits, 3); // confirmed in Task 1
  // top-level reflects the primary (tornado_kick), not counter_kick
  assert.equal(c.damage.ave, byName["tornado_kick"].damage.ave);
});

test("absent attack_skills leaves auto-attack behavior unchanged (no skills field)", () => {
  const s = createShim();
  tkBare(s);
  s.setEnemy(PORING);
  const c = s.readCombatResults();
  assert.equal(c.skills, undefined);
});

test("unknown attack-skill name throws", () => {
  const s = createShim();
  tkBare(s);
  assert.throws(() =>
    s.setAttackSkills([{ name: "fireball", level: 1, primary: true }]),
  );
});

test("zero primaries throws (top-level damage would be undefined)", () => {
  const s = createShim();
  tkBare(s);
  assert.throws(() => s.setAttackSkills([{ name: "tornado_kick", level: 7 }]));
});

test("two primaries throws (ambiguous top-level damage)", () => {
  const s = createShim();
  tkBare(s);
  assert.throws(() =>
    s.setAttackSkills([
      { name: "tornado_kick", level: 7, primary: true },
      { name: "roundhouse", level: 7, primary: true },
    ]),
  );
});

test("supportedAttackSkills returns the binding keys", () => {
  const s = createShim();
  const names = s.supportedAttackSkills();
  assert.ok(
    names && names.includes("tornado_kick") && names.includes("roundhouse"),
  );
});

const INLINE = {
  hp: 5000,
  atk_min: 100,
  atk_max: 200,
  def: 10,
  mdef: 5,
  race: "RC_Brute" as const,
  element: "Ele_Neutral" as const,
  element_lv: 1,
  size: "Size_Medium" as const,
  level: 50,
};

test("breakdown is computed against an inline mob (before m_Monster restore)", () => {
  const s = createShim();
  tkBare(s);
  s.setAttackSkills([{ name: "roundhouse", level: 7, primary: true }]);
  s.setEnemyInline(INLINE);
  const c = s.readCombatResults();
  assert.equal(c.skills?.length, 1);
  assert.ok(c.skills?.[0].damage.ave !== null && c.skills[0].damage.ave > 0);
  assert.equal(c.damage.ave, c.skills?.[0].damage.ave); // primary drives top-level
});

test("reset clears a scored skill: next auto-attack request is unbuffed baseline", () => {
  const s = createShim();
  tkBare(s);
  s.setEnemy(PORING);
  const auto = s.readCombatResults().damage.ave;
  s.setAttackSkills([{ name: "roundhouse", level: 7, primary: true }]);
  s.setEnemy(PORING);
  assert.ok(s.readCombatResults().damage.ave! > auto!);
  s.reset();
  tkBare(s);
  s.setEnemy(PORING);
  assert.equal(s.readCombatResults().damage.ave, auto); // back to auto-attack
});
