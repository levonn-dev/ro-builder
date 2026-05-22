import { before, beforeEach, describe, test } from "node:test";
import assert from "node:assert/strict";
import type { ShimSession } from "../src/shim.ts";
import { createShim } from "../src/shim.ts";

// IDs in equip() calls are iRO IDs; the canonical ID space every RO server
// emulator (Hercules, rAthena, etc.) exposes. The rocalc backend translates
// them to its internal indices via mapping.json at runtime.
//
//  | item                  | iRO id | rocalc internal id |
//  |-----------------------|--------|--------------------|
//  | Knife (3-slot dagger) | 1201   | m_Item[1]          |
//  | Cotton Shirt (armor)  | 2301   | m_Item[281]        |
//  | Guard (shield)        | 2101   | m_Item[306]        |
//  | Andre Card            | 4043   | m_Card[11]         |
//  | Soldier Skeleton Card | 4086   | m_Card[41]         |

// Tests that don't mutate the active class share one createShim() across
// runs; each fresh shim costs ~1.5 s, while reset() is two recompute calls
// over the existing window. setClass-mutating tests use their own session
// because reset() doesn't roll back rocalc's class globals (that would
// require ClickJob, which is the expensive path we're avoiding here).
describe("shim shared session; Novice default class", () => {
  let shim: ShimSession;
  before(() => {
    shim = createShim();
  });
  beforeEach(() => {
    shim.reset();
  });

  // Default state: rocalc auto-initializes Novice / BaseLV 1 / JobLV 1 /
  // all stats 1, with the auto-adjust BaseLV checkbox disabled by the shim.
  // HIT for that state is 1 + DEX = 2.
  test("default Novice level 1, all stats 1, HIT=2", () => {
    assert.equal(
      shim.readDerivedStats().hit,
      2,
      "default HIT should be 2 (BaseLV 1 + DEX 1)",
    );
  });

  test("readDerivedStats returns full stat block at default state", () => {
    assert.deepEqual(shim.readDerivedStats(), {
      hit: 2,
      flee: 2,
      cri: 1,
      atk: { base: 1, plus: 0 },
      matk: { min: 1, max: 1 },
      def: { hard: 0, soft: 1 },
      mdef: { hard: 0, soft: 1 },
      aspd: 150.3,
      maxHp: 40,
      maxSp: 11,
      statPointsRemaining: 48,
    });
  });

  // setStats writes A_STR/A_AGI/A_VIT/A_INT/A_DEX/A_LUK and triggers rocalc's
  // recompute. With DEX=99 on a Novice level 1, HIT should be 1+99 = 100.
  test("setStats(dex=99) yields HIT=100", () => {
    shim.setStats({ str: 1, agi: 1, vit: 1, int: 1, dex: 99, luk: 1 });
    assert.equal(shim.readDerivedStats().hit, 100);
  });

  // FLEE = baseLV + AGI; same shape as HIT but on a different stat. Catches
  // a setStats/readDerivedStats wiring that mistakenly aliases the two.
  test("setStats(agi=99) yields FLEE=100", () => {
    shim.setStats({ str: 1, agi: 99, vit: 1, int: 1, dex: 1, luk: 1 });
    assert.equal(shim.readDerivedStats().flee, 100);
  });

  // Pre-renewal Novice ATK = STR + ⌊STR/10⌋² + ⌊DEX/5⌋ + ⌊LUK/5⌋.
  // At STR=10, others=1: 10 + 1 + 0 + 0 = 11.
  test("setStats(str=10) yields ATK base=11", () => {
    shim.setStats({ str: 10, agi: 1, vit: 1, int: 1, dex: 1, luk: 1 });
    assert.deepEqual(shim.readDerivedStats().atk, { base: 11, plus: 0 });
  });

  // setLevel updates BaseLV and JobLV. Higher BaseLV grows the status point
  // pool and feeds HIT/FLEE/MaxHP. Novice has no job-bonus stat at jobLV 1.
  test("setLevel({base:50, job:1}) raises HIT to 51", () => {
    shim.setLevel({ base: 50, job: 1 });
    const stats = shim.readDerivedStats();
    assert.equal(stats.hit, 51, "HIT = baseLV(50) + DEX(1)");
    assert.equal(stats.statPointsRemaining, 420, "lvl 50 stat point pool");
  });

  // equip('weapon') writes A_weapon1 + A_Weapon_refine and triggers rocalc's
  // weapon-onchange chain. Knife (iRO 1201) has atk=17; on a Novice STR=1,
  // total base ATK should be 1 + 17 = 18.
  test("equip weapon Knife raises ATK base to 18", () => {
    shim.equip("weapon", { id: 1201, refine: 0 });
    assert.deepEqual(shim.readDerivedStats().atk, { base: 18, plus: 0 });
  });

  // Refinement on a level-1 weapon gives +2 ATK per refine (pre-renewal).
  // Knife +5 => +10 to the "plus" component; base unchanged.
  test("equip weapon Knife +5 adds +10 to ATK plus", () => {
    shim.equip("weapon", { id: 1201, refine: 5 });
    assert.deepEqual(shim.readDerivedStats().atk, { base: 18, plus: 10 });
  });

  // equip('armor') writes A_body + A_BODY_REFINE; same shape as weapon but
  // without ClickWeaponType. Cotton Shirt (iRO 2301) has DEF=1; equipping it
  // on Novice raises DEF.hard from 0 to 1, leaving soft DEF (= VIT 1)
  // unchanged.
  test("equip armor Cotton Shirt raises hard DEF from 0 to 1", () => {
    shim.equip("armor", { id: 2301, refine: 0 });
    assert.deepEqual(shim.readDerivedStats().def, { hard: 1, soft: 1 });
  });

  // Pre-renewal armor refine adds +1 DEF per refine for normal armors at low
  // refine levels. Cotton Shirt +7 yields +5 hard DEF on top of the base 1
  // (refinement bonus accelerates above +4; empirically 7 → 5).
  test("equip armor Cotton Shirt +7 raises hard DEF to 6", () => {
    shim.equip("armor", { id: 2301, refine: 7 });
    assert.deepEqual(shim.readDerivedStats().def, { hard: 6, soft: 1 });
  });

  // equip('shield') uses the same onchange chain as armor plus a trailing
  // setShieldWeight call (see SLOTS.shield in the rocalc backend). Guard
  // (iRO 2101, DEF 3) is one of the few shields a Novice can equip.
  test("equip shield Guard raises hard DEF from 0 to 3", () => {
    shim.equip("shield", { id: 2101, refine: 0 });
    assert.deepEqual(shim.readDerivedStats().def, { hard: 3, soft: 1 });
  });

  // Cards passed via equip's cards: [iRO_ids]. Andre Card (iRO 4043) is
  // +20 ATK on weapon. Knife (18 ATK base on Novice STR=1) + Andre = 38.
  test("equip weapon Knife with Andre card raises ATK base by 20", () => {
    shim.equip("weapon", { id: 1201, refine: 0, cards: [4043] });
    assert.deepEqual(shim.readDerivedStats().atk, { base: 38, plus: 0 });
  });

  // Soldier Skeleton card (iRO 4086) is +9 critical on weapon. Default CRI
  // for Novice all-stats-1 is 1; with this card it becomes 10. Also verifies
  // card effects route through the right field; a card that affects CRI
  // but not ATK should not change ATK.
  test("equip weapon Knife with Soldier Skeleton card raises CRI by 9", () => {
    shim.equip("weapon", { id: 1201, refine: 0, cards: [4086] });
    const stats = shim.readDerivedStats();
    assert.equal(stats.cri, 10, "CRI = 1 + 9 from card");
    assert.deepEqual(
      stats.atk,
      { base: 18, plus: 0 },
      "ATK unaffected by crit card",
    );
  });

  // equip throws on unknown slot keys to catch typos and orchestrator bugs
  // rather than silently no-op'ing. The id passed here is irrelevant; the
  // slot validation runs before any id translation.
  test("equip rejects unknown slot key", () => {
    // Cast through unknown to bypass the SlotKey union for this test;
    // we're verifying the runtime guard, not the type system.
    assert.throws(
      () => shim.equip("shoes" as unknown as "weapon", { id: 1201 }),
      /unknown equipment slot/,
    );
  });

  // Refinement is meaningless on accessories and mid/bottom headgear; reject
  // nonzero refine on those slots so callers can't silently get wrong numbers.
  test("equip rejects refine on accessory slot", () => {
    assert.throws(
      () => shim.equip("accessory1", { id: 1201, refine: 5 }),
      /does not support refinement/,
    );
  });

  // Armor only has 1 card slot. Passing 2 cards is rejected up front, not
  // silently truncated. Length check fires before id translation, so the
  // values here don't have to be real iRO IDs.
  test("equip rejects too many cards for armor slot", () => {
    assert.throws(
      () => shim.equip("armor", { id: 2301, cards: [4043, 4086] }),
      /supports max 1 card/,
    );
  });

  // Head bottom has no card slot at all. Any nonzero cards array should
  // throw rather than be ignored.
  test("equip rejects cards on head-bottom slot", () => {
    assert.throws(
      () => shim.equip("headBot", { id: 1201, cards: [4043] }),
      /supports max 0 card/,
    );
  });

  // iRO IDs not in the mapping table throw with an actionable message;
  // callers shouldn't silently get a wrong-item equip when the mapping is
  // missing an entry.
  test("equip throws on unmapped iRO id", () => {
    assert.throws(
      () => shim.equip("weapon", { id: 99999999 }),
      /no rocalc mapping/,
    );
  });

  // Class restriction guard: equipping an item the active class can't wear
  // (Cotton Shirt 2301 is iRO armor, but pasted into the weapon slot it
  // wouldn't be in A_weapon1's option list) throws a clear error rather
  // than letting rocalc choke on m_Item[''] later with a cryptic message.
  test("equip throws when item not in active class option list", () => {
    assert.throws(
      () => shim.equip("weapon", { id: 2301 }), // Cotton Shirt is armor, not weapon
      /not in current class's 'weapon' option list/,
    );
  });

  // setEnemy switches the active combat-sim target. iroMobId is the iRO
  // mob id (Poring = 1002); the shim translates it to rocalc's internal
  // m_Monster index (272 for Poring) via mapping.json. Damage min/ave
  // should be > 0 since Novice with Knife can hit Poring; Poring drops in
  // <60s of swinging.
  test("setEnemy(Poring) + Knife yields positive damage and finite battle time", () => {
    shim.equip("weapon", { id: 1201 });
    shim.setEnemy(1002);
    const c = shim.readCombatResults();
    assert.ok(
      c.damage.min !== null && c.damage.min > 0,
      `expected damage.min > 0, got ${c.damage.min}`,
    );
    assert.ok(
      c.damage.ave !== null &&
        c.damage.min !== null &&
        c.damage.ave >= c.damage.min,
      "avg damage at least min",
    );
    assert.ok(c.hit !== null && c.hit > 0, `expected hit% > 0, got ${c.hit}`);
    assert.ok(
      c.battleTimeSec !== null && c.battleTimeSec > 0 && c.battleTimeSec < 600,
      `expected finite battleTimeSec, got ${c.battleTimeSec}`,
    );
    assert.equal(c.enemy.race, "Plant");
    assert.equal(c.enemy.size, "Medium");
    assert.equal(c.enemy.hp, 50);
  });

  // setEnemy validates input shape (negative / non-integer) and rejects
  // iRO mob ids absent from mapping.json. server.ts's classifyError
  // recognizes both error patterns ("mob id ... out of range" /
  // "no rocalc mapping") and maps them to HTTP 400. (The "no rocalc
  // mapping" message is backend-internal; the contract just says the
  // shim throws a 4xx-classifiable error.)
  test("setEnemy bounds + mapping checks", () => {
    assert.throws(() => shim.setEnemy(-1), /must be a non-negative integer/);
    assert.throws(() => shim.setEnemy(1.5), /must be a non-negative integer/);
    // 99999999 has no mob mapping entry; caller-fixable error.
    assert.throws(() => shim.setEnemy(99999999), /no rocalc mapping/);
  });

  // setEnemy can land on a target the build can't kill (Novice barehand vs
  // MVP Eddga, iRO 1115). rocalc reports those with sentinel strings in
  // BattleTime ("Too high to calculate"). The shim must surface those
  // gracefully; null rather than NaN; so the orchestrator can detect
  // "build can't beat this target" without parser failures. Per-hit damage
  // itself stays a finite small number (rocalc still computes 1+ per hit);
  // the kill-time sentinel is the proper "build can't beat this" signal.
  test("setEnemy(Eddga MVP) on a too-weak build returns sentinel battleTime, not NaN", () => {
    // No weapon, default level 1, default stats; definitely can't kill Eddga.
    shim.setEnemy(1115);
    const c = shim.readCombatResults();
    assert.equal(c.enemy.hp, 152000, "Eddga HP from m_Monster");
    // Per-hit damage is finite (rocalc still resolves a tiny number per swing).
    assert.ok(
      c.damage.max !== null && c.damage.max >= 0,
      `damage.max should be a finite non-negative number, got ${c.damage.max}`,
    );
    // battleTimeSec is null when rocalc renders "Too high to calculate".
    assert.equal(
      c.battleTimeSec,
      null,
      "expect battleTimeSec=null for an unkillable target",
    );
  });

  // Schema sanity: setEnemyInline with Poring's documented stats should
  // produce the same combat numbers as setEnemy(1002). If the m_Monster
  // column mapping ever drifts (rocalc snapshot update, schema-discovery
  // regression), this test catches it via concrete numerical equality.
  test("setEnemyInline with Poring stats matches setEnemy(1002)", () => {
    // Use a STR-build with a sword so atk/def/hp scaling is observable.
    shim.setLevel({ base: 50, job: 30 });
    shim.setStats({ str: 60, agi: 30, vit: 10, int: 10, dex: 60, luk: 1 });
    shim.equip("weapon", { id: 1201 });

    shim.setEnemy(1002); // Poring
    const fromId = shim.readCombatResults();

    // Poring's Hercules stats: HP 50, Atk [7,10], Def 0, Mdef 5, Plant,
    // Water lvl 1, Medium.
    shim.setEnemyInline({
      hp: 50,
      atk_min: 7,
      atk_max: 10,
      def: 0,
      mdef: 5,
      race: "RC_Plant",
      element: "Ele_Water",
      element_lv: 1,
      size: "Size_Medium",
    });
    const inline = shim.readCombatResults();

    // Combat-load-bearing fields must match: damage range (atk-vs-def),
    // hit/flee/dodge (level + dex), battleTime (HP), incoming damage
    // (atk_min/max). Element/race/size affect damage modifiers from
    // any equipped element/race-keyed cards (none here, but the
    // readout strings should still match).
    assert.equal(inline.damage.min, fromId.damage.min, "damage.min");
    assert.equal(inline.damage.max, fromId.damage.max, "damage.max");
    assert.equal(inline.hit, fromId.hit, "hit");
    assert.equal(inline.battleTimeSec, fromId.battleTimeSec, "battleTimeSec");
    assert.equal(inline.incoming.min, fromId.incoming.min, "incoming.min");
    assert.equal(inline.incoming.max, fromId.incoming.max, "incoming.max");
    assert.equal(inline.enemy.hp, fromId.enemy.hp, "enemy.hp");
    assert.equal(inline.enemy.race, fromId.enemy.race, "enemy.race");
    assert.equal(inline.enemy.element, fromId.enemy.element, "enemy.element");
    assert.equal(inline.enemy.size, fromId.enemy.size, "enemy.size");
  });

  // Regression: setEnemyInline's finally-block must restore
  // m_Monster[SCRATCH_SLOT] to its pre-call state. If the snapshot/
  // restore ordering ever regresses (e.g., snapshot taken AFTER the row
  // overwrite, or restore omitted), a subsequent setEnemy(1002) would
  // read corrupted Poring stats from the scratch slot. Verify by
  // round-tripping: inline-Eddga-shape → setEnemy(1002) → numbers must
  // match a baseline setEnemy(1002) call exactly.
  //
  // Uses stats that meaningfully differ from Poring (152k HP, fire,
  // Large, high atk) so any leak from the inline call would visibly
  // skew Poring's combat output (Poring HP 50, Water, Medium, atk 7-10).
  test("setEnemyInline → setEnemy(Poring) leaves Poring stats uncorrupted", () => {
    shim.setLevel({ base: 50, job: 30 });
    shim.setStats({ str: 60, agi: 30, vit: 10, int: 10, dex: 60, luk: 1 });
    shim.equip("weapon", { id: 1201 });

    // Baseline: setEnemy(1002) before any inline call.
    shim.setEnemy(1002);
    const baseline = shim.readCombatResults();

    // Inline-Eddga-shape: stats that look nothing like Poring.
    shim.setEnemyInline({
      hp: 152000,
      atk_min: 800,
      atk_max: 1200,
      def: 25,
      mdef: 15,
      race: "RC_Brute",
      element: "Ele_Fire",
      element_lv: 3,
      size: "Size_Large",
      level: 65,
    });
    // Read combat results to ensure rocalc fully ran with the inline row.
    shim.readCombatResults();

    // After the inline call's finally restored m_Monster[SCRATCH_SLOT]
    // back to Poring, a fresh setEnemy(1002) must produce identical
    // numbers to the baseline above. (Without the restore, Poring's
    // m_Monster row would still hold Eddga-shape stats and the
    // combat readouts would diverge wildly.)
    shim.setEnemy(1002);
    const afterInline = shim.readCombatResults();

    assert.equal(
      afterInline.enemy.hp,
      baseline.enemy.hp,
      "Poring HP preserved",
    );
    assert.equal(
      afterInline.enemy.race,
      baseline.enemy.race,
      "Poring race preserved",
    );
    assert.equal(
      afterInline.enemy.element,
      baseline.enemy.element,
      "Poring element preserved",
    );
    assert.equal(
      afterInline.enemy.size,
      baseline.enemy.size,
      "Poring size preserved",
    );
    assert.equal(
      afterInline.damage.min,
      baseline.damage.min,
      "Poring damage.min preserved",
    );
    assert.equal(
      afterInline.damage.max,
      baseline.damage.max,
      "Poring damage.max preserved",
    );
    assert.equal(
      afterInline.battleTimeSec,
      baseline.battleTimeSec,
      "Poring battleTimeSec preserved",
    );
  });

  // setSkills([]) is the explicit no-op; callers that don't specify
  // skills should not crash, and the form's defaults (all 0) stay.
  test("setSkills([]) is a no-op", () => {
    assert.doesNotThrow(() => shim.setSkills([]));
  });

  // An iRO skill id that's not in the active class's m_JobBuff is
  // silently skipped (see setSkills doc for the relaxed behavior).
  // rocalc's m_JobBuff is the calc engine's stat-effect-skill set,
  // not the player's allocatable skill tree (skill_tree.conf). Many
  // skills the player legitimately allocates (TK kicks, Knight active
  // damage skills) aren't in m_JobBuff but must still pass through
  // scoring without throwing. Sword Mastery (id 3) is a Swordsman
  // passive; not in Novice's tree, so should silently no-op for the
  // active Novice session.
  test("setSkills silently skips skills not in active class tree", () => {
    assert.doesNotThrow(() => shim.setSkills([{ id: 3, level: 5 }]));
  });
});

