// Lock-in test: every name in the rocalc backend's CLASS_TO_ROCALC_ID
// must load cleanly via setClass. Catches regressions where a future
// rocalc-data refresh or form-template edit silently breaks a class.
//
// Slower than the rest of the suite; spins one fresh shim per
// canonical class; but still well under a minute. Worth the cost
// because a broken class only surfaces here; the runtime treats
// unknown class errors as 4xx, masking the regression.

import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// One canonical name per class id we promise to support. Aliases
// (mage, swordsman, taekwon_master, supernovice, sin_x, scholar,
// biochemist, minstrel) aren't tested separately; same id, same code
// path. Update this list in lockstep with CLASS_TO_ROCALC_ID in
// src/backends/rocalc/index.ts.
//
// Coverage: all 46 pre-renewal m_Job slots load EXCEPT id 34 (High
// Novice; rocalc has empty placeholder data). Confirmed via
// test/class-coverage.ts.
const SUPPORTED_CLASSES = [
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

for (const className of SUPPORTED_CLASSES) {
  test(`setClass("${className}") loads cleanly`, () => {
    const shim = createShim();
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
