import assert from "node:assert/strict";
import test from "node:test";

import { SliplaneProvider, serviceNameForVersion } from "./sliplane-provider.mjs";

const deployment = {
  appId: "f1540000-0000-4154-8154-000000000001",
  appName: "Allergen Formatter",
  version: "1.2.3",
  bundleUrl: "http://multica-backend-8nnfh2.internal:8080/api/cerebro/apps-internal/f1540000-0000-4154-8154-000000000001/1.2.3/bundle",
  bundleToken: "signed-bundle-token",
  invokeKey: "worker-invoke-key",
};
const workerCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";

test("requires an explicit worker branch and exact worker commit before creating a service", async () => {
  let requests = 0;
  const provider = new SliplaneProvider({
    apiKey: "key",
    projectId: "project-1",
    serverId: "server-1",
    workerBranch: "main",
    fetch: async () => (requests++, Response.json({}, { status: 201 })),
  });
  await assert.rejects(provider.createOrDeploy(deployment), /App deployment failed/);
  assert.equal(requests, 0);
});

test("uses the app's human name as the Sliplane service name", () => {
  const name = serviceNameForVersion(deployment.appName, deployment.appId, deployment.version);
  assert.match(name, /^Allergen Formatter · 1\.2\.3 · [0-9a-f]{8}$/);
  assert.doesNotMatch(name, /cerebro-app|f1540000/);
});

test("creates a private service on the runtime server and explicitly deploys it", async () => {
  const requests = [];
  let healthChecks = 0;
  const provider = new SliplaneProvider({
    apiKey: "sliplane-secret",
    projectId: "project-1",
    serverId: "server-1",
    workerBranch: "main",
    workerCommit,
    healthPollMs: 1,
    healthTimeoutMs: 5_000,
    fetch: async (url, init = {}) => {
      requests.push({ url, init, body: init.body ? JSON.parse(init.body) : null });
      if (String(url).endsWith("/services") && (!init.method || init.method === "GET")) return Response.json([]);
      if (String(url).endsWith("/services") && init.method === "POST") {
        return Response.json({ id: "service-1", network: { internalDomain: "cerebro-app-f1540000-v1-2-3.internal" } }, { status: 201 });
      }
      if (String(url).endsWith("/services/service-1") && (!init.method || init.method === "GET")) {
        return Response.json({ id: "service-1", serverId: "server-1", network: { internalDomain: "cerebro-app-f1540000-v1-2-3.internal" } });
      }
      if (String(url).endsWith("/services/service-1/deploy")) return Response.json({ status: "queued" }, { status: 202 });
      if (String(url) === "http://cerebro-app-f1540000-v1-2-3.internal:4311/healthz") {
        healthChecks++;
        return Response.json({ status: healthChecks === 1 ? "starting" : "ok", commit: workerCommit }, { status: healthChecks === 1 ? 503 : 200 });
      }
      throw new Error(`unexpected request ${init.method} ${url}`);
    },
  });

  const result = await provider.createOrDeploy(deployment);
  const create = requests.find((request) => String(request.url).endsWith("/services") && request.init.method === "POST");
  assert.equal(create.init.headers.authorization, "Bearer sliplane-secret");
  assert.equal(create.body.name, serviceNameForVersion(deployment.appName, deployment.appId, deployment.version));
  assert.equal(create.body.serverId, "server-1");
  assert.deepEqual(create.body.network, { public: false });
  assert.equal(create.body.deployment.branch, "main");
  assert.equal(create.body.deployment.autoDeploy, false);
  assert.equal(create.body.deployment.dockerfilePath, "apps/cerebro-apps-runtime/Dockerfile.worker");
  assert.deepEqual(create.body.env, [
    { key: "APP_ID", value: deployment.appId, secret: false },
    { key: "APP_VERSION", value: deployment.version, secret: false },
    { key: "BUNDLE_URL", value: deployment.bundleUrl, secret: false },
    { key: "BUNDLE_TOKEN", value: deployment.bundleToken, secret: true },
    { key: "BUNDLE_SHA256", value: "", secret: false },
    { key: "BACKEND_URL", value: "", secret: false },
    { key: "WORKER_COMMIT", value: workerCommit, secret: false },
    { key: "INVOKE_KEY", value: deployment.invokeKey, secret: true },
    { key: "PORT", value: "4311", secret: false },
  ]);
  assert.ok(!JSON.stringify(create.body).includes("sliplane.app"));
  assert.deepEqual(result, { serviceId: "service-1", internalDomain: "cerebro-app-f1540000-v1-2-3.internal" });
  assert.ok(requests.some((request) => /projects\/project-1\/services\/service-1\/deploy$/.test(request.url)));
  assert.equal(healthChecks, 2);
});

