import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { createShim } from "../../src/shim.ts";

const OVERLAY = join(
  import.meta.dirname,
  "../../../internal/catalog/data/attack_skills.yaml",
);

function overlayNames(): string[] {
  const names = new Set<string>();
  for (const raw of readFileSync(OVERLAY, "utf8").split("\n")) {
    const hash = raw.indexOf("#");
    const line = hash === -1 ? raw : raw.slice(0, hash);
    const m = line.match(/[\s{,]name:\s*([a-z][a-z0-9_]*)/);
    if (m) names.add(m[1]);
  }
  return [...names];
}

test("every attack_skills overlay name has a binding in the active backend", () => {
  const names = overlayNames();
  assert.ok(names.length > 0, "overlay scan found no names; check regex/paths");
  const supported = createShim().supportedAttackSkills();
  if (supported === null) return; // stub
  const set = new Set(supported);
  const missing = names.filter((n) => !set.has(n));
  assert.deepEqual(
    missing,
    [],
    `overlay attack skill(s) with no binding: ${missing.join(", ")}`,
  );
});
