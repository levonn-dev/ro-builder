import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Paladin self-buffs ride the rocalc job-buff bank (m_JobBuff[27], identical to
// Crusader's [13]); both new skills use the skill_slot driver. Faith raises
// readDerivedStats().maxHp; Spear Quicken raises readDerivedStats().aspd but ONLY
// with a two-handed spear (W_2HSPEAR) -- correct rocalc behavior. The test pins a
// Lance (iRO 1410, 2H spear) for Spear Quicken and a Pike (iRO 1407, 1H spear) for
// the weapon-gate control. One inherited buff (Spear Mastery) is exercised as a
// regression lock proving the shared bank drives on Paladin.
const LANCE = 1410; // 2H spear (W_2HSPEAR)
const PIKE = 1407; // 1H spear (W_1HSPEAR)

function palShim(weaponId: number) {
  const s = createShim();
  s.setClass("paladin");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 90, agi: 70, vit: 40, int: 1, dex: 60, luk: 20 });
  s.equip("weapon", { id: weaponId });
  return s;
}

const NEUTRAL = {
  hp: 50000,
  atk_min: 100,
  atk_max: 200,
  def: 20,
  mdef: 10,
  race: "RC_DemiHuman",
  element: "Ele_Neutral",
  element_lv: 1,
  size: "Size_Medium",
  level: 80,
} as const;

function maxHpWith(buffs: { name: string; level: number }[]): number {
  const s = palShim(LANCE);
  if (buffs.length) s.setBuffs(buffs);
  return s.readDerivedStats().maxHp;
}

function aspdWith(
  weaponId: number,
  buffs: { name: string; level: number }[],
): number {
  const s = palShim(weaponId);
  if (buffs.length) s.setBuffs(buffs);
  return s.readDerivedStats().aspd;
}

// --- Faith: +MaxHP (readDerivedStats().maxHp), weapon-agnostic ---
test("faith raises maxHp", () => {
  const base = maxHpWith([]);
  const got = maxHpWith([{ name: "faith", level: 10 }]);
  assert.ok(got > base, `faith should raise maxHp (${base} -> ${got})`);
});

test("faith scales with level (5 < 10)", () => {
  const lo = maxHpWith([{ name: "faith", level: 5 }]);
  const hi = maxHpWith([{ name: "faith", level: 10 }]);
  assert.ok(
    hi > lo,
    `faith level 10 maxHp should exceed level 5 (${lo} -> ${hi})`,
  );
});

// --- Spear Quicken: +ASPD (readDerivedStats().aspd), 2H spear only ---
test("spear_quicken raises aspd on a 2H spear (Lance)", () => {
  const base = aspdWith(LANCE, []);
  const got = aspdWith(LANCE, [{ name: "spear_quicken", level: 10 }]);
  assert.ok(
    got > base,
    `spear_quicken should raise aspd on a 2H spear (${base} -> ${got})`,
  );
});

test("spear_quicken does NOT change aspd on a 1H spear (Pike)", () => {
  const base = aspdWith(PIKE, []);
  const got = aspdWith(PIKE, [{ name: "spear_quicken", level: 10 }]);
  assert.equal(
    got,
    base,
    "spear_quicken applies ASPD to two-handed spears only",
  );
});

// --- Regression lock: an inherited buff (Spear Mastery) drives on Paladin ---
test("inherited spear_mastery raises damage on Paladin (shared bank drives)", () => {
  const b = palShim(LANCE);
  b.setEnemyInline(NEUTRAL);
  const base = b.readCombatResults().damage.ave;
  const s = palShim(LANCE);
  s.setBuffs([{ name: "spear_mastery", level: 10 }]);
  s.setEnemyInline(NEUTRAL);
  const got = s.readCombatResults().damage.ave;
  assert.ok(base != null && got != null, "damage.ave must be numeric");
  assert.ok(
    (got as number) > (base as number),
    `spear_mastery should raise damage on Paladin (${base} -> ${got})`,
  );
});

// --- reset() isolation ---
test("reset() clears Paladin buff banks (no maxHp leak)", () => {
  const s = palShim(LANCE);
  const plain = s.readDerivedStats().maxHp;
  s.setBuffs([{ name: "faith", level: 10 }]);
  s.readDerivedStats();
  s.reset();
  s.setClass("paladin");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 90, agi: 70, vit: 40, int: 1, dex: 60, luk: 20 });
  s.equip("weapon", { id: LANCE });
  const after = s.readDerivedStats().maxHp;
  assert.equal(after, plain, "reset must clear faith (no maxHp leak)");
});
