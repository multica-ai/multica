import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { createAppsRuntime } from "./runtime.mjs";
import { signServiceRequest } from "./auth.mjs";

async function fixture() {
  const root = await mkdtemp(join(tmpdir(), "multica-app-runtime-"));
  const version = join(root, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "1.0.0");
  await mkdir(join(version, "frontend"), { recursive: true });
  await mkdir(join(version, "backend"), { recursive: true });
  await writeFile(join(version, "frontend", "index.html"), "<h1>Allergen Formatter</h1>");
  await writeFile(
    join(version, "backend", "index.mjs"),
    `export default async (input) => ({ formatted: String(input.value).toUpperCase(), leaked: process.env.FIRTAL_REGISTRY_KEY ?? null, grantLeaked: input.grant_token ?? null });`,
  );
  return root;
}

test("serves an immutable app frontend version", async () => {
  const root = await fixture();
  const runtime = createAppsRuntime({ bundleRoot: root, frameAncestors: "https://multica.example", workerIsolation: "process" });
  const response = await runtime.fetch(
    new Request("http://runtime/apps/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/1.0.0/"),
  );
  assert.equal(response.status, 200);
  assert.match(await response.text(), /Allergen Formatter/);
  assert.equal(response.headers.get("cache-control"), "public, max-age=31536000, immutable");
  assert.match(response.headers.get("content-security-policy"), /frame-ancestors https:\/\/multica\.example/);
});

test("serves a validated published frontend without a shared filesystem", async () => {
  const requests = [];
  const runtime = createAppsRuntime({
    bundleStore: { get: async (...args) => (requests.push(args), { content: Buffer.from("<h1>Published</h1>"), mediaType: "text/html" }) },
    workerIsolation: "process",
  });
  const response = await runtime.fetch(new Request("http://runtime/apps/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/1.0.0/"));
  assert.equal(response.status, 200);
  assert.equal(await response.text(), "<h1>Published</h1>");
  assert.deepEqual(requests, [["aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "1.0.0", "frontend/index.html"]]);
});

test("serves the mini-app SDK with a real connection call route", async () => {
  const runtime = createAppsRuntime({ bundleRoot: await fixture(), workerIsolation: "process" });
  const response = await runtime.fetch(new Request("http://runtime/sdk/multica.js"));
  assert.equal(response.status, 200);
  const source = await response.text();
  assert.match(source, /multica\.app-sdk\.request/);
  assert.match(source, /requestId/);
  assert.doesNotMatch(source, /fetch\(/);
  assert.match(source, /storage:/);
  assert.match(source, /views:/);
});

test("isolates backend execution and strips registry system credentials", async () => {
  const root = await fixture();
  process.env.FIRTAL_REGISTRY_KEY = "rk_must_not_cross_worker_boundary";
  const runtime = createAppsRuntime({ bundleRoot: root, workerTimeoutMs: 2_000, workerIsolation: "process" });
  const response = await runtime.fetch(
    new Request("http://runtime/workers/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/1.0.0/invoke", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ value: "milk" }),
    }),
  );
  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), { formatted: "MILK", leaked: null, grantLeaked: null });
});

test("unwraps invocation input without exposing the private grant to app code", async () => {
  const runtime = createAppsRuntime({ bundleRoot: await fixture(), workerIsolation: "process" });
  const response = await runtime.fetch(
    new Request("http://runtime/workers/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/1.0.0/invoke", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ input: { value: "milk" }, grant_token: "private-invocation-grant" }),
    }),
  );
  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), { formatted: "MILK", leaked: null, grantLeaked: null });
});

test("one failed worker does not affect runtime health", async () => {
  const root = await fixture();
  const runtime = createAppsRuntime({ bundleRoot: root, workerIsolation: "process" });
  const missing = await runtime.fetch(
    new Request("http://runtime/workers/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb/1.0.0/invoke", { method: "POST", body: "{}" }),
  );
  assert.equal(missing.status, 502);
  assert.deepEqual(await missing.json(), { error: "App worker failed" });
  const health = await runtime.fetch(new Request("http://runtime/healthz"));
  assert.equal(health.status, 200);
  assert.deepEqual(await health.json(), { status: "ok" });
});

test("rejects path traversal before touching the filesystem", async () => {
  const runtime = createAppsRuntime({ bundleRoot: await fixture(), workerIsolation: "process" });
  const response = await runtime.fetch(new Request("http://runtime/apps/not-an-id/1.0.0/../../secret"));
  assert.equal(response.status, 404);
});

test("allergen fixture makes one AI call on the personal key", async () => {
  let calls = 0;
  const ai = (await import("node:http")).createServer((req, res) => {
    calls++;
    assert.equal(req.headers.authorization, "Bearer sk_personal");
    res.setHeader("content-type", "application/json");
    res.end(JSON.stringify({ choices: [{ message: { content: JSON.stringify({ formatted_ingredients: "WHEAT flour, MILK", allergens: ["WHEAT", "MILK"] }) } }] }));
  });
  await new Promise((resolve) => ai.listen(0, "127.0.0.1", resolve));
  const address = ai.address();
  const fixtureRoot = join(fileURLToPath(new URL(".", import.meta.url)), "fixtures");
  const runtime = createAppsRuntime({ bundleRoot: fixtureRoot, workerTimeoutMs: 2_000, workerIsolation: "process" });
  const response = await runtime.fetch(new Request("http://runtime/workers/f1540000-0000-4154-8154-000000000001/1.0.0/invoke", {
    method: "POST",
    body: JSON.stringify({ ingredients: "wheat flour, milk", registryKey: "sk_personal", aiBaseUrl: `http://127.0.0.1:${address.port}` }),
  }));
  ai.close();
  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), { formatted_ingredients: "WHEAT flour, MILK", allergens: ["WHEAT", "MILK"] });
  assert.equal(calls, 1);
});

