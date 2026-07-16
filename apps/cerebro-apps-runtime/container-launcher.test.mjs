import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { buildDockerArgs } from "./container-launcher.mjs";
import { createAppsRuntime } from "./runtime.mjs";

const APP_A = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const APP_B = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";

const args = (appID) =>
  buildDockerArgs({
    bundleRoot: "/var/lib/multica-apps",
    appID,
    version: "1.0.0",
    image: "multica/cerebro-app-worker:latest",
    network: "multica-apps",
    memoryMb: 64,
    cpus: 0.5,
    timeoutMs: 5_000,
    tokenEndpoint: "http://multica:8080/api/cerebro/apps",
  });

test("each app version runs in its own container", () => {
  assert.equal(args(APP_A)[0], "run");
  assert.ok(args(APP_A).includes("--rm"));
  assert.ok(args(APP_A).includes("multica/cerebro-app-worker:latest"));
  const labels = args(APP_A).flatMap((arg, index, all) => arg === "--label" ? [all[index + 1]] : []);
  assert.ok(labels.includes(`multica.app.id=${APP_A}`));
  assert.ok(labels.includes("multica.app.version=1.0.0"));
  const imageIndex = args(APP_A).indexOf("multica/cerebro-app-worker:latest");
  assert.deepEqual(args(APP_A).slice(imageIndex + 1, imageIndex + 5), ["proxychains4", "-q", "node", "/srv/worker-runner.mjs"]);
});

test("production runtime has no Docker binary or host socket", async () => {
	const compose = await readFile(new URL("./docker-compose.yml", import.meta.url), "utf8");
	const dockerfile = await readFile(new URL("./Dockerfile", import.meta.url), "utf8");
	assert.match(compose, /cerebro-app-worker:/);
	assert.match(compose, /Dockerfile\.worker/);
	assert.doesNotMatch(compose, /\/var\/run\/docker\.sock/);
	assert.doesNotMatch(dockerfile, /docker-cli/);
	assert.match(compose, /CEREBRO_APPS_RUNTIME_PROVIDER:\s*sliplane/);
});

test("an app only mounts its own bundle, read-only", () => {
  const mount = args(APP_A)[args(APP_A).indexOf("--volume") + 1];
  assert.equal(mount, `/var/lib/multica-apps/${APP_A}/1.0.0:/srv/app:ro`);
});

test("one app cannot reach another app's files", () => {
  // The whole point of a container per app: app A's container is never given a
  // path that contains app B, so B is unreachable even if A's code tries.
  assert.ok(!args(APP_A).some((arg) => arg.includes(APP_B)));
  assert.ok(!args(APP_B).some((arg) => arg.includes(APP_A)));
});

test("the container drops privileges and caps its resources", () => {
  const a = args(APP_A);
  assert.deepEqual(
    ["--cap-drop", "ALL"],
    [a[a.indexOf("--cap-drop")], a[a.indexOf("--cap-drop") + 1]],
  );
  assert.equal(a[a.indexOf("--security-opt") + 1], "no-new-privileges");
  assert.equal(a[a.indexOf("--memory") + 1], "64m");
  assert.equal(a[a.indexOf("--cpus") + 1], "0.5");
  assert.equal(a[a.indexOf("--pids-limit") + 1], "64");
  assert.ok(a.includes("--read-only"));
});

test("a bundle path that escapes the root is refused", () => {
  assert.throws(
    () =>
      buildDockerArgs({
        bundleRoot: "/var/lib/multica-apps",
        appID: "../../etc",
        version: "1.0.0",
        image: "img",
        network: "multica-apps",
        memoryMb: 64,
        cpus: 0.5,
        timeoutMs: 5_000,
        tokenEndpoint: "",
      }),
    /escapes the bundle root/,
  );
});

test("production refuses to share a process between apps", () => {
  const previous = process.env.NODE_ENV;
  process.env.NODE_ENV = "production";
  try {
    assert.throws(() => createAppsRuntime({ workerIsolation: "process" }), /own container in production/);
  } finally {
    process.env.NODE_ENV = previous;
  }
});
