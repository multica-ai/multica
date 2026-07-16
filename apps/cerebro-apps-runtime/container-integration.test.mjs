import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdtemp, realpath, rm, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { promisify } from "node:util";
import test from "node:test";

const exec = promisify(execFile);
const APP_A = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const APP_B = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const VERSION = "1.0.0";
const CONTAINER_TEST_TIMEOUT_MS = 180_000;
const DOCKER_COMMAND_TIMEOUT_MS = 20_000;
const DOCKER_BUILD_TIMEOUT_MS = 120_000;

test("killing app A leaves app B healthy on an internal-only network", { timeout: CONTAINER_TEST_TIMEOUT_MS }, async (t) => {
  if (process.env.GITHUB_ACTIONS === "true") {
    t.skip("GitHub Actions Docker builds time out before the container assertions run");
    return;
  }

  try {
    await docker(["info"]);
  } catch {
    t.skip("Docker is unavailable");
    return;
  }

  const root = await realpath(await mkdtemp(join(import.meta.dirname, ".container-test-")));
  const suffix = String(process.pid);
  const network = `multica-apps-test-${suffix}`;
  const image = `multica/cerebro-app-worker:test-${suffix}`;
  const bundleServer = `multica-app-bundles-${suffix}`;
  const appAContainer = `multica-app-a-${suffix}`;
  const appBContainer = `multica-app-b-${suffix}`;
  t.after(async () => {
    await docker(["rm", "-f", appAContainer, appBContainer, bundleServer]).catch(() => {});
    await docker(["network", "rm", network]).catch(() => {});
    await docker(["image", "rm", "-f", image]).catch(() => {});
    await rm(root, { recursive: true, force: true });
  });

  await Promise.all([
    writeBundle(root, "a.json", APP_A, `export default async function run() { return { app: "A" }; }`),
    writeBundle(root, "b.json", APP_B, `export default async function run(input) { return { app: "B", input }; }`),
  ]);
  await docker(["build", "-f", "apps/cerebro-apps-runtime/Dockerfile.worker", "-t", image, "."], { cwd: resolve(import.meta.dirname, "../.."), timeout: DOCKER_BUILD_TIMEOUT_MS });
  await docker(["network", "create", "--internal", network]);
  await startBundleServer({ image, network, root, name: bundleServer });
  await Promise.all([
    startWorker({ image, network, name: appAContainer, hostname: "app-a", appID: APP_A, bundlePath: "a.json" }),
    startWorker({ image, network, name: appBContainer, hostname: "app-b", appID: APP_B, bundlePath: "b.json" }),
  ]);

  await waitForHealth({ image, network, hostname: "app-a" });
  await waitForHealth({ image, network, hostname: "app-b" });
  await docker(["kill", appAContainer]);
  assert.equal(await probe({ image, network, hostname: "app-b" }), "200");

  const { stdout: internal } = await docker(["network", "inspect", network, "--format", "{{.Internal}}"]).catch(() => ({ stdout: "" }));
  assert.equal(internal.trim(), "true");
  const { stdout: ports } = await docker(["inspect", appBContainer, "--format", "{{json .NetworkSettings.Ports}}"]).catch(() => ({ stdout: "" }));
  assert.equal(ports.trim(), "{}");
});

function docker(args, options = {}) {
  return exec("docker", args, { timeout: DOCKER_COMMAND_TIMEOUT_MS, ...options });
}

async function writeBundle(root, filename, appID, backendSource) {
  const files = [
    file("app.json", JSON.stringify({ manifest: { schema_version: "1", name: appID, version: VERSION, scopes: [], frontend: { entry: "frontend/index.html" }, backend: { entry: "backend/index.mjs" } } })),
    file("frontend/index.html", "<!doctype html><title>Mini app</title>"),
    file("backend/index.mjs", backendSource),
  ];
  await writeFile(join(root, filename), JSON.stringify({ files }));
}

function file(path, content) {
  const body = Buffer.from(content);
  return { path, content_base64: body.toString("base64"), sha256: createHash("sha256").update(body).digest("hex") };
}

async function startBundleServer({ image, network, root, name }) {
  const source = `const http=require("node:http"),fs=require("node:fs");http.createServer((req,res)=>{const file="/bundles/"+req.url.slice(1);try{res.writeHead(200,{"content-type":"application/json"}).end(fs.readFileSync(file))}catch{res.writeHead(404).end()}}).listen(8080,"0.0.0.0")`;
  await docker(["run", "-d", "--rm", "--name", name, "--network", network, "--network-alias", "bundles", "--read-only", "--volume", `${root}:/bundles:ro`, "--entrypoint", "node", image, "-e", source]);
}

async function startWorker({ image, network, name, hostname, appID, bundlePath }) {
  await docker(["run", "-d", "--rm", "--name", name, "--network", network, "--network-alias", hostname, "--read-only", "--tmpfs", "/tmp:size=32m", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--memory", "64m", "--memory-swap", "64m", "--cpus", "0.5", "--pids-limit", "64", "--env", `APP_ID=${appID}`, "--env", `APP_VERSION=${VERSION}`, "--env", `BUNDLE_URL=http://bundles:8080/${bundlePath}`, "--env", "BUNDLE_TOKEN=test", "--env", "INVOKE_KEY=test", "--env", "BACKEND_URL=http://backend.invalid", image]);
}

async function probe({ image, network, hostname }) {
  const source = `fetch("http://${hostname}:4311/healthz").then(r=>{console.log(r.status);process.exit(r.ok?0:1)}).catch(()=>process.exit(1))`;
  const { stdout } = await docker(["run", "--rm", "--network", network, "--entrypoint", "node", image, "-e", source]);
  return stdout.trim();
}

async function waitForHealth(options) {
  for (let attempt = 0; attempt < 50; attempt += 1) {
    if (await probe(options).catch(() => "") === "200") return;
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 100));
  }
  throw new Error(`Worker ${options.hostname} did not become healthy`);
}
