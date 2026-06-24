import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { createShim } from "../../src/shim.ts";

// Parity guard: every self-buff the Go side can declare (the catalog overlays)
// must have a binding in the active calc backend, else the buff silently does
// nothing and surfaces only at score time. This references ONLY the shim
// contract (ShimSession.supportedBuffs) -- never a backend's internal binding
// table -- so it stays correct across backends. Backends that do not enforce
// buff names (supportedBuffs() === null, e.g. the stub) make the check vacuous.

// The overlays live in the Go catalog tree; they are the shared source of truth
// for buff names across the Go/sidecar boundary. class_innate_buffs.yaml carries
// class-mechanic buffs (e.g. Super Novice no_death_bonus) that also flow through
// setBuffs and therefore need a binding too.
const OVERLAY_DIR = join(import.meta.dirname, "../../../internal/catalog/data");
const OVERLAYS = ["skill_buffs.yaml", "class_innate_buffs.yaml"];

// Buff names are the lowercase_snake value of a `name:` key inside a self_buff
// or class-innate entry. Scanned without a YAML-parser dependency: per line,
// drop any `#` comment, then match `name:`. The leading [\s{,] excludes
// `aegis_name:` (the char before "name:" there is "_"), and the lowercase-only
// value class excludes the uppercase aegis names.
function overlayBuffNames(): string[] {
  const names = new Set<string>();
  for (const file of OVERLAYS) {
    const text = readFileSync(join(OVERLAY_DIR, file), "utf8");
    for (const raw of text.split("\n")) {
      const hash = raw.indexOf("#");
      const line = hash === -1 ? raw : raw.slice(0, hash);
      const m = line.match(/[\s{,]name:\s*([a-z][a-z0-9_]*)/);
      if (m) names.add(m[1]);
    }
  }
  return [...names];
}

test("every overlay buff name has a binding in the active backend", () => {
  const names = overlayBuffNames();
  assert.ok(
    names.length > 0,
    "overlay scan found no buff names; check the regex/paths",
  );

  const supported = createShim().supportedBuffs();
  if (supported === null) return; // backend does not enforce buff names (stub)

  const supportedSet = new Set(supported);
  const missing = names.filter((n) => !supportedSet.has(n));
  assert.deepEqual(
    missing,
    [],
    `overlay buff(s) with no binding in the active backend: ${missing.join(", ")}`,
  );
});
