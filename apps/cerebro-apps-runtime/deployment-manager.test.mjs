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

test("reaps superseded versions only after the new version is reported ready", async () => {
  const order = [];
  const manager = new DeploymentManager({
    provider: {
      async createOrDeploy() {
        order.push("deploy");
        return { serviceId: "service-1", internalDomain: "app.internal" };
      },
      async reapSuperseded(appId, version) {
        order.push(`reap:${appId}@${version}`);
        return 3;
      },
    },
    backend: {
      deploymentInput: async (value) => value,
      callback: async () => order.push("ready"),
      pending: async () => [],
    },
  });

  await manager.deploy(request);
  assert.deepEqual(order, ["deploy", "ready", `reap:${request.appId}@${request.version}`]);
});

test("a reap failure never fails an otherwise healthy deploy", async () => {
  const manager = new DeploymentManager({
    provider: {
      createOrDeploy: async () => ({ serviceId: "service-1", internalDomain: "app.internal" }),
      reapSuperseded: async () => {
        throw new Error("sliplane list unreachable");
      },
    },
    backend: {
      deploymentInput: async (value) => value,
      callback: async () => {},
      pending: async () => [],
    },
  });

  const result = await manager.deploy(request);
  assert.deepEqual(result, { serviceId: "service-1", internalDomain: "app.internal" });
});

test("deploy still succeeds against a provider without a reaper", async () => {
  const manager = new DeploymentManager({
    provider: { createOrDeploy: async () => ({ serviceId: "service-1", internalDomain: "app.internal" }) },
    backend: { deploymentInput: async (value) => value, callback: async () => {}, pending: async () => [] },
  });
  const result = await manager.deploy(request);
  assert.equal(result.serviceId, "service-1");
});
