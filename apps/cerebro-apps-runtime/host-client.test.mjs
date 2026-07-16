import assert from "node:assert/strict";
import test from "node:test";

import { createHostClient } from "./host-client.mjs";

test("keeps invocation grants in private authenticated host calls", async () => {
  const requests = [];
  const host = createHostClient({
    baseUrl: "http://backend.internal:8080",
    grantToken: "invocation-grant",
    fetch: async (url, init) => (requests.push({ url: String(url), init }), Response.json({ value: "ok" })),
  });
  assert.deepEqual(await host.registryCall("read", "products", { parameters: { sku: "1" } }), { value: "ok" });
  assert.deepEqual(await host.connectionCall("11111111-1111-4111-8111-111111111111", "search", { q: "milk" }), { value: "ok" });
  assert.equal(requests[0].url, "http://backend.internal:8080/api/cerebro/apps-internal/host/registry");
  assert.equal(requests[1].url, "http://backend.internal:8080/api/cerebro/apps-internal/host/connection");
  assert.equal(requests[0].init.headers.authorization, "Bearer invocation-grant");
});
