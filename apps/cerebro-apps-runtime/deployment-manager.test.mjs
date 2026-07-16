import assert from "node:assert/strict";
import test from "node:test";

import { DeploymentManager } from "./deployment-manager.mjs";

const request = { appId: "f1540000-0000-4154-8154-000000000001", version: "1.0.0", bundleSha256: "a".repeat(64) };

test("deduplicates concurrent deployment requests and reports ready once", async () => {
  let deployments = 0;
  const callbacks = [];
  const manager = new DeploymentManager({
    provider: {
      async createOrDeploy() {
        deployments++;
        return { serviceId: "service-1", internalDomain: "app.internal" };
      },
    },
    backend: {
      deploymentInput: async (value) => ({ ...value, bundleUrl: "http://backend.internal/bundle", bundleToken: "token", invokeKey: "invoke" }),
      callback: async (appId, version, value) => callbacks.push({ appId, version, value }),
      pending: async () => [],
    },
  });

  await Promise.all([manager.deploy(request), manager.deploy(request)]);
  assert.equal(deployments, 1);
  assert.deepEqual(callbacks, [{ appId: request.appId, version: request.version, value: { status: "ready", external_service_id: "service-1", internal_domain: "app.internal" } }]);
});

test("resumes every pending deployment after restart", async () => {
  const deployed = [];
  const manager = new DeploymentManager({
    provider: { createOrDeploy: async (value) => (deployed.push(value), { serviceId: value.appId, internalDomain: `${value.appId}.internal` }) },
    backend: {
      deploymentInput: async (value) => value,
      callback: async () => {},
      pending: async () => [request, { ...request, appId: "a1540000-0000-4154-8154-000000000002" }],
    },
  });

  await manager.resume();
  assert.equal(deployed.length, 2);
});

test("forwards pause and delete lifecycle operations to the provider", async () => {
  const calls = [];
  const manager = new DeploymentManager({
    provider: {
      pause: async (serviceId) => calls.push(["pause", serviceId]),
      delete: async (serviceId) => calls.push(["delete", serviceId]),
    },
    backend: {},
  });
  await manager.lifecycle("pause", "service-1");
  await manager.lifecycle("delete", "service-2");
  assert.deepEqual(calls, [["pause", "service-1"], ["delete", "service-2"]]);
  await assert.rejects(manager.lifecycle("restart", "service-3"), /Unsupported app lifecycle operation/);
});
