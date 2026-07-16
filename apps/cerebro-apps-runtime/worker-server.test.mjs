import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import test from "node:test";

import { createWorkerRuntime } from "./worker-server.mjs";

const file = (path, content) => ({
  path,
  media_type: "text/javascript",
  content_base64: Buffer.from(content).toString("base64"),
  sha256: createHash("sha256").update(content).digest("hex"),
});

const files = [
  file("app.json", JSON.stringify({ manifest: { schema_version: "1", name: "Test", version: "1.0.0", backend: { entry: "backend/index.mjs" }, frontend: { entry: "frontend/index.html" } } })),
  file("frontend/index.html", "<h1>Test</h1>"),
  file("backend/index.mjs", `export default ({ value }) => ({ value: value.toUpperCase(), secret: typeof process })`),
];

test("loads one immutable bundle and only exposes health and authenticated invoke", async () => {
  const runtime = await createWorkerRuntime({
    appId: "f1540000-0000-4154-8154-000000000001",
    version: "1.0.0",
    bundleUrl: "http://backend.internal/bundle",
    bundleToken: "bundle-token",
    workerCommit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    invokeKey: "invoke-key",
    fetch: async (_url, init) => {
      assert.equal(init.headers.authorization, "Bearer bundle-token");
      return Response.json({ files });
    },
  });

  const health = await runtime.fetch(new Request("http://worker/healthz"));
  assert.equal(health.status, 200);
  assert.equal((await health.json()).commit, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");
  assert.equal((await runtime.fetch(new Request("http://worker/other"))).status, 404);
  assert.equal((await runtime.fetch(new Request("http://worker/invoke", { method: "POST", body: "{}" }))).status, 401);
  const invoked = await runtime.fetch(new Request("http://worker/invoke", {
    method: "POST",
    headers: { "content-type": "application/json", "x-multica-invoke-key": "invoke-key" },
    body: JSON.stringify({ value: "milk" }),
  }));
  assert.equal(invoked.status, 200);
  assert.deepEqual(await invoked.json(), { value: "MILK", secret: "undefined" });
});

test("loads backend relative modules from the verified bundle", async () => {
  const modularFiles = [
    ...files.map((entry) => entry.path === "backend/index.mjs"
      ? file(entry.path, `import { format } from "./format.mjs"; export default ({ value }) => ({ value: format(value) })`)
      : entry),
    file("backend/format.mjs", `export const format = (value) => value.toUpperCase()`),
  ];
  const runtime = await createWorkerRuntime({
    appId: "f1540000-0000-4154-8154-000000000001",
    version: "1.0.0",
    bundleUrl: "http://backend.internal/bundle",
    bundleToken: "token",
    invokeKey: "key",
    fetch: async () => Response.json({ files: modularFiles }),
  });
  const response = await runtime.fetch(new Request("http://worker/invoke", {
    method: "POST",
    headers: { "x-multica-invoke-key": "key" },
    body: JSON.stringify({ value: "milk" }),
  }));
  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), { value: "MILK" });
});

test("rejects a bundle whose file hash changed", async () => {
  const tampered = files.map((entry) => ({ ...entry }));
  tampered[2].content_base64 = Buffer.from("changed").toString("base64");
  await assert.rejects(createWorkerRuntime({
    appId: "f1540000-0000-4154-8154-000000000001",
    version: "1.0.0",
    bundleUrl: "http://backend.internal/bundle",
    bundleToken: "token",
    invokeKey: "key",
    fetch: async () => Response.json({ files: tampered }),
  }), /App bundle failed validation/);
});

test("rejects a bundle that does not match the published bundle hash", async () => {
  await assert.rejects(createWorkerRuntime({
    appId: "f1540000-0000-4154-8154-000000000001",
    version: "1.0.0",
    bundleUrl: "http://backend.internal/bundle",
    bundleToken: "token",
    expectedBundleSha256: "0".repeat(64),
    invokeKey: "key",
    fetch: async () => Response.json({ files }),
  }), /App bundle failed validation/);
});

test("rejects unsafe paths and oversized files before sandbox startup", async () => {
  for (const extra of [file("../secret", "x"), file("backend/large.bin", "x".repeat((512 << 10) + 1))]) {
    await assert.rejects(createWorkerRuntime({
      appId: "f1540000-0000-4154-8154-000000000001",
      version: "1.0.0",
      bundleUrl: "http://backend.internal/bundle",
      bundleToken: "token",
      invokeKey: "key",
      fetch: async () => Response.json({ files: [...files, extra] }),
    }), /App bundle failed validation/);
  }
});

test("unwraps a private invocation grant and exposes only allowlisted host calls", async () => {
  const hostedFiles = files.map((entry) => entry.path === "backend/index.mjs"
    ? file(entry.path, `export default async (input, multica) => ({ input, registry: await multica.registry.call("read", "products", {}), connection: await multica.connections.call("connection-1", "search", {}) })`)
    : entry);
  let grant;
  const runtime = await createWorkerRuntime({
    appId: "f1540000-0000-4154-8154-000000000001",
    version: "1.0.0",
    bundleUrl: "http://backend.internal/bundle",
    bundleToken: "token",
    invokeKey: "key",
    fetch: async () => Response.json({ files: hostedFiles }),
    hostFactory: (token) => {
      grant = token;
      return { registryCall: async () => "registry-ok", connectionCall: async () => "connection-ok" };
    },
  });
  const response = await runtime.fetch(new Request("http://worker/invoke", {
    method: "POST",
    headers: { "x-multica-invoke-key": "key" },
    body: JSON.stringify({ input: { sku: "1" }, grant_token: "opaque-grant" }),
  }));
  assert.equal(response.status, 200);
  assert.equal(grant, "opaque-grant");
  assert.deepEqual(await response.json(), { input: { sku: "1" }, registry: "registry-ok", connection: "connection-ok" });
});
