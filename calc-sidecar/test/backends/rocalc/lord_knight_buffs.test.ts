import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Lord Knight self-buffs all live in the rocalc job-buff bank (m_JobBuff[21]),
// so each rides the skill_slot driver. Three weapons exercise the weapon-gated
// steroids: a Two-Handed Sword (iRO 1163 Claymore) for the 2H line, a One-Handed
// Sword (iRO 1126 Saber) for Sword Mastery / Onehand Quicken, and a Spear (iRO
// 1407 Pike) for Spear Mastery. ATK/damage steroids move
// readCombatResults().damage.ave; ASPD steroids move readDerivedStats().aspd;
// HIT moves readDerivedStats().hit (uncapped -- combat.hit caps at 100).
function lkShim(weaponId: number) {
  const s = createShim();
  s.setClass("lord_knight");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 99, agi: 60, vit: 50, int: 1, dex: 60, luk: 1 });
  s.equip("weapon", { id: weaponId });
  return s;
}

const TWO_HAND_SWORD = 1163; // Claymore
const ONE_HAND_SWORD = 1126; // Saber
const SPEAR = 1407; // Pike

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

function damageWith(weaponId: number, buffName: string, level: number): number {
  const s = lkShim(weaponId);
  s.setBuffs([{ name: buffName, level }]);
  s.setEnemyInline(TARGET);
  const ave = s.readCombatResults().damage.ave;
  assert.ok(ave != null, `${buffName}: damage.ave must be numeric`);
  return ave as number;
}

function baseDamage(weaponId: number): number {
  const s = lkShim(weaponId);
  s.setEnemyInline(TARGET);
  const ave = s.readCombatResults().damage.ave;
  assert.ok(ave != null, "base damage.ave must be numeric");
  return ave as number;
}

// --- ATK / damage steroids (2H sword) ---
test("two_handed_sword_mastery raises 2H-sword damage (+ATK)", () => {
  const plain = baseDamage(TWO_HAND_SWORD);
  const got = damageWith(TWO_HAND_SWORD, "two_handed_sword_mastery", 10);
  assert.ok(
    got > plain,
    `two_handed_sword_mastery should raise damage (${plain} -> ${got})`,
  );
});

test("aura_blade raises 2H-sword damage (fixed added damage)", () => {
  const plain = baseDamage(TWO_HAND_SWORD);
  const got = damageWith(TWO_HAND_SWORD, "aura_blade", 5);
  assert.ok(got > plain, `aura_blade should raise damage (${plain} -> ${got})`);
});

test("concentration raises 2H-sword damage (+ATK)", () => {
  const plain = baseDamage(TWO_HAND_SWORD);
  const got = damageWith(TWO_HAND_SWORD, "concentration", 5);
  assert.ok(
    got > plain,
    `concentration should raise damage (${plain} -> ${got})`,
  );
});

test("berserk raises 2H-sword damage (+ATK)", () => {
  const plain = baseDamage(TWO_HAND_SWORD);
  const got = damageWith(TWO_HAND_SWORD, "berserk", 1);
  assert.ok(got > plain, `berserk should raise damage (${plain} -> ${got})`);
});

// --- ASPD steroids (readDerivedStats().aspd) ---
test("twohand_quicken raises ASPD (2H sword)", () => {
  const s = lkShim(TWO_HAND_SWORD);
  const base = s.readDerivedStats().aspd;
  s.setBuffs([{ name: "twohand_quicken", level: 10 }]);
  const got = s.readDerivedStats().aspd;
  assert.ok(
    got > base,
    `twohand_quicken should raise ASPD (${base} -> ${got})`,
  );
});

test("berserk raises ASPD", () => {
  const s = lkShim(TWO_HAND_SWORD);
  const base = s.readDerivedStats().aspd;
  s.setBuffs([{ name: "berserk", level: 1 }]);
  const got = s.readDerivedStats().aspd;
  assert.ok(got > base, `berserk should raise ASPD (${base} -> ${got})`);
});

