import { test } from "node:test";
import assert from "node:assert/strict";
import { createShim } from "../../../src/shim.ts";

// Musical Lesson (Bard/Clown) and Dancing Lesson (Dancer/Gypsy) are job-bank
// passives that boost the performer's OWN weapon damage. rocalc gates them by
// weapon class: Musical Lesson only helps Instruments, Dancing Lesson only
// Whips. Each test pairs a positive (matching weapon -> damage.ave rises) with
// a negative (a bow -> no change), the weapon-gate lock used for Gunslinger
// gatling_fever and Paladin spear_quicken.
const VIOLIN = 1901; // Instrument
const ROPE = 1950; // Whip
const BOW = 1701;

function shim(cls: string, weaponId: number) {
  const s = createShim();
  s.setClass(cls);
  s.setLevel({ base: 99, job: 70 });
  s.setStats({ str: 80, agi: 70, vit: 30, int: 30, dex: 70, luk: 20 });
  s.equip("weapon", { id: weaponId });
  return s;
}
function aveWith(
  cls: string,
  weaponId: number,
  buffs: { name: string; level: number }[],
): number {
  const s = shim(cls, weaponId);
  if (buffs.length) s.setBuffs(buffs);
  const ave = s.readCombatResults().damage.ave;
  assert.ok(ave != null, "damage.ave must be numeric");
  return ave as number;
}

test("musical_lesson raises damage.ave with an Instrument", () => {
  const base = aveWith("clown", VIOLIN, []);
  const got = aveWith("clown", VIOLIN, [{ name: "musical_lesson", level: 10 }]);
  assert.ok(
    got > base,
    `musical_lesson should raise ave with an instrument (${base} -> ${got})`,
  );
});

test("musical_lesson does NOT raise damage.ave with a bow (instrument-gated)", () => {
  const base = aveWith("clown", BOW, []);
  const got = aveWith("clown", BOW, [{ name: "musical_lesson", level: 10 }]);
  assert.equal(
    got,
    base,
    "musical_lesson must not raise ave outside Instruments",
  );
});

test("dancing_lesson raises damage.ave with a Whip", () => {
  const base = aveWith("gypsy", ROPE, []);
  const got = aveWith("gypsy", ROPE, [{ name: "dancing_lesson", level: 10 }]);
  assert.ok(
    got > base,
    `dancing_lesson should raise ave with a whip (${base} -> ${got})`,
  );
});

test("dancing_lesson does NOT raise damage.ave with a bow (whip-gated)", () => {
  const base = aveWith("gypsy", BOW, []);
  const got = aveWith("gypsy", BOW, [{ name: "dancing_lesson", level: 10 }]);
  assert.equal(got, base, "dancing_lesson must not raise ave outside Whips");
});