test("treats Sliplane's empty 204 deploy response as success", async () => {
  const provider = new SliplaneProvider({
    apiKey: "key",
    projectId: "project-1",
    serverId: "server-1",
    workerBranch: "main",
    workerCommit,
    healthPollMs: 1,
    healthTimeoutMs: 100,
    fetch: async (url, init = {}) => {
      if (String(url).endsWith("/services") && (!init.method || init.method === "GET")) return Response.json([]);
      if (String(url).endsWith("/services") && init.method === "POST") {
        return Response.json({ id: "service-1", network: { internalDomain: "ready.internal" } }, { status: 201 });
      }
      if (String(url).endsWith("/services/service-1") && (!init.method || init.method === "GET")) {
        return Response.json({ id: "service-1", serverId: "server-1", network: { internalDomain: "ready.internal" } });
      }
      if (String(url).endsWith("/services/service-1/deploy")) return new Response(null, { status: 204 });
      if (String(url) === "http://ready.internal:4311/healthz") return Response.json({ status: "ok", commit: workerCommit });
      throw new Error(`unexpected request ${init.method} ${url}`);
    },
  });

  assert.deepEqual(await provider.createOrDeploy(deployment), { serviceId: "service-1", internalDomain: "ready.internal" });
});

test("refreshes a created service before using its canonical internal domain", async () => {
  const healthUrls = [];
  let deployed = false;
  const provider = new SliplaneProvider({
    apiKey: "key",
    projectId: "project-1",
    serverId: "server-1",
    workerBranch: "main",
    workerCommit,
    healthPollMs: 1,
    healthTimeoutMs: 5_000,
    fetch: async (url, init = {}) => {
      if (String(url).endsWith("/services") && (!init.method || init.method === "GET")) return Response.json([]);
      if (String(url).endsWith("/services") && init.method === "POST") {
        return Response.json({
          id: "service-1",
          serverId: "server-1",
          network: { internalDomain: "provisional-ujd9pc.internal" },
        }, { status: 201 });
      }
      if (String(url).endsWith("/services/service-1") && (!init.method || init.method === "GET")) {
        return Response.json({
          id: "service-1",
          serverId: "server-1",
          status: deployed ? "live" : "pending",
          network: { internalDomain: deployed ? "canonical.internal" : "provisional-ujd9pc.internal" },
        });
      }
      if (String(url).endsWith("/services/service-1/deploy")) {
        deployed = true;
        return new Response(null, { status: 204 });
      }
      if (String(url).endsWith("/healthz")) {
        healthUrls.push(String(url));
        if (String(url) === "http://canonical.internal:4311/healthz") {
          return Response.json({ status: "ok", commit: workerCommit });
        }
        throw new Error("getaddrinfo ENOTFOUND");
      }
      throw new Error(`unexpected request ${init.method} ${url}`);
    },
  });

  assert.deepEqual(await provider.createOrDeploy(deployment), {
    serviceId: "service-1",
    internalDomain: "canonical.internal",
  });
  assert.deepEqual(healthUrls, ["http://canonical.internal:4311/healthz"]);
});