test("allergen fixture loads a CSP-safe client that obtains a personal token before invoking its worker", async () => {
  const fixtureRoot = join(fileURLToPath(new URL(".", import.meta.url)), "fixtures");
  const runtime = createAppsRuntime({ bundleRoot: fixtureRoot, workerIsolation: "process" });
  const page = await runtime.fetch(new Request("http://runtime/apps/f1540000-0000-4154-8154-000000000001/1.0.0/"));
  const html = await page.text();
  assert.match(html, /<script type="module" src="\.\/app\.js"><\/script>/);
  assert.doesNotMatch(html, /<script>[^<]/);

  const script = await runtime.fetch(new Request("http://runtime/apps/f1540000-0000-4154-8154-000000000001/1.0.0/app.js"));
  const source = await script.text();
  assert.match(source, /createMulticaApp/);
  assert.match(source, /registryKey:\s*token\.key/);
  assert.match(source, /aiBaseUrl:\s*token\.ai_base_url/);
  // An app must never pin itself to one environment: the gateway it may call is
  // whatever the token broker handed out, so staging and local can never leak
  // traffic to the production registry.
  assert.doesNotMatch(source, /https?:\/\/[^"']*registry/);
});

test("accepts only signed deployment requests and dispatches them once", async () => {
  const deployments = [];
  const runtime = createAppsRuntime({
    bundleRoot: await fixture(),
    workerIsolation: "process",
    runtimeServiceKey: "service-secret",
    deploymentManager: { deploy: async (value) => deployments.push(value) },
  });
  const body = Buffer.from(JSON.stringify({ app_id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", version: "1.0.0", bundle_sha256: "a".repeat(64) }));
  const unsigned = await runtime.fetch(new Request("http://runtime/deployments", { method: "POST", body }));
  assert.equal(unsigned.status, 401);
  const signed = signServiceRequest("service-secret", "POST", "/deployments", body);
  const accepted = await runtime.fetch(new Request("http://runtime/deployments", {
    method: "POST",
    headers: { "x-multica-timestamp": signed.timestamp, "x-multica-signature": signed.signature },
    body,
  }));
  assert.equal(accepted.status, 202);
  await new Promise((resolve) => setImmediate(resolve));
  assert.deepEqual(deployments, [{ appId: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", version: "1.0.0", bundleSha256: "a".repeat(64) }]);
});

test("accepts only signed pause and delete lifecycle requests", async () => {
  const calls = [];
  const runtime = createAppsRuntime({
    bundleRoot: await fixture(),
    workerIsolation: "process",
    runtimeServiceKey: "service-secret",
    deploymentManager: { lifecycle: async (...args) => calls.push(args) },
  });
  const body = Buffer.from('{"service_id":"service-123"}');
  const unsigned = await runtime.fetch(new Request("http://runtime/lifecycle/pause", { method: "POST", body }));
  assert.equal(unsigned.status, 401);
  for (const action of ["pause", "delete"]) {
    const path = `/lifecycle/${action}`;
    const signed = signServiceRequest("service-secret", "POST", path, body);
    const response = await runtime.fetch(new Request(`http://runtime${path}`, {
      method: "POST",
      headers: { "x-multica-timestamp": signed.timestamp, "x-multica-signature": signed.signature },
      body,
    }));
    assert.equal(response.status, 204);
  }
  assert.deepEqual(calls, [["pause", "service-123"], ["delete", "service-123"]]);
});

test("proxies a signed invoke to the concrete private worker domain", async () => {
  const proxied = [];
  const runtime = createAppsRuntime({
    bundleRoot: await fixture(),
    workerIsolation: "process",
    runtimeServiceKey: "service-secret",
    backendClient: { deployment: async () => ({ internalDomain: "worker-ab12.internal", invokeKey: "invoke-key" }) },
    fetch: async (url, init) => {
      proxied.push({ url: String(url), init });
      return Response.json({ value: "MILK" });
    },
  });
  const path = "/workers/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/1.0.0/invoke";
  const body = Buffer.from('{"value":"milk"}');
  const signed = signServiceRequest("service-secret", "POST", path, body);
  const response = await runtime.fetch(new Request(`http://runtime${path}`, {
    method: "POST",
    headers: { "x-multica-timestamp": signed.timestamp, "x-multica-signature": signed.signature },
    body,
  }));
  assert.equal(response.status, 200);
  assert.equal(proxied[0].url, "http://worker-ab12.internal:4311/invoke");
  assert.equal(proxied[0].init.headers["x-multica-invoke-key"], "invoke-key");
});
