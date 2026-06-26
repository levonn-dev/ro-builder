// score is the sidecar's HTTP-facing entry point in functional form: a
// request object describing a build (class/level/stats/equipment/enemy)
// goes in, a response with derived stats and optional combat results comes
// back. The HTTP layer (server.ts) is a thin JSON adapter on top of this.
//
// Concurrency: score() runs inside a worker thread (src/worker.ts) and
// the ShimPool (src/pool.ts) serializes dispatch per worker; only one
// message-handler tick is in flight per worker at a time. That's where
// the "rocalc state can't interleave" guarantee actually lives.
//
// score() stays fully synchronous as defense-in-depth. The shim holds
// mutable globals on the jsdom window (n_A_STR, m_Item, ...); an
// accidental `await` here wouldn't corrupt anything today thanks to
// the pool's busy-flag, but future maintainers should still keep this
// path sync so the safety net survives a pool refactor.
//
// Class handling: setClass is expensive (~250 ms; ClickJob rebuilds
// option lists) but the shim refreshes its reset() snapshot when
// setClass runs, so reset() afterwards correctly rolls back to the new
// class's baseline. We track the last applied class across requests and
// only call setClass when it actually changes; missing class field in a
// request is treated as Novice (0).

import { createShim, CALC_VERSION } from "./shim.ts";
import { ScoreValidationError } from "./errors.ts";
import type {
  EquipSpec,
  ScoreRequest,
  ScoreResponse,
  ShimSession,
  SlotKey,
} from "./types.ts";

export { ScoreValidationError };

let shim: ShimSession | null = null;
let lastClass = "novice"; // matches the shim's post-createShim default state

function getShim(): ShimSession {
  if (!shim) {
    shim = createShim();
    lastClass = "novice";
  }
  return shim;
}

// resetScoreShim drops the cached shim. Used by tests to prove
// independence across runs and called internally on non-validation errors
// to recover from shim-internal state corruption (see score()).
export function resetScoreShim(): void {
  shim = null;
  lastClass = "novice";
}

// isValidationError returns true for errors marked as caller-fixable;
// the typed ScoreValidationError class or any Error carrying a
// `validation: true` flag (the cross-thread duck-typed form). These
// leave the shim's internal state intact because they're thrown before
// any form / rocalc-internal mutation.
//
// Anything else; TypeError from a missing form element deep inside
// ClickJob, ReferenceError from rocalc reading an uninitialized global;
// indicates a half-completed mutation and the shim is now untrustworthy.
//
// Same predicate drives two decisions:
//   1. server.ts classifyError → 400 vs 500 (caller-fixable vs server bug)
//   2. score() shim disposal → keep the cache vs throw it away
export function isValidationError(err: unknown): boolean {
  if (err instanceof ScoreValidationError) return true;
  return (
    err instanceof Error &&
    (err as { validation?: boolean }).validation === true
  );
}

const SLOT_KEYS = new Set<SlotKey>([
  "weapon",
  "headTop",
  "headMid",
  "headBot",
  "shield",
  "armor",
  "garment",
  "footgear",
  "accessory1",
  "accessory2",
]);

// applyEquipment applies every slot, collecting per-slot validation errors
// instead of throwing on the first. Unusable items (unknown to the calc, or
// not equippable by this class) AND unknown slot keys are all gathered, so a
// build with several bad slots surfaces ALL of them in one response and the
// caller fixes every one in a single resubmit rather than one per round-trip.
//
// Only caller-fixable (validation) errors are collected; a non-validation
// error means the shim's form globals are half-applied (corruption), so it
// re-throws immediately and score()'s catch disposes the shim. Partially
// applied good slots are harmless: score() resets the shim at the start of
// every request, rolling them back.
export function applyEquipment(
  s: Pick<ShimSession, "equip">,
  equipment: Partial<Record<SlotKey, EquipSpec>>,
): void {
  const errors: string[] = [];
  for (const slot of Object.keys(equipment) as SlotKey[]) {
    if (!SLOT_KEYS.has(slot)) {
      // An unknown slot key is caller-fixable too; aggregate it rather than
      // short-circuiting, so a typo'd slot alongside bad items still reports
      // everything at once.
      errors.push(`${slot}: unknown equipment slot`);
      continue;
    }
    const spec = equipment[slot];
    if (!spec) continue;
    try {
      s.equip(slot, spec);
    } catch (e) {
      if (!isValidationError(e)) throw e;
      errors.push(`${slot}: ${(e as Error).message}`);
    }
  }
  if (errors.length > 0) {
    throw new ScoreValidationError(
      `${errors.length} equipment item(s) cannot be used; fix all and resubmit:\n- ${errors.join("\n- ")}`,
    );
  }
}

