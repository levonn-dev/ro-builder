// Public entry point for the calc shim.
//
// The shim is the contract the Go orchestrator drives over HTTP. It's
// intentionally engine-agnostic; different calculators are plugged in by
// writing a new backend under src/backends/<engine>/ that exports
// createShim() and CALC_VERSION, then adding one entry to the BACKENDS
// table in backends/registry.ts. The active backend is selected at
// runtime via the CALC_BACKEND env var (default: rocalc).
//
// vendor/<engine>/ stays at the top level (outside src/) so users can
// drop files there without touching our code. The vendor/ dir itself is
// tracked (via .gitkeep) but its contents are gitignored.
//
// The full contract (request/response shapes, slot keys, derived-stat
// fields, combat-result fields) is defined in ./types.ts. This file
// just re-exports the active backend's factory.

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

export { createShim, CALC_VERSION } from "./backends/registry.ts";
