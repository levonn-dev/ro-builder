import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Assassin Cross with a Katar equipped (iRO 1252). EDP and the katar masteries
// multiply WEAPON ATK, so unlike the Scholar caster endows these tests need a
// real weapon, not a bare fist.
function sinxShim() {
  const s = createShim();
  s.setClass("assassin_cross");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 90, agi: 90, vit: 40, int: 1, dex: 70, luk: 40 });
  s.equip("weapon", { id: 1252 });
  return s;
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

test("enchant_deadly_poison raises auto-attack damage", () => {
  const base = sinxShim();
  base.setEnemyInline(TARGET);
  const plain = base.readCombatResults().damage.ave;
  const s = sinxShim();
  s.setBuffs([{ name: "enchant_deadly_poison", level: 5 }]);
  s.setEnemyInline(TARGET);
  const edp = s.readCombatResults().damage.ave;
  assert.ok(plain != null && edp != null, "damage.ave must be numeric");
  assert.ok(
    (edp as number) > (plain as number),
    `EDP should raise damage (${plain} -> ${edp})`,
  );
});

test("enchant_poison endows poison (changes damage vs an Ele_Poison target)", () => {
  // Plain neutral katar hits a poison target ~100%; a poison-endowed weapon
  // hits it for far less (poison vs poison), so the damage must change.
  const tgt = { ...TARGET, element: "Ele_Poison" as typeof TARGET.element };
  const base = sinxShim();
  base.setEnemyInline(tgt);
  const plain = base.readCombatResults().damage.ave;
  const s = sinxShim();
  s.setBuffs([{ name: "enchant_poison", level: 5, element: "poison" }]);
  s.setEnemyInline(tgt);
  const poison = s.readCombatResults().damage.ave;
  assert.ok(plain != null && poison != null, "damage.ave must be numeric");
  assert.notEqual(
    poison,
    plain,
    "enchant_poison should change damage vs a poison target",
  );
});

test("katar_mastery raises katar damage", () => {
  const base = sinxShim();
  base.setEnemyInline(TARGET);
  const plain = base.readCombatResults().damage.ave;
  const s = sinxShim();
  s.setBuffs([{ name: "katar_mastery", level: 10 }]);
  s.setEnemyInline(TARGET);
  const got = s.readCombatResults().damage.ave;
  assert.ok(plain != null && got != null, "damage.ave must be numeric");
  assert.ok(
    (got as number) > (plain as number),
    `katar mastery should raise damage (${plain} -> ${got})`,
  );
});

test("advanced_katar_mastery raises katar damage", () => {
  const base = sinxShim();
  base.setEnemyInline(TARGET);
  const plain = base.readCombatResults().damage.ave;
  const s = sinxShim();
  s.setBuffs([{ name: "advanced_katar_mastery", level: 5 }]);
  s.setEnemyInline(TARGET);
  const got = s.readCombatResults().damage.ave;
  assert.ok(plain != null && got != null, "damage.ave must be numeric");
  assert.ok(
    (got as number) > (plain as number),
    `advanced katar mastery should raise damage (${plain} -> ${got})`,
  );
});

test("improve_dodge raises flee", () => {
  const base = sinxShim().readDerivedStats().flee;
  const s = sinxShim();
  s.setBuffs([{ name: "improve_dodge", level: 10 }]);
  assert.ok(
    s.readDerivedStats().flee > base,
    "improve dodge should raise flee",
  );
});

test("double_attack / hand masteries / sonic_acceleration apply cleanly", () => {
  // With a katar these are no-ops in the auto-attack sim (Double Attack is
  // dagger-only, the hand masteries need dual-wield, Sonic Acceleration only
  // affects Sonic Blow). The contract: the rocalc id resolves and the build
  // still scores.
  for (const name of [
    "double_attack",
    "right_hand_mastery",
    "left_hand_mastery",
    "sonic_acceleration",
  ]) {
    const s = sinxShim();
    s.setBuffs([{ name, level: 1 }]);
    s.setEnemyInline(TARGET);
    assert.ok(
      s.readCombatResults().damage.ave != null,
      `${name} should not break scoring`,
    );
  }
});

test("reset() clears the SinX buff bank (no leak across requests)", () => {
  const s = sinxShim();
  s.setEnemyInline(TARGET);
  const plain = s.readCombatResults().damage.ave;
  s.setBuffs([{ name: "enchant_deadly_poison", level: 5 }]);
  s.readCombatResults();
  s.reset();
  s.setClass("assassin_cross");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 90, agi: 90, vit: 40, int: 1, dex: 70, luk: 40 });
  s.equip("weapon", { id: 1252 });
  s.setEnemyInline(TARGET);
  const after = s.readCombatResults().damage.ave;
  assert.equal(after, plain, "reset must clear EDP (no leak)");
});
