import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Whitesmith with a Two-Handed Axe (iRO 1360). The axe is required: Adrenaline
// Rush is Axe/Mace-gated, and the axe's Medium size penalty (75%) is what makes
// Weapon Perfection observable. ATK/damage steroids move damage.ave; ASPD
// steroids move readDerivedStats().aspd; HIT moves readDerivedStats().hit
// (uncapped -- combat.hit caps at 100).
function wsShim() {
  const s = createShim();
  s.setClass("whitesmith");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 90, agi: 60, vit: 40, int: 1, dex: 60, luk: 1 });
  s.equip("weapon", { id: 1360 });
  return s;
}

const TARGET = {
  hp: 50000,
  atk_min: 100,
  atk_max: 200,
  def: 20,
  mdef: 10,
  race: "RC_DemiHuman" as const,
  element: "Ele_Neutral" as const,
  element_lv: 1 as const,
  size: "Size_Medium" as const,
  level: 80,
};

function damageWith(buffName: string, level: number): number {
  const s = wsShim();
  s.setBuffs([{ name: buffName, level }]);
  s.setEnemyInline(TARGET);
  const ave = s.readCombatResults().damage.ave;
  assert.ok(ave != null, `${buffName}: damage.ave must be numeric`);
  return ave as number;
}

function baseDamage(): number {
  const s = wsShim();
  s.setEnemyInline(TARGET);
  const ave = s.readCombatResults().damage.ave;
  assert.ok(ave != null, "base damage.ave must be numeric");
  return ave as number;
}

test("over_thrust raises auto-attack damage (%ATK)", () => {
  const plain = baseDamage();
  const got = damageWith("over_thrust", 5);
  assert.ok(
    got > plain,
    `over_thrust should raise damage (${plain} -> ${got})`,
  );
});

test("maximum_over_thrust raises auto-attack damage (%ATK)", () => {
  const plain = baseDamage();
  const got = damageWith("maximum_over_thrust", 5);
  assert.ok(
    got > plain,
    `maximum_over_thrust should raise damage (${plain} -> ${got})`,
  );
});

test("maximize_power raises auto-attack damage (min roll -> max roll)", () => {
  const plain = baseDamage();
  const got = damageWith("maximize_power", 5);
  assert.ok(
    got > plain,
    `maximize_power should raise damage (${plain} -> ${got})`,
  );
});

test("crazy_uproar raises auto-attack damage (+5 STR)", () => {
  const plain = baseDamage();
  const got = damageWith("crazy_uproar", 1);
  assert.ok(
    got > plain,
    `crazy_uproar should raise damage (${plain} -> ${got})`,
  );
});

test("weapon_perfection raises damage vs a Medium target (size 75% -> 100%)", () => {
  // Two-Handed Axe vs Medium = 75% weapon ATK; Weapon Perfection removes the
  // penalty. Driven via the support bank (A2_Skill7 checkbox), not the job bank.
  const plain = baseDamage();
  const got = damageWith("weapon_perfection", 5);
  assert.ok(
    got > plain,
    `weapon_perfection should raise damage vs Medium (${plain} -> ${got})`,
  );
});

test("hilt_binding raises auto-attack damage (+ATK)", () => {
  // Empirical fork: classified stat_buff expecting +ATK. If rocalc's Hilt
  // Binding moves no damage, downgrade overlay kind to `status` in Task 2,
  // relax this to a no-op smoke (got === plain), and note the finding.
  const plain = baseDamage();
  const got = damageWith("hilt_binding", 1);
  assert.ok(
    got > plain,
    `hilt_binding should raise damage (${plain} -> ${got})`,
  );
});

test("adrenaline_rush raises ASPD (Axe/Mace-gated, axe equipped)", () => {
  const base = wsShim().readDerivedStats().aspd;
  const s = wsShim();
  s.setBuffs([{ name: "adrenaline_rush", level: 5 }]);
  assert.ok(
    s.readDerivedStats().aspd > base,
    `adrenaline_rush should raise ASPD (${base} -> ${s.readDerivedStats().aspd})`,
  );
});

test("advanced_adrenaline_rush raises ASPD", () => {
  const base = wsShim().readDerivedStats().aspd;
  const s = wsShim();
  s.setBuffs([{ name: "advanced_adrenaline_rush", level: 1 }]);
  assert.ok(
    s.readDerivedStats().aspd > base,
    `advanced_adrenaline_rush should raise ASPD (${base} -> ${s.readDerivedStats().aspd})`,
  );
});

test("weaponry_research raises damage and HIT", () => {
  const plainDmg = baseDamage();
  const baseHit = wsShim().readDerivedStats().hit;
  const s = wsShim();
  s.setBuffs([{ name: "weaponry_research", level: 10 }]);
  s.setEnemyInline(TARGET);
  const got = s.readCombatResults().damage.ave;
  assert.ok(got != null, "weaponry_research: damage.ave must be numeric");
  assert.ok(
    (got as number) > plainDmg,
    `weaponry_research should raise damage (${plainDmg} -> ${got})`,
  );
  assert.ok(
    s.readDerivedStats().hit > baseHit,
    `weaponry_research should raise HIT (${baseHit} -> ${s.readDerivedStats().hit})`,
  );
});

test("weaponry_research scales with level (5 < 10)", () => {
  // +2 ATK & +2 HIT per level; damage.ave rises monotonically with the level.
  const lo = damageWith("weaponry_research", 5);
  const hi = damageWith("weaponry_research", 10);
  assert.ok(
    hi > lo,
    `weaponry_research level 10 damage should exceed level 5 (${lo} -> ${hi})`,
  );
});

test("skin_tempering applies cleanly and leaves auto-attack damage unchanged", () => {
  // Skin Tempering grants fire/neutral resistance -- a defensive stat the
  // auto-attack offense sim does not read, so driving it is a no-op here.
  // Contract: the rocalc id resolves and the build still scores, unchanged.
  const plain = baseDamage();
  const got = damageWith("skin_tempering", 5);
  assert.equal(
    got,
    plain,
    "skin_tempering should not change auto-attack damage",
  );
});

test("reset() clears the Whitesmith buff banks (no leak across requests)", () => {
  const s = wsShim();
  s.setEnemyInline(TARGET);
  const plain = s.readCombatResults().damage.ave;
  s.setBuffs([{ name: "over_thrust", level: 5 }]);
  s.readCombatResults();
  s.reset();
  s.setClass("whitesmith");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 90, agi: 60, vit: 40, int: 1, dex: 60, luk: 1 });
  s.equip("weapon", { id: 1360 });
  s.setEnemyInline(TARGET);
  const after = s.readCombatResults().damage.ave;
  assert.equal(after, plain, "reset must clear over_thrust (no leak)");
});
