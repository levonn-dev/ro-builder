import { test, before } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Alchemist/Creator self-buffs ride the rocalc job-buff bank (m_JobBuff[19] ==
// m_JobBuff[33] == [537,59,68,241]); axe_mastery uses the skill_slot driver. It
// is AXE-GATED: it raises damage.ave only with an axe, and is inert with any
// non-axe weapon (the load-bearing negatives below lock that). The bonus is
// combat-sim mastery ATK, so reset() is verified via damage.ave (not atk.base,
// which the buff never moves). Buffs apply before the enemy (production order).
//   axe_mastery (2H axe) -> readCombatResults().damage.ave   (+30 at lv10)
//   axe_mastery (dagger / mace) -> inert

const AXE_2H = 1360; // Two-Handed Axe (W_2HAXE)
const DAGGER = 1207; // Main Gauche (W_DAGGER) -- non-axe negative
const MACE = 1501; // Club (W_MACE) -- non-axe negative

// The shim (jsdom + class load) is the expensive part, so it is created once in
// before() and reused for the Alchemist tests. reset() preserves the class but
// clears the buff banks and rolls level / stats / equipment back to baseline, so
// fresh(weaponId) re-applies the build for a clean, leak-free baseline without
// re-paying createShim+setClass. The Creator case keeps its own session because
// reset() does not roll back rocalc's class globals (that needs the expensive
// class change).
let shim: ReturnType<typeof createShim>;
before(() => {
  shim = createShim();
  shim.setClass("alchemist");
});
function fresh(weaponId: number) {
  shim.reset();
  shim.setLevel({ base: 99, job: 70 });
  shim.setStats({ str: 90, agi: 40, vit: 40, int: 40, dex: 90, luk: 20 });
  shim.equip("weapon", { id: weaponId });
  return shim;
}

// Creator (and any non-Alchemist) needs its own session: reset() preserves the
// class, so the shared Alchemist shim cannot stand in for it.
function otherClassShim(weaponId: number, className: string) {
  const s = createShim();
  s.setClass(className);
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 90, agi: 40, vit: 40, int: 40, dex: 90, luk: 20 });
  s.equip("weapon", { id: weaponId });
  return s;
}

const ENEMY = {
  hp: 50000,
  atk_min: 1500,
  atk_max: 2000,
  def: 20,
  mdef: 10,
  race: "RC_Brute",
  element: "Ele_Neutral",
  element_lv: 1,
  size: "Size_Medium",
  level: 90,
};

type Buff = { name: string; level: number };

function aveWith(
  weaponId: number,
  buffs: Buff[],
  className = "alchemist",
): number {
  const s =
    className === "alchemist"
      ? fresh(weaponId)
      : otherClassShim(weaponId, className);
  if (buffs.length) s.setBuffs(buffs);
  s.setEnemyInline(ENEMY);
  const ave = s.readCombatResults().damage.ave;
  assert.ok(ave != null, "damage.ave must be numeric");
  return ave as number;
}

test("axe_mastery raises damage.ave at lv10 (2H axe)", () => {
  const base = aveWith(AXE_2H, []);
  const got = aveWith(AXE_2H, [{ name: "axe_mastery", level: 10 }]);
  assert.ok(
    got > base,
    `axe_mastery lv10 should raise damage.ave on an axe (${base} -> ${got})`,
  );
});

test("axe_mastery scales with level (lv1 < lv10)", () => {
  const lo = aveWith(AXE_2H, [{ name: "axe_mastery", level: 1 }]);
  const hi = aveWith(AXE_2H, [{ name: "axe_mastery", level: 10 }]);
  assert.ok(hi > lo, `axe_mastery lv10 should exceed lv1 (${lo} -> ${hi})`);
});

test("axe_mastery is inert on a dagger (axe-gated)", () => {
  const base = aveWith(DAGGER, []);
  const got = aveWith(DAGGER, [{ name: "axe_mastery", level: 10 }]);
  assert.equal(
    got,
    base,
    "axe_mastery must not move damage.ave on a non-axe weapon (dagger)",
  );
});

test("axe_mastery is inert on a mace (axe-gated)", () => {
  const base = aveWith(MACE, []);
  const got = aveWith(MACE, [{ name: "axe_mastery", level: 10 }]);
  assert.equal(
    got,
    base,
    "axe_mastery must not move damage.ave on a non-axe weapon (mace)",
  );
});

test("axe_mastery raises damage.ave for Creator too (identical bank)", () => {
  const base = aveWith(AXE_2H, [], "creator");
  const got = aveWith(AXE_2H, [{ name: "axe_mastery", level: 10 }], "creator");
  assert.ok(
    got > base,
    `axe_mastery lv10 should raise Creator damage.ave (${base} -> ${got})`,
  );
});

test("reset() clears axe_mastery (no damage.ave leak)", () => {
  const plain = aveWith(AXE_2H, []);
  const s = fresh(AXE_2H);
  s.setBuffs([{ name: "axe_mastery", level: 10 }]);
  s.setEnemyInline(ENEMY);
  s.readCombatResults();
  s.reset();
  s.setClass("alchemist");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 90, agi: 40, vit: 40, int: 40, dex: 90, luk: 20 });
  s.equip("weapon", { id: AXE_2H });
  s.setEnemyInline(ENEMY);
  const after = s.readCombatResults().damage.ave;
  assert.equal(
    after,
    plain,
    "reset must clear axe_mastery (no damage.ave leak)",
  );
});