// Class-mutating test in its own session; setSkills exercises the full
// path: setClass populates m_JobBuff[1] (Swordsman) and the form's
// A_skill0..A_skill21 selects, then setSkills writes into the right
// slot. Sword Mastery (iRO id 3) is a Swordsman passive that's reliably
// in m_JobBuff[1]. Verifies the wiring; rocalc-semantic effects on derived
// stats (Sword Mastery + sword equipped → ATK bump) are out of scope here.
test("setSkills on Swordsman accepts Sword Mastery (id 3)", () => {
  const s = createShim();
  s.setClass("swordman");
  assert.doesNotThrow(() => s.setSkills([{ id: 3, level: 10 }]));
});

// reset() must restore the session to its initial post-create state so the
// shared-session pattern above is sound. Verifies all four mutators are
// rolled back: setStats, setLevel, equip-weapon, equip-armor.
test("reset() returns shim to initial state after mutations", () => {
  const shim = createShim();
  shim.setStats({ str: 99, agi: 99, vit: 99, int: 99, dex: 99, luk: 99 });
  shim.setLevel({ base: 50, job: 50 });
  shim.equip("weapon", { id: 1201, refine: 5 });
  shim.equip("armor", { id: 2301, refine: 7 });

  shim.reset();

  const stats = shim.readDerivedStats();
  assert.equal(stats.hit, 2, "HIT back to 2");
  assert.deepEqual(stats.atk, { base: 1, plus: 0 }, "ATK back to 1+0");
  assert.deepEqual(stats.def, { hard: 0, soft: 1 }, "DEF back to 0+1");
  assert.equal(stats.maxHp, 40, "MaxHP back to 40");
  assert.equal(stats.statPointsRemaining, 48, "STPOINT back to 48");
});

// setClass("taekwon_kid") switches to Taekwon Kid (the current focus class).
// Verify by behavioral effect: at the same level/stats, TK and Novice diverge
// on derived stats; different HP factors, job-bonus tables, etc. Uses two
// own sessions because reset() doesn't roll back class.
test('setClass("taekwon_kid") produces different derived stats than Novice', () => {
  const novice = createShim();
  novice.setLevel({ base: 50, job: 50 });
  const noviceStats = novice.readDerivedStats();

  const tk = createShim();
  tk.setClass("taekwon_kid");
  tk.setLevel({ base: 50, job: 50 });
  const tkStats = tk.readDerivedStats();

  assert.notEqual(
    tkStats.maxHp,
    noviceStats.maxHp,
    "TK and Novice MaxHP differ at same level",
  );
  assert.notEqual(
    tkStats.maxSp,
    noviceStats.maxSp,
    "TK and Novice MaxSP differ at same level",
  );
});

test("setClass rejects unknown class names with valid options listed", () => {
  const s = createShim();
  assert.throws(() => s.setClass("ranger"), /unknown class/);
});
