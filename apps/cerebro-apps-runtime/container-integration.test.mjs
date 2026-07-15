import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtemp, mkdir, realpath, rm, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { promisify } from "node:util";
import test from "node:test";

import { runInContainer } from "./container-launcher.mjs";

const exec = promisify(execFile);
const APP_A = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const APP_B = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const VERSION = "1.0.0";

test("killing app A leaves app B healthy on an internal-only network", { timeout: 60_000 }, async (t) => {
  try {
    await exec("docker", ["info"]);
  } catch {
    t.skip("Docker is unavailable");
    return;
  }

  const root = await realpath(await mkdtemp(join(import.meta.dirname, ".container-test-")));
  const network = `multica-apps-test-${process.pid}`;
  const image = `multica/cerebro-app-worker:test-${process.pid}`;
  t.after(async () => {
    await exec("docker", ["rm", "-f", ...(await containerIDs(APP_A)), ...(await containerIDs(APP_B))]).catch(() => {});
    await exec("docker", ["network", "rm", network]).catch(() => {});
    await exec("docker", ["image", "rm", "-f", image]).catch(() => {});
    await rm(root, { recursive: true, force: true });
  });

  await Promise.all([
    writeBackend(root, APP_A, `export default async function run() { await new Promise((resolve) => setTimeout(resolve, 30_000)); return { app: "A" }; }`),
    writeBackend(root, APP_B, `export default async function run(input) { return { app: "B", input }; }`),
  ]);
  await exec("docker", ["build", "-f", "apps/cerebro-apps-runtime/Dockerfile.worker", "-t", image, "."], { cwd: resolve(import.meta.dirname, "../..") });
  await exec("docker", ["network", "create", "--internal", network]);

  const common = { bundleRoot: root, version: VERSION, workerTimeoutMs: 30_000, image, network, memoryMb: 64, cpus: 0.5, tokenEndpoint: "http://token-layer.invalid" };
  const appA = runInContainer({ ...common, appID: APP_A, input: {} });
  // Attach the expected rejection handler before Docker can report the killed
  // container's exit. Under CI load, awaiting app B first left a narrow window
  // where node:test treated app A's expected rejection as unhandled.
  const appAExit = assert.rejects(appA);
  await waitForContainer(APP_A);

  assert.deepEqual(await runInContainer({ ...common, appID: APP_B, input: { alive: true } }), { app: "B", input: { alive: true } });
  await exec("docker", ["kill", ...(await containerIDs(APP_A))]);
  await appAExit;

  const { stdout } = await exec("docker", ["network", "inspect", network, "--format", "{{.Internal}}"]);
  assert.equal(stdout.trim(), "true");
});

async function writeBackend(root, appID, source) {
  const directory = join(root, appID, VERSION, "backend");
  await mkdir(directory, { recursive: true, mode: 0o755 });
  await writeFile(join(directory, "index.mjs"), source, { mode: 0o644 });
}

async function containerIDs(appID) {
  const { stdout } = await exec("docker", ["ps", "-aq", "--filter", `label=multica.app.id=${appID}`]);
  return stdout.trim().split("\n").filter(Boolean);
}

async function waitForContainer(appID) {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    if ((await containerIDs(appID)).length > 0) return;
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error(`Container for ${appID} did not start`);
}
