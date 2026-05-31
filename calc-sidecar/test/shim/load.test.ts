import { test } from "node:test";
import { createShim } from "../../src/shim.ts";

// Step 2 deliverable: rocalc loads cleanly in headless Node. createShim does
// the full jsdom + 7-file load + DOM compat wiring; if any of those fail it
// throws synchronously. This test is the public smoke check for the harness.
test("createShim loads rocalc without throwing", () => {
  createShim();
});
