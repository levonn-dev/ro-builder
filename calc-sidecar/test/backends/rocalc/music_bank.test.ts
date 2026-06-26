import { test, before } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// The A3 "Music and Dance Skills" bank (n_Skill3SW -> n_A_Buf3[]) is wired as a
// backend-only capability: these are Bard/Dancer SONGS, which do not affect the
// performer in pre-renewal, so they are NOT self-buffs (no overlay row, never
// surface in ClassBuffs/resolver). They are driven directly via setBuffs here to
// prove the music driver populates the A3 selects, seeds the performer
// sub-params, and moves the verified scored field. An Instrument is equipped so
// weapon-sensitive effects manifest.
const VIOLIN = 1901;

let shim: ReturnType<typeof createShim>;

before(() => {
  shim = createShim();
  shim.setClass("clown");
});

function fresh() {
  shim.reset();
  shim.setLevel({ base: 99, job: 70 });
  shim.setStats({ str: 80, agi: 70, vit: 30, int: 30, dex: 70, luk: 20 });
  shim.equip("weapon", { id: VIOLIN });
  return shim;
}

test("a_whistle raises flee (A3 music driver populates + drives the bank)", () => {
  const base = fresh().readDerivedStats().flee;
  const s = fresh();
  s.setBuffs([{ name: "a_whistle", level: 10 }]);
  const got = s.readDerivedStats().flee;
  assert.ok(got > base, `a_whistle should raise flee (${base} -> ${got})`);
});

test("reset() clears the A3 bank (no flee leak)", () => {
  const s = fresh();
  const plain = s.readDerivedStats().flee;
  s.setBuffs([{ name: "a_whistle", level: 10 }]);
  s.readDerivedStats();
  s.reset();
  s.setClass("clown");
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 80, agi: 70, vit: 30, int: 30, dex: 70, luk: 20 });
  s.equip("weapon", { id: VIOLIN });
  const after = s.readDerivedStats().flee;
  assert.equal(
    after,
    plain,
    "reset must clear n_Skill3SW/n_A_Buf3 (no flee leak)",
  );
});

// Remaining scored songs (verified fields) + wired-but-inert songs.
const SCORED: [string, number, "flee" | "aspd" | "maxHp" | "hit" | "cri"][] = [
  ["assassin_cross_of_sunset", 10, "aspd"],
  ["apple_of_idun", 10, "maxHp"],
  ["humming", 10, "hit"],
  ["fortunes_kiss", 10, "cri"],
];
for (const [name, lv, field] of SCORED) {
  test(`${name} raises ${field}`, () => {
    const base = fresh().readDerivedStats()[field];
    const s = fresh();
    s.setBuffs([{ name, level: lv }]);
    const got = s.readDerivedStats()[field];
    assert.ok(got > base, `${name} should raise ${field} (${base} -> ${got})`);
  });
}

test("drum_on_the_battlefield raises damage.ave", () => {
  const base = fresh().readCombatResults().damage.ave ?? 0;
  const s = fresh();
  s.setBuffs([{ name: "drum_on_the_battlefield", level: 5 }]);
  const got = s.readCombatResults().damage.ave ?? 0;
  assert.ok(got > base, `drum should raise ave (${base} -> ${got})`);
});

// Wired-but-inert in the auto-attack sim (cast-time / resist / exp / lvl-4-weapon
// gate). Bound per the "bind every control rocalc exposes" directive; assert they
// drive without throwing rather than asserting a field move.
const INERT = [
  "poem_of_bragi",
  "service_for_you",
  "dont_forget_me",
  "invulnerable_siegfried",
  "rich_man_kim",
  "ring_of_nibelungen",
];
for (const name of INERT) {
  test(`${name} is wired and drives without error`, () => {
    const s = fresh();
    assert.doesNotThrow(() => s.setBuffs([{ name, level: 5 }]));
    const ave = s.readCombatResults().damage.ave;
    assert.ok(ave != null, `${name}: combat still computes`);
  });
}