test("reuses an existing deployment before creating another Sliplane service", async () => {
  let creates = 0;
  const provider = new SliplaneProvider({
    apiKey: "key",
    projectId: "project-1",
    serverId: "server-1",
    workerBranch: "main",
    workerCommit,
    fetch: async (url, init = {}) => {
      if (String(url).endsWith("/services") && (!init.method || init.method === "GET")) {
        return Response.json([{ id: "existing", name: serviceNameForVersion(deployment.appName, deployment.appId, deployment.version), serverId: "server-1", network: { internalDomain: "existing.internal" }, env: [
          { key: "APP_ID", value: deployment.appId },
          { key: "APP_VERSION", value: deployment.version },
          { key: "BUNDLE_SHA256", value: "" },
          { key: "WORKER_COMMIT", value: workerCommit },
        ] }]);
      }
      if (String(url).endsWith("/services") && init.method === "POST") {
        creates++;
        return Response.json({}, { status: 201 });
      }
      if (String(url).endsWith("/services/existing/deploy")) return new Response(null, { status: 204 });
      if (String(url) === "http://existing.internal:4311/healthz") return Response.json({ status: "ok", commit: workerCommit });
      throw new Error(`unexpected request ${init.method} ${url}`);
    },
  });

  assert.deepEqual(await provider.createOrDeploy(deployment), { serviceId: "existing", internalDomain: "existing.internal" });
  assert.equal(creates, 0);
});

test("reuses a deterministic service after a create conflict", async () => {
  let creates = 0;
  let lists = 0;
  const provider = new SliplaneProvider({
    apiKey: "key",
    projectId: "project-1",
    serverId: "server-1",
    workerBranch: "main",
    workerCommit,
    fetch: async (url, init = {}) => {
      if (String(url).endsWith("/services") && init.method === "POST") {
        creates++;
        return new Response("already exists", { status: 409 });
      }
      if (String(url).endsWith("/services") && (!init.method || init.method === "GET")) {
        lists++;
        if (lists === 1) return Response.json([]);
        return Response.json([{ id: "existing", name: serviceNameForVersion(deployment.appName, deployment.appId, deployment.version), serverId: "server-1", network: { internalDomain: "existing.internal" }, env: [
          { key: "APP_ID", value: deployment.appId },
          { key: "APP_VERSION", value: deployment.version },
          { key: "BUNDLE_SHA256", value: "" },
          { key: "WORKER_COMMIT", value: workerCommit },
        ] }]);
      }
      if (String(url).endsWith("/services/existing/deploy")) return Response.json({}, { status: 202 });
      if (String(url) === "http://existing.internal:4311/healthz") return Response.json({ status: "ok", commit: workerCommit });
      throw new Error(`unexpected request ${init.method} ${url}`);
    },
  });

  assert.deepEqual(await provider.createOrDeploy(deployment), { serviceId: "existing", internalDomain: "existing.internal" });
  assert.equal(creates, 1);
});

test("service identity cannot collide on app IDs or prerelease punctuation", () => {
  assert.notEqual(
    serviceNameForVersion("Formatter", "aaaaaaaa-1111-4111-8111-111111111111", "1.0.0"),
    serviceNameForVersion("Formatter", "aaaaaaaa-2222-4222-8222-222222222222", "1.0.0"),
  );
  assert.notEqual(
    serviceNameForVersion(deployment.appName, deployment.appId, "1.0.0-alpha.1"),
    serviceNameForVersion(deployment.appName, deployment.appId, "1.0.0-alpha-1"),
  );
});

