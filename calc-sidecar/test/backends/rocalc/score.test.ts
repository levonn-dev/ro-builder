import { test, describe, before } from "node:test";
import assert from "node:assert/strict";
import { score, resetScoreShim } from "../../../src/score.ts";

// score() is the unified "build → numbers" entry point the HTTP layer
// hands every /score request to. It owns a single long-lived shim so
// requests don't pay the ~1.5s rocalc-load cost; reset() between calls
// keeps state isolated. Tests use the same shared shim for the same
// reason; and we expose resetScoreShim() so a final test can prove the
// shim survives across many sequential calls without drifting.

describe("score(); default-class (Novice) requests", () => {
  before(() => {
    resetScoreShim();
  });

  // Default build (no fields) → defaults from the shim baseline: Novice
  // level 1, all stats 1. HIT = baseLV(1) + DEX(1) = 2; the same readback
  // we use as a sanity-check for the shim itself.
  test("empty request returns default Novice readDerivedStats", () => {
    const r = score({});
    assert.equal(r.derived.hit, 2);
    assert.deepEqual(r.derived.atk, { base: 1, plus: 0 });
    assert.equal(r.derived.maxHp, 40);
    assert.equal(
      r.combat,
      undefined,
      "combat is omitted when no enemy specified",
    );
  });

  // Stats-only request: setStats applied, derived stats reflect the change.
  // DEX=99 + baseLV=1 = HIT 100; verifies setStats is wired through the
  // request shape correctly.
  test("stats-only request sets stats and returns updated derived", () => {
    const r = score({
      stats: { str: 1, agi: 1, vit: 1, int: 1, dex: 99, luk: 1 },
    });
    assert.equal(r.derived.hit, 100);
  });

  // Equipment is a map of slot → spec; the server iterates it and calls
  // shim.equip per entry. Card ids inside spec are iRO ids (4043 = Andre
  // = +20 ATK on weapon); translated to rocalc ids via the mapping table
  // by the shim, end-to-end coverage check.
  test("equipment + cards in request flows through the iRO-id translation", () => {
    const r = score({
      stats: { str: 1, agi: 1, vit: 1, int: 1, dex: 1, luk: 1 },
      equipment: {
        weapon: { id: 1201, refine: 0, cards: [4043] }, // Knife + Andre
        armor: { id: 2301, refine: 0 }, // Cotton Shirt
      },
    });
    // Knife base ATK 17 + STR 1 + Andre +20 = 38
    assert.deepEqual(r.derived.atk, { base: 38, plus: 0 });
    // Cotton Shirt has DEF 1 → hard DEF goes 0→1
    assert.deepEqual(r.derived.def, { hard: 1, soft: 1 });
  });

  // Including enemy in the request triggers setEnemy + readCombatResults.
  // Poring (iRO mob id 1002) is the canonical small target; Knife-wielding
  // Novice should land hits and resolve a finite battle time. The shim
  // translates iRO 1002 → rocalc m_Monster index 272 via mapping.json.
  test("enemy in request adds combat block to response", () => {
    const r = score({
      equipment: { weapon: { id: 1201, refine: 0 } },
      enemy: 1002, // Poring (iRO mob id)
    });
    assert.ok(r.combat, "combat should be present when enemy specified");
    assert.ok(
      r.combat!.damage.min !== null && r.combat!.damage.min > 0,
      "damage.min > 0 vs Poring",
    );
    assert.ok(
      r.combat!.battleTimeSec !== null &&
        r.combat!.battleTimeSec > 0 &&
        r.combat!.battleTimeSec < 600,
      "finite battleTimeSec",
    );
    assert.equal(r.combat!.enemy.race, "Plant");
  });

  // Two consecutive calls with different inputs should not bleed state;
  // each request must reset to baseline before applying its build. Catches
  // a "we forgot to reset between requests" regression.
  test("consecutive calls reset state between them", () => {
    score({ stats: { str: 99, agi: 99, vit: 99, int: 99, dex: 99, luk: 99 } });
    const r = score({});
    assert.equal(
      r.derived.hit,
      2,
      "second empty request gets default state, not stale stats",
    );
  });
});

// setClass is heavyweight; it runs ClickJob which rebuilds option lists
// and resets equipment slots. Outside the shared describe block so the
// shim can be torn down/recreated at the end without disturbing other
// tests' assumptions.
test("class in request switches active class for this build", () => {
  resetScoreShim();
  const r = score({
    class: "taekwon_kid",
    level: { base: 50, job: 50 },
    stats: { str: 1, agi: 50, vit: 1, int: 1, dex: 1, luk: 1 },
  });
  // TK with AGI 50 at lvl 50: FLEE = baseLV(50) + AGI(50) + tk-job-bonus.
  // Just asserting it's well into 3-digits; exact value depends on
  // job-bonus tables we haven't pinned to a specific number.
  assert.ok(
    r.derived.flee >= 100,
    `expected FLEE >= 100 with TK at lvl 50/50 + AGI 50, got ${r.derived.flee}`,
  );
});

// Regression: once a setClass for a class the shim can't fully initialize
// (Knight, Hunter, etc.; missing form-template elements throw a TypeError
// deep inside ClickJob), the shim's internal state is half-applied and
// every subsequent request starts erroring on "Cannot read properties of
// null/undefined"; even minimal Novice ones with no equipment.
//
// Fix: score() catches non-validation errors and disposes the cached shim
// so the next request rebuilds from scratch. This test triggers a known-
// broken class, asserts the immediate throw, then asserts the next
// minimal call succeeds (which it wouldn't without disposal).
test("non-validation error disposes shim so subsequent requests recover", () => {
  resetScoreShim();
  // Warm up: a clean baseline call so we know the shim is in good
  // shape going in.
  const baseline = score({});
  assert.equal(baseline.derived.hit, 2, "baseline Novice should be HIT=2");

  // Trigger corruption: pass a numeric class id that still fails inside
  // rocalc's ClickJob (id 34; High Novice, a rocalc placeholder with
  // empty skill data, throws "Cannot read properties of undefined" on
  // the first m_Skill[m_JobBuff[34][0]][1] access). The numeric escape
  // hatch in resolveRocalcClassID lets us reach ClickJob(34) without
  // the name validation rejecting it first. The error leaves form
  // globals half-applied; the disposal path is what we're testing.
  let brokenThrew: Error | null = null;
  try {
    score({ class: "34" });
  } catch (e) {
    brokenThrew = e as Error;
  }
  assert.ok(brokenThrew, "expected setClass on broken class to throw");
  assert.ok(
    !/no rocalc mapping|unknown class|unknown equipment slot/.test(
      brokenThrew.message,
    ),
    `broken-class error should be non-validation (corruption-class), got: ${brokenThrew.message}`,
  );

  // Recovery: minimal Novice call. Without the disposal fix this throws
  // "Cannot read properties of null/undefined" because the shim's globals
  // are half-applied from the failed setClass. With the fix the shim was
  // disposed, so this call creates a fresh one and succeeds.
  const recovered = score({});
  assert.equal(
    recovered.derived.hit,
    2,
    "minimal Novice request must succeed after a corrupting failure",
  );
});
