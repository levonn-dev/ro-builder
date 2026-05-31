import { test, describe, before, after } from "node:test";
import assert from "node:assert/strict";
import type { AddressInfo } from "node:net";
import type { Server } from "node:http";
import { createServer } from "../../../server.ts";

// HTTP integration tests for rocalc-specific behavior: equipment
// validation rules and iRO-id mapping. These tests assume the rocalc
// backend's option-list checks and mapping.json are active; running
// them against another backend (stub) would not exercise the same code
// path.

describe("POST /score HTTP integration (rocalc-specific)", () => {
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
});
