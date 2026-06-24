// Backend registry. Each backend exports createShim and CALC_VERSION
// (the ShimSession factory + version stamp); the registry table maps a
// short name to its module specifier and the env var CALC_BACKEND selects
// which one is active.
//
// Selective loading is load-bearing: when CALC_BACKEND=stub, the rocalc
// module is never loaded and never tries to read vendor/. This is what lets
// CI run against the stub on a checkout with an empty vendor.
//
// We load the selected backend with a SYNCHRONOUS require() rather than a
// dynamic import(), specifically so this module, and therefore a worker
// thread's entire module graph, contains NO top-level await. A worker whose
// module graph is async can abort the process with a V8 fatal ("Check failed:
// (location_) != nullptr", in AsyncModuleExecutionFulfilled) when it is
// terminated, or calls process.exit(), while that async graph is still
// evaluating. Those are unfixed, TLA-specific upstream Node bugs
// (nodejs/node#53238, #43182). ShimPool spawns short-lived workers and
// terminates them eagerly, so the window was hit intermittently, only under
// the fast stub backend, where one pool's teardown overlaps the next pool's
// spawn. require() keeps the selective-load property AND keeps createShim
// synchronous, while making this module's evaluation fully synchronous.
//
// Adding a backend: drop a dir under ./<name>/, export createShim and
// CALC_VERSION, add one entry to BACKENDS below.

import { createRequire } from "node:module";
import type { ShimSession } from "../types.ts";

type BackendModule = {
  createShim: () => ShimSession;
  CALC_VERSION: string;
};

const BACKENDS: Record<string, string> = {
  rocalc: "./rocalc/index.ts",
  stub: "./stub/index.ts",
};

const name = process.env.CALC_BACKEND ?? "rocalc";
const spec = BACKENDS[name];
if (!spec) {
  throw new Error(
    `unknown CALC_BACKEND: ${name} (valid: ${Object.keys(BACKENDS).join(", ")})`,
  );
}

const backend = createRequire(import.meta.url)(spec) as BackendModule;
export const createShim = backend.createShim;
export const CALC_VERSION = backend.CALC_VERSION;