// rocalcCombatCaveats returns the always-applicable approximations rocalc
// makes in its combat sim; sourced from rocalc.com's own "known issues"
// notes. Documented at the request-shape level (we surface them whenever
// combat sim runs) rather than detected per-build, since rocalc's calc
// path doesn't expose hooks to know whether a given build hit any of them.
function rocalcCombatCaveats(): string[] {
  return [
    "rocalc_lh_crit_damage: left-hand weapon critical damage may be slightly off (Assassin dual-wield)",
    "rocalc_pct_dmg_lh: % damage modifiers from cards/effects do not propagate to left-hand damage",
    "rocalc_skill_approx: Soul Breaker / Shield Boomerang / Shield Chain / Grimtooth damage differs slightly from in-game",
    "rocalc_enemy_skill_approx: Grand Cross / Soul Breaker / Evil Land enemy damage not fully modeled",
  ];
}

export function score(req: ScoreRequest): ScoreResponse {
  const s = getShim();

  try {
    // reset() first so the previous request's stats/equipment/level don't
    // leak into this one. After reset the shim is back at the last
    // applied class's baseline (default Novice for a fresh shim).
    s.reset();

    // Apply class change only if it actually differs from the current
    // session class; setClass is the one expensive mutator and rerunning
    // it for every request would waste ~250 ms per call. Missing or empty
    // class is treated as "novice" so back-to-back requests with and
    // without class roll back correctly.
    const targetClass =
      req.class && req.class.trim() !== "" ? req.class : "novice";
    if (targetClass !== lastClass) {
      s.setClass(targetClass);
      lastClass = targetClass;
    }

    if (req.level) s.setLevel(req.level);
    if (req.stats) s.setStats(req.stats);
    if (req.skills && req.skills.length > 0) s.setSkills(req.skills);
    if (req.equipment) applyEquipment(s, req.equipment);
    if (req.buffs && req.buffs.length > 0) s.setBuffs(req.buffs);
    if (req.attack_skills && req.attack_skills.length > 0)
      s.setAttackSkills(req.attack_skills);

    const out: ScoreResponse = {
      derived: s.readDerivedStats(),
      calc_version: CALC_VERSION,
    };
    // enemy and enemy_inline are mutually exclusive; set one, the
    // other, or neither. Reject explicit both; that's a caller bug.
    if (req.enemy != null && req.enemy_inline != null) {
      throw new ScoreValidationError(
        "enemy and enemy_inline are mutually exclusive; set only one",
      );
    }
    const flags: string[] = [];
    if (req.enemy != null) {
      s.setEnemy(req.enemy);
      out.combat = s.readCombatResults();
    } else if (req.enemy_inline != null) {
      s.setEnemyInline(req.enemy_inline);
      // INVARIANT: readCombatResults must read only the rocalc DOM <td>
      // cells (strID_0..2, BattleTime, B_*, etc.), never the live
      // m_Monster array. setEnemyInline restores m_Monster[SCRATCH_SLOT]
      // back to Poring inside its finally before returning, so any read
      // of m_Monster here would see Poring stats; not the inline mob's.
      // If you extend readCombatResults to expose a field that needs an
      // m_Monster read, capture it inside setEnemyInline before the
      // restore (see the finally-block comment in backends/rocalc).
      out.combat = s.readCombatResults();
      if (req.enemy_inline.level == null || req.enemy_inline.level <= 0) {
        flags.push(
          "inline_mob_level_defaulted_to_1: hit/flee scaling assumes a level-1 target; " +
            "pass enemy_inline.level for accurate per-target rates",
        );
      }
    }
    // Rocalc's documented known issues that affect the numbers in
    // `out`. Surfaced so the LLM and the build library can flag
    // approximate scores rather than treat them as ground truth.
    if (out.combat) {
      flags.push(...rocalcCombatCaveats());
    }
    if (out.combat?.skills) {
      for (const sk of out.combat.skills) {
        if (sk.uncertainty)
          flags.push(`attack_skill_${sk.name}: ${sk.uncertainty}`);
      }
    }
    if (flags.length > 0) {
      out.uncertainty = flags;
    }
    return out;
  } catch (e) {
    // Validation errors (unmapped id, unknown class, etc.) are thrown
    // before any state mutation; the shim is fine, just re-throw.
    // Anything else is rocalc-internal: the shim's form globals may be
    // half-applied and subsequent requests will start tripping on
    // "Cannot read properties of null/undefined" errors that have
    // nothing to do with their own input. Dispose so the next request
    // pays the ~1.5s recreate cost once and gets a clean slate.
    if (!isValidationError(e)) {
      resetScoreShim();
    }
    throw e;
  }
}
