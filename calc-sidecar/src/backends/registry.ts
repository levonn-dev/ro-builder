// Backend registry. Each backend exports createShim and CALC_VERSION
// (the ShimSession factory + version stamp); the registry table maps a
// short name to a dynamic-import factory and the env var CALC_BACKEND
// selects which one is active.
//
// Dynamic import() is load-bearing: when CALC_BACKEND=stub, the rocalc
// module is never imported and never tries to read vendor/. This is
// what lets CI run against the stub on a checkout with an empty vendor.
//
// Adding a backend: drop a dir under ./<name>/, export createShim and
// CALC_VERSION, add one entry to BACKENDS below.

import type { ShimSession } from "../types.ts";

type BackendModule = {
  createShim: () => ShimSession;
  CALC_VERSION: string;
};

const BACKENDS: Record<string, () => Promise<BackendModule>> = {
  rocalc: () => import("./rocalc/index.ts"),
  stub: () => import("./stub/index.ts"),
};

const name = process.env.CALC_BACKEND ?? "rocalc";
const factory = BACKENDS[name];
if (!factory) {
  throw new Error(
    `unknown CALC_BACKEND: ${name} (valid: ${Object.keys(BACKENDS).join(", ")})`,
  );
}

const backend = await factory();
export const createShim = backend.createShim;
export const CALC_VERSION = backend.CALC_VERSION;
