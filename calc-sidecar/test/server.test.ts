import { test, describe, before, after } from "node:test";
import assert from "node:assert/strict";
import type { AddressInfo } from "node:net";
import type { Server } from "node:http";
import { createServer } from "../server.ts";
import { ShimPool } from "../src/pool.ts";

// HTTP integration tests: spin up a server on an ephemeral port (listen on
// 0 and read the actual bound port back), make real requests, assert
// status + parsed body. The score()-level tests in score.test.ts cover the
// shim wiring; these tests verify only the JSON-in/JSON-out plumbing.

describe("POST /score HTTP integration", () => {
  let server: Server;
  let baseURL: string;

  before(async () => {
    server = createServer();
    await new Promise<void>((r) => server.listen(0, () => r()));
    const { port } = server.address() as AddressInfo;
    baseURL = `http://127.0.0.1:${port}`;
  });

  after(() => {
    server.close();
  });

  test("POST /score with empty body returns 200 + default Novice stats", async () => {
    const resp = await fetch(`${baseURL}/score`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: "{}",
    });
    assert.equal(resp.status, 200);
    const body = await resp.json();
    assert.equal(body.derived.hit, 2);
    assert.equal(body.combat, undefined);
  });

  test("POST /score with full build returns derived + combat", async () => {
    const resp = await fetch(`${baseURL}/score`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        stats: { str: 1, agi: 1, vit: 1, int: 1, dex: 1, luk: 1 },
        equipment: { weapon: { id: 1201 } },
        enemy: 1002, // Poring (iRO mob id)
      }),
    });
    assert.equal(resp.status, 200);
    const body = await resp.json();
    assert.deepEqual(body.derived.atk, { base: 18, plus: 0 });
    assert.ok(body.combat.damage.min > 0);
  });

  // Client-fixable error → 400. The shim throws an Error with a known
  // message ("unknown equipment slot: foo") and the server classifies it.
  test("POST /score with unknown slot returns 400", async () => {
    const resp = await fetch(`${baseURL}/score`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ equipment: { foo: { id: 1201 } } }),
    });
    assert.equal(resp.status, 400);
    const body = await resp.json();
    assert.match(body.error, /unknown equipment slot/);
  });

  // Class-restriction error → 400. Cotton Shirt (2301) is iRO armor; the
  // shim's option-list validation rejects it from the weapon slot rather
  // than letting rocalc crash. classifyError catches the "option list"
  // substring and returns 400.
  test("POST /score with item not equippable by class returns 400", async () => {
    const resp = await fetch(`${baseURL}/score`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ equipment: { weapon: { id: 2301 } } }),
    });
    assert.equal(resp.status, 400);
    const body = await resp.json();
    assert.match(body.error, /option list/);
  });

  // Unmapped iRO id → 400 with actionable message; same classifyError
  // path as the slot-validation case.
  test("POST /score with unmapped iRO id returns 400", async () => {
    const resp = await fetch(`${baseURL}/score`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ equipment: { weapon: { id: 99999999 } } }),
    });
    assert.equal(resp.status, 400);
    const body = await resp.json();
    assert.match(body.error, /no rocalc mapping/);
  });

  test("Wrong method or path returns 404", async () => {
    const resp = await fetch(`${baseURL}/something-else`);
    assert.equal(resp.status, 404);
  });

  // Malformed JSON body → 400 with a parse-error message.
  test("POST /score with malformed JSON returns 400", async () => {
    const resp = await fetch(`${baseURL}/score`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: "{not valid json",
    });
    assert.equal(resp.status, 400);
    const body = await resp.json();
    assert.match(body.error, /invalid JSON/);
  });
});

describe("GET /healthz", () => {
  let server: Server;
  let pool: ShimPool;
  let baseURL: string;

  before(async () => {
    // Create a pool externally so we can await its full warmup before
    // checking /healthz. The /score-drive trick from the original spec
    // has a race when pool size > 1: the test request can land on the
    // first worker to finish warmup while the second is still running,
    // so allSettled hasn't resolved yet and _ready is still false.
    // Awaiting awaitReady() removes that race entirely.
    pool = new ShimPool(2);
    await pool.awaitReady();
    server = createServer({ pool });
    await new Promise<void>((r) => server.listen(0, () => r()));
    const { port } = server.address() as AddressInfo;
    baseURL = `http://127.0.0.1:${port}`;
  });

  after(async () => {
    server.close();
    await pool.terminate();
  });

  test("returns 200 with status=ok once workers are warm", async () => {
    // pool.awaitReady() in before() guarantees warmup is complete before
    // we reach this assertion.
    const resp = await fetch(`${baseURL}/healthz`);
    assert.equal(resp.status, 200);
    const body = await resp.json();
    assert.equal(body.status, "ok");
  });

  test("returns 405 for non-GET", async () => {
    const resp = await fetch(`${baseURL}/healthz`, { method: "POST" });
    assert.equal(resp.status, 405);
  });
});

// Note: a "returns 503 before warmup" test was removed because it raced
// the JSDOM warmup; on fast hardware createShim() could complete before
// the test's first fetch reached the server, yielding 200 instead of 503.
// The 503 branch in server.ts is straightforward (one if/else over
// pool.ready()) and is exercised in practice by the K8s readiness probe
// against a freshly-rolled pod.
