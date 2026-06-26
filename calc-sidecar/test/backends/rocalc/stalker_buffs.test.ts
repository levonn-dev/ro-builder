import { test, before } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Stalker self-buffs ride the rocalc job-buff bank (m_JobBuff[28]); all three new
// skills use the skill_slot driver. Close Confine raises readDerivedStats().flee;
// Stealth (Chase Walk) raises readCombatResults().damage.ave via its STR bonus;
// Counter Instinct (Reject Sword) is a damage-reflect with no auto-attack offense
// effect (wired-but-inert status). Test weapon: dagger Main Gauche (iRO 1207). One
// inherited buff (Sword Mastery) is exercised as a regression lock proving the
// shared bank drives on Stalker.
const DAGGER = 1207;

// The shim (jsdom + class load) is the expensive part, so it is created once in
// before() and reused. reset() preserves the class but clears the buff banks and
// rolls level / stats / equipment back to baseline, so fresh() re-applies the
// Stalker build for a clean, leak-free baseline without re-paying createShim+setClass.
let shim: ReturnType<typeof createShim>;
before(() => {
  shim = createShim();
  shim.setClass("stalker");
});
function fresh() {
  shim.reset();
  shim.setLevel({ base: 99, job: 70 });
  shim.setStats({ str: 70, agi: 90, vit: 30, int: 1, dex: 70, luk: 20 });
  shim.equip("weapon", { id: DAGGER });
  return shim;
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

function fleeWith(buffs: { name: string; level: number }[]): number {
  const s = fresh();
  if (buffs.length) s.setBuffs(buffs);
  return s.readDerivedStats().flee;
}

function aveWith(buffs: { name: string; level: number }[]): number {
  const s = fresh();
  if (buffs.length) s.setBuffs(buffs);
  s.setEnemyInline(NEUTRAL);
  const ave = s.readCombatResults().damage.ave;
  assert.ok(ave != null, "damage.ave must be numeric");
  return ave as number;
}

// --- Close Confine: +flee (readDerivedStats().flee) ---
test("close_confine raises flee", () => {
  const base = fleeWith([]);
  const got = fleeWith([{ name: "close_confine", level: 1 }]);
  assert.ok(got > base, `close_confine should raise flee (${base} -> ${got})`);
});

// --- Stealth (Chase Walk): +ATK via STR (readCombatResults().damage.ave) ---
test("stealth raises damage.ave", () => {
  const base = aveWith([]);
  const got = aveWith([{ name: "stealth", level: 5 }]);
  assert.ok(got > base, `stealth should raise damage.ave (${base} -> ${got})`);
});

// --- Counter Instinct (Reject Sword): reflect, inert in the offense sim ---
test("counter_instinct leaves damage.ave unchanged (wired-but-inert)", () => {
  const base = aveWith([]);
  const got = aveWith([{ name: "counter_instinct", level: 5 }]);
  assert.equal(
    got,
    base,
    "counter_instinct is a reflect; no auto-attack offense effect",
  );
});

// --- Regression lock: an inherited buff (Sword Mastery) drives on Stalker ---
test("inherited sword_mastery raises damage on Stalker (shared bank drives)", () => {
  const base = aveWith([]);
  const got = aveWith([{ name: "sword_mastery", level: 10 }]);
  assert.ok(
    got > base,
    `sword_mastery should raise damage on Stalker (${base} -> ${got})`,
  );
});

// --- reset() isolation ---
test("reset() clears Stalker buff banks (no flee leak)", () => {
  const s = fresh();
  const plain = s.readDerivedStats().flee;
  s.setBuffs([{ name: "close_confine", level: 1 }]);
  s.readDerivedStats();
  s.reset();
  s.setClass("stalker");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 70, agi: 90, vit: 30, int: 1, dex: 70, luk: 20 });
  s.equip("weapon", { id: DAGGER });
  const after = s.readDerivedStats().flee;
  assert.equal(after, plain, "reset must clear close_confine (no flee leak)");
});
