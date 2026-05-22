// Diagnostic; runs setClass for every job id 0..45 in a fresh shim per
// class. Reports which work, which throw, and the exact error/location.
//
// Run: node calc-sidecar/test/class-coverage.ts
//
// Not part of the regular test suite; spins ~46 fresh shims (~70s
// total). Used to drive form-template fixes; re-run after editing
// form-template.html to verify progress.

import { createShim } from "../src/shim.ts";

interface Result {
  id: number;
  ok: boolean;
  error?: string;
  stack?: string;
  hp?: number;
}

const results: Result[] = [];

for (let id = 0; id <= 45; id++) {
  const r: Result = { id, ok: false };
  try {
    const shim = createShim();
    shim.setClass(String(id));
    const derived = shim.readDerivedStats();
    r.ok = true;
    r.hp = derived.maxHp;
  } catch (e) {
    r.error = e instanceof Error ? e.message : String(e);
    r.stack = e instanceof Error ? (e.stack ?? "") : "";
  }
  results.push(r);
}

const ok = results.filter((r) => r.ok);
const fail = results.filter((r) => !r.ok);

console.log(`\n=== Coverage: ${ok.length}/46 classes working ===`);
console.log("working ids:", ok.map((r) => r.id).join(", "));

console.log("\n=== Per-failure detail ===");
for (const r of fail) {
  console.log(`\n[id ${r.id}] ${r.error}`);
  if (r.stack) {
    // Trim stack to the first 4 frames; enough to localize the issue
    const lines = r.stack.split("\n").slice(0, 5);
    for (const l of lines) console.log(`    ${l.trim()}`);
  }
}

console.log(`\n=== Summary ===`);
console.log(`  working: ${ok.length}/46`);
console.log(`  broken:  ${fail.length}/46`);
