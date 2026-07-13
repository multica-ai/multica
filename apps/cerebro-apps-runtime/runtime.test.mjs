import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { createAppsRuntime } from "./runtime.mjs";

async function fixture() {
  const root = await mkdtemp(join(tmpdir(), "multica-app-runtime-"));
  const version = join(root, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "1.0.0");
  await mkdir(join(version, "frontend"), { recursive: true });
  await mkdir(join(version, "backend"), { recursive: true });
  await writeFile(join(version, "frontend", "index.html"), "<h1>Allergen Formatter</h1>");
  await writeFile(
    join(version, "backend", "index.mjs"),
    `export default async ({ value }) => ({ formatted: String(value).toUpperCase(), leaked: process.env.FIRTAL_REGISTRY_KEY ?? null });`,
  );
  return root;
}

test("serves an immutable app frontend version", async () => {
  const root = await fixture();
  const runtime = createAppsRuntime({ bundleRoot: root, frameAncestors: "https://multica.example" });
  const response = await runtime.fetch(
    new Request("http://runtime/apps/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/1.0.0/"),
  );
  assert.equal(response.status, 200);
  assert.match(await response.text(), /Allergen Formatter/);
  assert.equal(response.headers.get("cache-control"), "public, max-age=31536000, immutable");
  assert.match(response.headers.get("content-security-policy"), /frame-ancestors https:\/\/multica\.example/);
});

test("isolates backend execution and strips registry system credentials", async () => {
  const root = await fixture();
  process.env.FIRTAL_REGISTRY_KEY = "rk_must_not_cross_worker_boundary";
  const runtime = createAppsRuntime({ bundleRoot: root, workerTimeoutMs: 2_000 });
  const response = await runtime.fetch(
    new Request("http://runtime/workers/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/1.0.0/invoke", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ value: "milk" }),
    }),
  );
  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), { formatted: "MILK", leaked: null });
});

test("one failed worker does not affect runtime health", async () => {
  const root = await fixture();
  const runtime = createAppsRuntime({ bundleRoot: root });
  const missing = await runtime.fetch(
    new Request("http://runtime/workers/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb/1.0.0/invoke", { method: "POST", body: "{}" }),
  );
  assert.equal(missing.status, 502);
  const health = await runtime.fetch(new Request("http://runtime/healthz"));
  assert.equal(health.status, 200);
  assert.deepEqual(await health.json(), { status: "ok" });
});

test("rejects path traversal before touching the filesystem", async () => {
  const runtime = createAppsRuntime({ bundleRoot: await fixture() });
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
  const runtime = createAppsRuntime({ bundleRoot: fixtureRoot, workerTimeoutMs: 2_000 });
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
  const runtime = createAppsRuntime({ bundleRoot: fixtureRoot });
  const page = await runtime.fetch(new Request("http://runtime/apps/f1540000-0000-4154-8154-000000000001/1.0.0/"));
  const html = await page.text();
  assert.match(html, /<script src="\.\/app\.js"><\/script>/);
  assert.doesNotMatch(html, /<script>[^<]/);

  const script = await runtime.fetch(new Request("http://runtime/apps/f1540000-0000-4154-8154-000000000001/1.0.0/app.js"));
  const source = await script.text();
  assert.match(source, /api\/cerebro\/apps\/\$\{appId\}\/token/);
  assert.match(source, /registryKey:\s*token\.key/);
  assert.match(source, /aiBaseUrl:\s*token\.ai_base_url/);
  // An app must never pin itself to one environment: the gateway it may call is
  // whatever the token broker handed out, so staging and local can never leak
  // traffic to the production registry.
  assert.doesNotMatch(source, /https?:\/\/[^"']*registry/);
});
