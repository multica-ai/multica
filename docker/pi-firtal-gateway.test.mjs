import assert from "node:assert/strict";
import { createServer } from "node:http";
import test from "node:test";

// One realistic GET /v1/models body: a chat model the key holds, the Kimi K3
// row that FIR-3653 grants, and an embedding model that must never reach Pi.
const GATEWAY_MODELS = {
  object: "list",
  data: [
    {
      id: "claude-sonnet-5",
      object: "model",
      owned_by: "anthropic",
      firtal_display_name: "Claude Sonnet 5",
      firtal_capability: "chat",
      firtal_supports_vision: true,
      firtal_cost: { input_per_1k: 0.003, output_per_1k: 0.015, cached_input_per_1k: 0.0003 },
    },
    {
      id: "moonshotai/kimi-k3",
      object: "model",
      owned_by: "openrouter",
      firtal_display_name: "Kimi K3 (OpenRouter)",
      firtal_capability: "chat",
      firtal_supports_vision: false,
      firtal_cost: { input_per_1k: 0.003, output_per_1k: 0.015, cached_input_per_1k: 0.0003 },
    },
    {
      id: "qwen/qwen3-embedding-8b",
      object: "model",
      owned_by: "openrouter",
      firtal_display_name: "Qwen3 Embedding 8B",
      firtal_capability: "embedding",
    },
  ],
};

let server;
let baseUrl;
let requests;

function stubPi() {
  const registered = new Map();
  return {
    registered,
    registerProvider(name, config) {
      registered.set(name, config);
    },
  };
}

async function loadExtension() {
  // Cache-bust so each test re-reads process.env at module scope.
  const module = await import(`./pi-firtal-gateway.ts?test=${requests.length}-${Math.random()}`);
  return module;
}

test.before(async () => {
  requests = [];
  server = createServer((request, response) => {
    requests.push({ url: request.url, authorization: request.headers.authorization || "" });
    if (request.url === "/api/ai/proxy/v1/models") {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify(GATEWAY_MODELS));
      return;
    }
    response.writeHead(404).end();
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  baseUrl = `http://127.0.0.1:${server.address().port}`;
});

test.after(async () => {
  await new Promise((resolve) => server.close(resolve));
});

test.beforeEach(() => {
  process.env.FIRTAL_REGISTRY_URL = baseUrl;
  process.env.FIRTAL_REGISTRY_KEY = "rk_test";
  delete process.env.FIRTAL_REGISTRY_MODEL;
});

test("registers every chat model the gateway grants the key", async () => {
  const pi = stubPi();
  const { default: register } = await loadExtension();
  await register(pi);

  const provider = pi.registered.get("firtal-gateway");
  assert.ok(provider, "firtal-gateway provider must be registered");
  assert.equal(provider.baseUrl, `${baseUrl}/api/ai/proxy/v1`);
  assert.deepEqual(
    provider.models.map((model) => model.id),
    ["claude-sonnet-5", "moonshotai/kimi-k3"],
    "embedding models must be filtered out",
  );
  assert.equal(requests.at(-1).authorization, "Bearer rk_test");
});

test("converts gateway per-1K prices to Pi per-1M prices", async () => {
  const pi = stubPi();
  const { default: register } = await loadExtension();
  await register(pi);

  const kimi = pi.registered.get("firtal-gateway").models.find((m) => m.id === "moonshotai/kimi-k3");
  assert.deepEqual(kimi.cost, { input: 3, output: 15, cacheRead: 0.3, cacheWrite: 0 });
  assert.deepEqual(kimi.input, ["text"], "a non-vision model must not advertise image input");
});

test("keeps the fallback model registered when discovery fails", async () => {
  process.env.FIRTAL_REGISTRY_URL = "http://127.0.0.1:1";
  const pi = stubPi();
  const { default: register } = await loadExtension();
  await register(pi);

  const provider = pi.registered.get("firtal-gateway");
  assert.deepEqual(
    provider.models.map((model) => model.id),
    ["claude-sonnet-5"],
    "cerebro_pi_fallback.go always retries on the fallback model, so it must survive an unreachable gateway",
  );
});

test("honours FIRTAL_REGISTRY_MODEL as the fallback model", async () => {
  process.env.FIRTAL_REGISTRY_URL = "http://127.0.0.1:1";
  process.env.FIRTAL_REGISTRY_MODEL = "claude-haiku-4-5";
  const pi = stubPi();
  const { default: register } = await loadExtension();
  await register(pi);

  assert.deepEqual(
    pi.registered.get("firtal-gateway").models.map((model) => model.id),
    ["claude-haiku-4-5"],
  );
});

test("does nothing without gateway credentials", async () => {
  delete process.env.FIRTAL_REGISTRY_URL;
  delete process.env.FIRTAL_REGISTRY_KEY;
  const pi = stubPi();
  const { default: register } = await loadExtension();
  await register(pi);

  assert.equal(pi.registered.size, 0);
});

test("toProviderModels adds the fallback model to an empty catalog", async () => {
  const { toProviderModels } = await loadExtension();
  assert.deepEqual(
    toProviderModels({ data: [] }, "claude-sonnet-5").map((model) => model.id),
    ["claude-sonnet-5"],
  );
  assert.deepEqual(
    toProviderModels(undefined, "claude-sonnet-5").map((model) => model.id),
    ["claude-sonnet-5"],
  );
});