test("onehand_quicken raises ASPD (1H sword)", () => {
  const s = lkShim(ONE_HAND_SWORD);
  const base = s.readDerivedStats().aspd;
  s.setBuffs([{ name: "onehand_quicken", level: 1 }]);
  const got = s.readDerivedStats().aspd;
  assert.ok(
    got > base,
    `onehand_quicken should raise ASPD (${base} -> ${got})`,
  );
});

// --- HIT steroid ---
test("concentration raises HIT", () => {
  const s = lkShim(TWO_HAND_SWORD);
  const base = s.readDerivedStats().hit;
  s.setBuffs([{ name: "concentration", level: 5 }]);
  const got = s.readDerivedStats().hit;
  assert.ok(got > base, `concentration should raise HIT (${base} -> ${got})`);
});

// --- weapon-gated masteries ---
test("sword_mastery raises 1H-sword damage (+ATK)", () => {
  const plain = baseDamage(ONE_HAND_SWORD);
  const got = damageWith(ONE_HAND_SWORD, "sword_mastery", 10);
  assert.ok(
    got > plain,
    `sword_mastery should raise damage (${plain} -> ${got})`,
  );
});

test("spear_mastery raises spear damage (+ATK)", () => {
  const plain = baseDamage(SPEAR);
  const got = damageWith(SPEAR, "spear_mastery", 10);
  assert.ok(
    got > plain,
    `spear_mastery should raise damage (${plain} -> ${got})`,
  );
});

// --- scaling ---
test("two_handed_sword_mastery scales with level (5 < 10)", () => {
  const lo = damageWith(TWO_HAND_SWORD, "two_handed_sword_mastery", 5);
  const hi = damageWith(TWO_HAND_SWORD, "two_handed_sword_mastery", 10);
  assert.ok(
    hi > lo,
    `two_handed_sword_mastery level 10 should exceed level 5 (${lo} -> ${hi})`,
  );
});

// --- auto_berserk: empirical fork — raises damage (Provoke ATK/DEF effect) ---
// rocalc's Auto Berserk applies a Provoke-like ATK boost + enemy DEF reduction
// through the combat sim, so it is stat_buff not a pure status no-op.
test("auto_berserk raises 2H-sword damage (Provoke ATK/DEF effect)", () => {
  const plain = baseDamage(TWO_HAND_SWORD);
  const got = damageWith(TWO_HAND_SWORD, "auto_berserk", 1);
  assert.ok(
    got > plain,
    `auto_berserk should raise damage (${plain} -> ${got})`,
  );
});

// --- status no-ops (offense sim does not read these) ---
for (const buff of [
  "endure",
  "increase_hp_recovery",
  "cavalier_mastery",
  "parrying",
]) {
  test(`${buff} applies cleanly and leaves 2H-sword damage unchanged`, () => {
    const plain = baseDamage(TWO_HAND_SWORD);
    const got = damageWith(TWO_HAND_SWORD, buff, 1);
    assert.equal(got, plain, `${buff} should not change auto-attack damage`);
  });
}

// --- reset() isolation ---
test("reset() clears the Lord Knight buff banks (no leak across requests)", () => {
  const s = lkShim(TWO_HAND_SWORD);
  s.setEnemyInline(TARGET);
  const plain = s.readCombatResults().damage.ave;
  s.setBuffs([{ name: "concentration", level: 5 }]);
  s.readCombatResults();
  s.reset();
  s.setClass("lord_knight");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 99, agi: 60, vit: 50, int: 1, dex: 60, luk: 1 });
  s.equip("weapon", { id: TWO_HAND_SWORD });
  s.setEnemyInline(TARGET);
  const after = s.readCombatResults().damage.ave;
  assert.equal(after, plain, "reset must clear concentration (no leak)");
});
