// Lock-in checks: every name in the rocalc backend's CLASS_TO_ROCALC_ID must
// load cleanly via setClass. Catches regressions where a future rocalc-data
// refresh or form-template edit silently breaks a class.
//
// The 45 supported classes are sharded across classes_1..classes_5.test.ts so
// node --test runs them as parallel processes. setClass (~1.5s ClickJob) is
// serial WITHIN a file and the calc work is synchronous, so the only way to
// parallelize the sweep is across files; one big file made this the slowest in
// the suite (~64s). Each shard reuses one shim (the ~2.4s jsdom + engine load
// is paid once per shard); back-to-back setClass on a reused shim mirrors how
// the production ShimPool reuses workers, and no buffs are applied here, so
// there is no bank state to leak between classes.
//
// To change the shard count, update the `count` passed to shard() in every
// classes_N.test.ts and add/remove shard files to match.

import { test, before } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// One canonical name per class id we promise to support. Aliases (mage,
// swordsman, taekwon_master, supernovice, sin_x, scholar, biochemist, minstrel)
// aren't tested separately; same id, same code path. Update this list in
// lockstep with CLASS_TO_ROCALC_ID in src/backends/rocalc/index.ts.
//
// Coverage: all 46 pre-renewal m_Job slots load EXCEPT id 34 (High Novice;
// rocalc has empty placeholder data). Confirmed via test/class-coverage.ts.
export const SUPPORTED_CLASSES = [
  // First job
  "novice",
  "swordman",
  "thief",
  "acolyte",
  "archer",
  "magician",
  "merchant",

  // Second job
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

  // Trans
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

  // High first-class (id 34 High Novice excluded; rocalc placeholder)
  "high_swordman",
  "high_thief",
  "high_acolyte",
  "high_archer",
  "high_magician",
  "high_merchant",

  // Taekwon path
  "taekwon_kid",
  "star_gladiator",
  "soul_linker",

  // Expanded
  "ninja",
  "gunslinger",
];

// Round-robin slice `index` of `count` over SUPPORTED_CLASSES. Round-robin
// (not contiguous) deliberately spreads the cost-heavy trans classes (bigger
// skill trees => slower ClickJob, ~2.5-3s each) evenly across shards instead of
// clustering them in the tier-contiguous block, which balances shard runtimes.
export function shard(index: number, count: number): string[] {
  return SUPPORTED_CLASSES.filter((_, i) => i % count === index);
}

// Registers one "loads cleanly" test per class against a single reused shim.
// Called at module top-level by each classes_N.test.ts shard file.
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