test("rejects an existing service whose immutable deployment identity mismatches", async () => {
  let deployCalled = false;
  const provider = new SliplaneProvider({
    apiKey: "key",
    projectId: "project-1",
    serverId: "server-1",
    workerBranch: "main",
    workerCommit,
    fetch: async (url, init = {}) => {
      if (String(url).endsWith("/services") && init.method === "POST") return new Response("exists", { status: 409 });
      if (String(url).endsWith("/services")) return Response.json([{
        id: "wrong-service",
        name: serviceNameForVersion(deployment.appName, deployment.appId, deployment.version),
        serverId: "server-1",
        network: { internalDomain: "wrong.internal" },
        env: [{ key: "APP_ID", value: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" }],
      }]);
      if (String(url).includes("/deploy")) deployCalled = true;
      throw new Error(`unexpected request ${init.method} ${url}`);
    },
  });
  await assert.rejects(provider.createOrDeploy(deployment), /App deployment failed/);
  assert.equal(deployCalled, false);
});

test("fails safely when the private worker never becomes healthy", async () => {
  const provider = new SliplaneProvider({
    apiKey: "key",
    projectId: "project-1",
    serverId: "server-1",
    workerBranch: "main",
    workerCommit,
    healthPollMs: 1,
    healthTimeoutMs: 5,
    fetch: async (url, init = {}) => {
      if (String(url).endsWith("/services") && (!init.method || init.method === "GET")) return Response.json([]);
      if (String(url).endsWith("/services") && init.method === "POST") {
        return Response.json({ id: "service-1", network: { internalDomain: "never-ready.internal" } }, { status: 201 });
      }
      if (String(url).endsWith("/services/service-1") && (!init.method || init.method === "GET")) {
        return Response.json({ id: "service-1", serverId: "server-1", network: { internalDomain: "never-ready.internal" } });
      }
      if (String(url).endsWith("/services/service-1/deploy")) return Response.json({}, { status: 202 });
      if (String(url) === "http://never-ready.internal:4311/healthz") return Response.json({ status: "starting" }, { status: 503 });
      throw new Error(`unexpected request ${init.method} ${url}`);
    },
  });

  await assert.rejects(provider.createOrDeploy(deployment), /App deployment failed/);
});

test("rejects a healthy worker running a different commit", async () => {
  const provider = new SliplaneProvider({
    apiKey: "key", projectId: "project-1", serverId: "server-1", workerBranch: "main", workerCommit,
    healthPollMs: 1, healthTimeoutMs: 5,
    fetch: async (url, init = {}) => {
      if (String(url).endsWith("/services") && (!init.method || init.method === "GET")) return Response.json([]);
      if (String(url).endsWith("/services") && init.method === "POST") return Response.json({ id: "service-1", network: { internalDomain: "wrong-commit.internal" } }, { status: 201 });
      if (String(url).endsWith("/services/service-1") && (!init.method || init.method === "GET")) return Response.json({ id: "service-1", serverId: "server-1", network: { internalDomain: "wrong-commit.internal" } });
      if (String(url).endsWith("/services/service-1/deploy")) return Response.json({}, { status: 202 });
      if (String(url) === "http://wrong-commit.internal:4311/healthz") return Response.json({ status: "ok", commit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" });
      throw new Error(`unexpected request ${init.method} ${url}`);
    },
  });
  await assert.rejects(provider.createOrDeploy(deployment), /App deployment failed/);
});

test("masks Sliplane response bodies from callers", async () => {
  const provider = new SliplaneProvider({
    apiKey: "key",
    projectId: "project-1",
    serverId: "server-1",
    workerBranch: "main",
    workerCommit,
    fetch: async () => new Response("secret=/srv/private and api-key", { status: 500 }),
  });
  await assert.rejects(provider.createOrDeploy(deployment), (error) => {
    assert.equal(error.message, "App deployment failed");
    assert.doesNotMatch(error.message, /secret|private|api-key/);
    return true;
  });
});

test("pauses and deletes only the concrete private service", async () => {
  const requests = [];
  const provider = new SliplaneProvider({
    apiKey: "key",
    projectId: "project-1",
    serverId: "server-1",
    fetch: async (url, init = {}) => (requests.push([String(url), init.method]), new Response(null, { status: 204 })),
  });
  await provider.pause("service-1");
  await provider.delete("service-1");
  assert.deepEqual(requests, [
    ["https://ctrl.sliplane.io/v0/projects/project-1/services/service-1/pause", "POST"],
    ["https://ctrl.sliplane.io/v0/projects/project-1/services/service-1", "DELETE"],
  ]);
});
