import { test, before, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Assassin Cross with a Katar equipped (iRO 1252). EDP and the katar masteries
// multiply WEAPON ATK, so unlike the Scholar caster endows these tests need a
// real weapon, not a bare fist.
//
// The shim (jsdom + class load) is the expensive part, so it is created once in
// before() and reused. reset() preserves the class but clears the buff / debuff
// / land / music banks, so reset()+reconfigure in beforeEach gives every test a
// clean, leak-free baseline without re-paying the ~4s createShim+setClass.
let shim: ReturnType<typeof createShim>;

// reset() rolls level / stats / equipment back to the class baseline, so the
// SinX build is re-applied after each reset (the class itself survives reset()).
function reconfig(): void {
  shim.setLevel({ base: 99, job: 70 });
  shim.setStats({ str: 90, agi: 90, vit: 40, int: 1, dex: 70, luk: 40 });
  shim.equip("weapon", { id: 1252 });
}

before(() => {
  shim = createShim();
  shim.setClass("assassin_cross");
});

beforeEach(() => {
  shim.reset();
  reconfig();
});

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
  shim.setEnemyInline(TARGET);
  const plain = shim.readCombatResults().damage.ave;
  shim.setBuffs([{ name: "enchant_deadly_poison", level: 5 }]);
  shim.setEnemyInline(TARGET);
  const edp = shim.readCombatResults().damage.ave;
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
  shim.setEnemyInline(tgt);
  const plain = shim.readCombatResults().damage.ave;
  shim.setBuffs([{ name: "enchant_poison", level: 5, element: "poison" }]);
  shim.setEnemyInline(tgt);
  const poison = shim.readCombatResults().damage.ave;
  assert.ok(plain != null && poison != null, "damage.ave must be numeric");
  assert.notEqual(
    poison,
    plain,
    "enchant_poison should change damage vs a poison target",
  );
});

test("katar_mastery raises katar damage", () => {
  shim.setEnemyInline(TARGET);
  const plain = shim.readCombatResults().damage.ave;
  shim.setBuffs([{ name: "katar_mastery", level: 10 }]);
  shim.setEnemyInline(TARGET);
  const got = shim.readCombatResults().damage.ave;
  assert.ok(plain != null && got != null, "damage.ave must be numeric");
  assert.ok(
    (got as number) > (plain as number),
    `katar mastery should raise damage (${plain} -> ${got})`,
  );
});

test("advanced_katar_mastery raises katar damage", () => {
  shim.setEnemyInline(TARGET);
  const plain = shim.readCombatResults().damage.ave;
  shim.setBuffs([{ name: "advanced_katar_mastery", level: 5 }]);
  shim.setEnemyInline(TARGET);
  const got = shim.readCombatResults().damage.ave;
  assert.ok(plain != null && got != null, "damage.ave must be numeric");
  assert.ok(
    (got as number) > (plain as number),
    `advanced katar mastery should raise damage (${plain} -> ${got})`,
  );
});

test("improve_dodge raises flee", () => {
  const base = shim.readDerivedStats().flee;
  shim.setBuffs([{ name: "improve_dodge", level: 10 }]);
  assert.ok(
    shim.readDerivedStats().flee > base,
    "improve dodge should raise flee",
  );
});

test("double_attack / hand masteries / sonic_acceleration apply cleanly", () => {
  // With a katar these are no-ops in the auto-attack sim (Double Attack is
  // dagger-only, the hand masteries need dual-wield, Sonic Acceleration only
  // affects Sonic Blow). The contract: the rocalc id resolves and the build
  // still scores. reset() between buffs isolates each one (no accumulation).
  for (const name of [
    "double_attack",
    "right_hand_mastery",
    "left_hand_mastery",
    "sonic_acceleration",
  ]) {
    shim.reset();
    reconfig();
    shim.setBuffs([{ name, level: 1 }]);
    shim.setEnemyInline(TARGET);
    assert.ok(
      shim.readCombatResults().damage.ave != null,
      `${name} should not break scoring`,
    );
  }
});

test("reset() clears the SinX buff bank (no leak across requests)", () => {
  shim.setEnemyInline(TARGET);
  const plain = shim.readCombatResults().damage.ave;
  shim.setBuffs([{ name: "enchant_deadly_poison", level: 5 }]);
  shim.readCombatResults();
  shim.reset();
  shim.setClass("assassin_cross");
  reconfig();
  shim.setEnemyInline(TARGET);
  const after = shim.readCombatResults().damage.ave;
  assert.equal(after, plain, "reset must clear EDP (no leak)");
});
