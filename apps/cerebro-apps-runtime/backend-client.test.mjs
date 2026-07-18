import assert from "node:assert/strict";
import test from "node:test";

import { BackendClient } from "./backend-client.mjs";

test("builds a scoped deployment input without exposing the runtime service key", async () => {
  const client = new BackendClient({ baseUrl: "http://backend.internal:8080", serviceKey: "runtime-secret", fetch: async () => Response.json([]) });
  const input = await client.deploymentInput({ appId: "f1540000-0000-4154-8154-000000000001", version: "1.0.0", bundleSha256: "a".repeat(64) });
  assert.match(input.bundleUrl, /apps-internal\/f1540000-0000-4154-8154-000000000001\/1\.0\.0\/bundle$/);
  assert.ok(input.bundleToken);
  assert.ok(input.invokeKey);
  assert.equal(JSON.stringify(input).includes("runtime-secret"), false);
});

test("signs callbacks and reads pending deployments", async () => {
  const requests = [];
  const client = new BackendClient({
    baseUrl: "http://backend.internal:8080",
    serviceKey: "secret",
    fetch: async (url, init = {}) => {
      requests.push({ url: String(url), init });
      if (init.method === "POST") return Response.json({ status: "ready" });
      return Response.json([{ app_id: "a", app_name: "Allergen Formatter", version: "1.0.0", bundle_sha256: "b" }]);
    },
  });
  await client.callback("a", "1.0.0", { status: "ready" });
  assert.match(requests[0].init.headers["x-multica-signature"], /^sha256=/);
  assert.ok(requests[0].init.headers["x-multica-timestamp"]);
  assert.deepEqual(await client.pending(), [{ appId: "a", appName: "Allergen Formatter", version: "1.0.0", bundleSha256: "b" }]);
});

test("resolves the concrete ready internal domain instead of guessing a service name", async () => {
  const client = new BackendClient({
    baseUrl: "http://backend.internal:8080",
    serviceKey: "secret",
    fetch: async (_url, init) => {
      assert.match(init.headers["x-multica-signature"], /^sha256=/);
      return Response.json({ external_service_id: "service-1", internal_domain: "cerebro-app-f1540000-ab12cd.internal" });
    },
  });
  assert.deepEqual(await client.deployment("f1540000-0000-4154-8154-000000000001", "1.0.0"), {
    serviceId: "service-1",
    internalDomain: "cerebro-app-f1540000-ab12cd.internal",
    invokeKey: client.invokeKey("f1540000-0000-4154-8154-000000000001", "1.0.0"),
  });
});

test("downloads bundles over a signed service request", async () => {
  let request;
  const client = new BackendClient({ baseUrl: "http://backend", serviceKey: "secret", fetch: async (url, init) => {
    request = { url, init };
    return Response.json({ files: [] });
  } });
  assert.deepEqual(await client.bundle("f1540000-0000-4154-8154-000000000001", "1.0.0"), { files: [] });
  assert.equal(request.url, "http://backend/api/cerebro/apps-internal/f1540000-0000-4154-8154-000000000001/1.0.0/bundle");
  assert.match(request.init.headers["x-multica-signature"], /^sha256=[0-9a-f]{64}$/);
});
