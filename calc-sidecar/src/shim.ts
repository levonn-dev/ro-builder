// Public entry point for the calc shim.
//
// The shim is the contract the Go orchestrator drives over HTTP. It's
// intentionally engine-agnostic; different calculators can be plugged in
// by writing a new backend under src/backends/<engine>/ that exports a
// createShim() returning the ShimSession interface, then switching the
// re-export below. Each backend translates the generic contract into pokes
// at its own form, vendor files, and recompute functions.
//
// vendor/<engine>/ stays at the top level (outside src/) so users can drop
// files there without touching our code. Engine-specific HTML (e.g. the
// synthetic form-template that mirrors rocalc's <form> shape) lives next to
// the backend that uses it.
//
// The full contract; request/response shapes, slot keys, derived-stat
// fields, combat-result fields; is defined in ./types.ts. This file just
// names the active backend.

export type {
  SlotKey,
  Stats,
  Level,
  EquipSpec,
  SkillAlloc,
  EnemyStats,
  DerivedStats,
  CombatResults,
  ShimSession,
  ScoreRequest,
  ScoreResponse,
} from "./types.ts";

export { createShim, CALC_VERSION } from "./backends/rocalc/index.ts";
