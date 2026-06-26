// Lock-in checks: every name in the rocalc backend's CLASS_TO_ROCALC_ID must
// load cleanly via setClass. Catches regressions where a future rocalc-data
// refresh or form-template edit silently breaks a class.
//
// The 45 supported classes are split by job tier across classes_first_job /
// _second_job / _transcendent / _high_first / _expanded .test.ts so node --test
// runs them as parallel processes. setClass (~1.5-3s ClickJob, heavier for
// trans classes) is serial WITHIN a file and the calc work is synchronous, so
// the only way to parallelize the sweep is across files; one big file made this
// the slowest in the suite (~64s). Each shard reuses one shim (the ~2.4s jsdom
// + engine load is paid once per shard); back-to-back setClass on a reused shim
// mirrors how the production ShimPool reuses workers, and no buffs are applied
// here, so there is no bank state to leak between classes.
//
// Tiers are uneven (second job 14, trans 13 are the largest), but they're the
// natural class grouping and each shard stays well under the suite's
// contention-bound wall. SUPPORTED_CLASSES below is composed from the tier
// arrays, so the per-tier files and the full list can't drift apart.

import { test, before } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// One canonical name per class id we promise to support. Aliases (mage,
// swordsman, taekwon_master, supernovice, sin_x, scholar, biochemist, minstrel)
// aren't tested separately; same id, same code path. Update these arrays in
// lockstep with CLASS_TO_ROCALC_ID in src/backends/rocalc/index.ts.
//
// Coverage: all 46 pre-renewal m_Job slots load EXCEPT id 34 (High Novice;
// rocalc has empty placeholder data). Confirmed via test/class-coverage.ts.

export const FIRST_JOB = [
  "novice",
  "swordman",
  "thief",
  "acolyte",
  "archer",
  "magician",
  "merchant",
];

export const SECOND_JOB = [
  "knight",
  "assassin",
  "priest",
  "hunter",
  "wizard",
  "blacksmith",
  "crusader",
  "rogue",
  "monk",
  "bard",
  "dancer",
  "sage",
  "alchemist",
  "super_novice",
];

export const TRANSCENDENT = [
  "lord_knight",
  "assassin_cross",
  "high_priest",
  "sniper",
  "high_wizard",
  "whitesmith",
  "paladin",
  "stalker",
  "champion",
  "clown",
  "gypsy",
  "professor",
  "creator",
];

// Transcendent first-class slots (id 34 High Novice excluded; rocalc placeholder).
export const HIGH_FIRST = [
  "high_swordman",
  "high_thief",
  "high_acolyte",
  "high_archer",
  "high_magician",
  "high_merchant",
];

// Expanded classes: the Taekwon path plus Ninja / Gunslinger.
export const EXPANDED = [
  "taekwon_kid",
  "star_gladiator",
  "soul_linker",
  "ninja",
  "gunslinger",
];

// Single source of truth for the full supported set, composed from the tiers so
// the per-tier shard files can't drift out of sync with the whole.
export const SUPPORTED_CLASSES = [
  ...FIRST_JOB,
  ...SECOND_JOB,
  ...TRANSCENDENT,
  ...HIGH_FIRST,
  ...EXPANDED,
];

// Registers one "loads cleanly" test per class against a single reused shim.
// Called at module top-level by each classes_<tier>.test.ts shard file.
export function runClassLoadChecks(classes: string[]): void {
  let shim: ReturnType<typeof createShim>;
  before(() => {
    shim = createShim();
  });

  for (const className of classes) {
    test(`setClass("${className}") loads cleanly`, () => {
      shim.setClass(className);
      // readDerivedStats forces the calc to run; throws if the shim is
      // in a half-initialized state from a partial setClass.
      const stats = shim.readDerivedStats();
      assert.ok(
        stats.maxHp > 0,
        `MaxHP should be positive after setClass(${className}), got ${stats.maxHp}`,
      );
      assert.ok(
        stats.aspd > 0,
        `ASPD should be positive after setClass(${className}), got ${stats.aspd}`,
      );
    });
  }
}
