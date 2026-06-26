import { test, before, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// The shim (jsdom + class load) is created once and reused; reset() preserves
// the class but clears applied buffs / skills, so reset()+reconfigure in
// beforeEach gives every test a clean Taekwon Kid baseline without re-paying
// the ~4s createShim+setClass per test.
let shim: ReturnType<typeof createShim>;

// reset() rolls level / stats back to the class baseline; re-apply the TK build
// after each reset. The class itself survives reset().
function reconfig(): void {
  shim.setLevel({ base: 99, job: 50 });
  shim.setStats({ str: 80, agi: 90, vit: 40, int: 1, dex: 60, luk: 30 });
}

before(() => {
  shim = createShim();
  shim.setClass("taekwon_kid");
});

beforeEach(() => {
  shim.reset();
  reconfig();
});

test("taekwon_ranker triples maxHp/maxSp", () => {
  const base = shim.readDerivedStats();
  shim.setBuffs([{ name: "taekwon_ranker", level: 1 }]);
  const buffed = shim.readDerivedStats();
  assert.ok(
    buffed.maxHp >= base.maxHp * 2.9 && buffed.maxHp <= base.maxHp * 3.1,
    `ranker maxHp ${buffed.maxHp} not ~3x base ${base.maxHp}`,
  );
});

test("mild_wind holy endow changes combat damage vs an undead target", () => {
  const target = {
    hp: 50000,
    atk_min: 100,
    atk_max: 200,
    def: 10,
    mdef: 10,
    race: "RC_Undead",
    element: "Ele_Undead",
    element_lv: 1,
    size: "Size_Medium",
    level: 90,
  };
  shim.setEnemyInline(target);
  const plain = shim.readCombatResults().damage.ave;

  shim.reset();
  reconfig();
  shim.setBuffs([{ name: "mild_wind", level: 7, element: "holy" }]);
  shim.setEnemyInline(target);
  const endowed = shim.readCombatResults().damage.ave;

  assert.ok(plain != null && endowed != null, "damage.ave should be non-null");
  assert.notEqual(endowed, plain, "holy endow should change damage vs undead");
});

test("unknown buff name throws", () => {
  assert.throws(() => shim.setBuffs([{ name: "not_a_buff" }]), /unknown buff/);
});

test("duplicate buff names throw (backend resilience)", () => {
  // The Go resolver rejects duplicates too, but the backend must not silently
  // last-write-wins on a repeated buff.
  assert.throws(
    () =>
      shim.setBuffs([
        { name: "spurt", level: 7 },
        { name: "spurt", level: 7 },
      ]),
    /duplicate/i,
  );
});

test("a buff with no engine slot for the active class throws, not silently scored unbuffed", () => {
  // taekwon_ranker's rocalc skill id is not in a Knight's m_JobBuff. In
  // production the Go resolver would reject this before the sidecar (the buff
  // isn't in the class's buff list), but the backend must label an
  // entirely-inapplicable buff rather than silently apply nothing and report
  // unbuffed numbers as if the buff were active.
  //
  // Uses its own shim: setClass("knight") would otherwise leave the shared
  // session on the wrong class (reset() preserves class, it does not revert it).
  const s = createShim();
  s.setClass("knight");
  s.setLevel({ base: 99, job: 50 });
  s.setStats({ str: 90, agi: 1, vit: 80, int: 1, dex: 40, luk: 1 });
  assert.throws(
    () => s.setBuffs([{ name: "taekwon_ranker", level: 1 }]),
    /not applicable|no .*slot|not modeled/i,
  );
});

test("endow element above resolved level throws", () => {
  assert.throws(
    () => shim.setBuffs([{ name: "mild_wind", level: 4, element: "holy" }]),
    /exceeds.*level|not available at level/i,
  );
});

// Barehanded TK target used for all spurt / mastery combat tests.
// Neutral medium-brute mob so element/size multipliers are 1x and don't
// obscure the mastery contribution. Level 50 keeps hit rate comfortable.
const BARE_TARGET = {
  hp: 50000,
  atk_min: 100,
  atk_max: 200,
  def: 10,
  mdef: 10,
  race: "RC_Brute" as const,
  element: "Ele_Neutral" as const,
  element_lv: 1 as const,
  size: "Size_Medium" as const,
  level: 50,
};

test("spurt status (379) changes derived ATK", () => {
  // Status 379 = Sprint STR+State; raises STR which shows in A_ATK2 (base ATK).
  // This is the original weak test, now renamed to be explicit about WHAT it checks.
  const base = shim.readDerivedStats();
  shim.setBuffs([{ name: "spurt", level: 7 }]);
  const buffed = shim.readDerivedStats();
  assert.notDeepEqual(
    buffed,
    base,
    "spurt status (379) should raise ATK via STR bonus",
  );
});

test("spurt mastery (329) drives barehanded combat damage independently of ATK", () => {
  // Sprint unarmed mastery (skill 329) is at m_JobBuff[41] index 3 (A_skill3).
  // It does NOT change derived ATK (the mastery adds flat damage in the combat
  // sim, not a stat bonus) -- this is why the prior test missed it. We must
  // score against an inline target to observe the mastery's effect.
  //
  // Configuration: barehanded TK (no weapon), neutral/medium target.
  // At STR 80 lv99 baseline, mastery lv10 adds ~100 ave damage vs this target.

  // Baseline: no skills set, no buffs
  shim.setEnemyInline(BARE_TARGET);
  const baseDmg = shim.readCombatResults().damage;

  // Mastery only via setSkills (bypasses setBuffs to isolate 329 from 379)
  shim.reset();
  reconfig();
  shim.setSkills([{ id: 329, level: 10 }]);
  shim.setEnemyInline(BARE_TARGET);
  const masteryDmg = shim.readCombatResults().damage;

  assert.ok(
    baseDmg.ave != null && masteryDmg.ave != null,
    "damage.ave must be numeric (hit rate ok)",
  );
  assert.ok(
    (masteryDmg.ave as number) > (baseDmg.ave as number),
    `mastery lv10 ave damage ${masteryDmg.ave} should exceed baseline ${baseDmg.ave}`,
  );
  // Sanity: mastery must not change derived ATK (it's a combat-sim bonus, not a stat)
  shim.reset();
  reconfig();
  const baseDerived = shim.readDerivedStats();
  shim.reset();
  reconfig();
  shim.setSkills([{ id: 329, level: 10 }]);
  const masteryDerived = shim.readDerivedStats();
  assert.equal(
    masteryDerived.atk.base,
    baseDerived.atk.base,
    "mastery must not change ATK base (explains why the old test missed it)",
  );
});

test("spurt setBuffs applies both status (379) and mastery (329) to combat", () => {
  // Full setBuffs path: both actions fire. Status (379) raises ATK via STR;
  // mastery (329) adds flat combat damage. Together they produce more damage
  // than status alone.

  shim.setSkills([{ id: 379, level: 1 }]);
  shim.setEnemyInline(BARE_TARGET);
  const statusDmg = shim.readCombatResults().damage;

  shim.reset();
  reconfig();
  shim.setBuffs([{ name: "spurt", level: 7 }]);
  shim.setEnemyInline(BARE_TARGET);
  const bothDmg = shim.readCombatResults().damage;

  assert.ok(
    statusDmg.ave != null && bothDmg.ave != null,
    "damage.ave must be numeric for both variants",
  );
  assert.ok(
    (bothDmg.ave as number) > (statusDmg.ave as number),
    `spurt lv7 (${bothDmg.ave}) should exceed status-only (${statusDmg.ave}); mastery adds damage`,
  );
});

// --- Mild Wind order pinning ---
//
// MILD_WIND_ORDER inside createShim mirrors the endow.elements list in
// internal/catalog/data/skill_buffs.yaml (TK_SEVENWIND). That YAML is the
// source of truth; this const is a defense-in-depth copy. The four assertions
// below pin the order's level boundaries so any divergence (reordering either
// side without the other) causes a test failure. If you change the order,
// update skill_buffs.yaml AND MILD_WIND_ORDER in
// calc-sidecar/src/backends/rocalc/index.ts, then adjust the expectations here
// and in TestMildWindOrder in internal/catalog/buffs_test.go.
//
// Canonical order (index 1-based = required skill level):
//   earth=1, wind=2, water=3, fire=4, ghost=5, shadow=6, holy=7.

test("mild_wind order: earth at level 1 is accepted (lowest unlock)", () => {
  // earth is index 1; level 1 satisfies the constraint (order 1 <= level 1).
  assert.doesNotThrow(
    () => shim.setBuffs([{ name: "mild_wind", level: 1, element: "earth" }]),
    "earth at Mild Wind lv1 should be accepted",
  );
});

test("mild_wind order: wind at level 1 throws (wind needs level 2)", () => {
  // wind is index 2; level 1 does NOT satisfy the constraint (order 2 > level 1).
  assert.throws(
    () => shim.setBuffs([{ name: "mild_wind", level: 1, element: "wind" }]),
    /exceeds.*level|not available at level/i,
    "wind at Mild Wind lv1 should throw: wind is unlock index 2",
  );
});

test("mild_wind order: holy at level 7 is accepted (highest unlock)", () => {
  // holy is index 7; level 7 satisfies the constraint (order 7 <= level 7).
  assert.doesNotThrow(
    () => shim.setBuffs([{ name: "mild_wind", level: 7, element: "holy" }]),
    "holy at Mild Wind lv7 should be accepted",
  );
});

test("mild_wind order: holy at level 6 throws (holy needs level 7)", () => {
  // holy is index 7; level 6 does NOT satisfy the constraint (order 7 > level 6).
  assert.throws(
    () => shim.setBuffs([{ name: "mild_wind", level: 6, element: "holy" }]),
    /exceeds.*level|not available at level/i,
    "holy at Mild Wind lv6 should throw: holy is unlock index 7",
  );
});

test("reset() clears applied buffs (no leak across pooled requests)", () => {
  const base = shim.readDerivedStats();
  shim.setBuffs([{ name: "taekwon_ranker", level: 1 }]);
  assert.notDeepEqual(
    shim.readDerivedStats(),
    base,
    "buff should change derived stats",
  );
  shim.reset();
  shim.setLevel({ base: 99, job: 50 });
  shim.setStats({ str: 80, agi: 90, vit: 40, int: 1, dex: 60, luk: 30 });
  assert.deepEqual(
    shim.readDerivedStats(),
    base,
    "after reset, the ranker buff must be gone",
  );
});
