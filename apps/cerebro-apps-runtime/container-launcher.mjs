import { spawn } from "node:child_process";
import { resolve, sep } from "node:path";

// Each published app version runs in its OWN container. The bundle mount is
// scoped to a single app+version directory, so one app cannot read another's
// files even if its code tries to.
export function buildDockerArgs({ bundleRoot, appID, version, image, network, memoryMb, cpus, timeoutMs, tokenEndpoint }) {
  const root = resolve(bundleRoot);
  const bundle = resolve(root, appID, version);
  if (!bundle.startsWith(root + sep)) throw new Error("App bundle path escapes the bundle root");
  return [
    "run",
    "--rm",
    "--interactive",
    "--network",
    network,
    "--label",
    `multica.app.id=${appID}`,
    "--label",
    `multica.app.version=${version}`,
    "--memory",
    `${memoryMb}m`,
    "--memory-swap",
    `${memoryMb}m`,
    "--cpus",
    String(cpus),
    "--pids-limit",
    "64",
    "--read-only",
    "--tmpfs",
    "/tmp:size=32m",
    "--cap-drop",
    "ALL",
    "--security-opt",
    "no-new-privileges",
    "--stop-timeout",
    String(Math.ceil(timeoutMs / 1000)),
    "--volume",
    `${bundle}:/srv/app:ro`,
    "--env",
    `MULTICA_APP_ID=${appID}`,
    "--env",
    `MULTICA_APP_VERSION=${version}`,
    "--env",
    `MULTICA_APP_TOKEN_ENDPOINT=${tokenEndpoint}`,
    image,
    "proxychains4",
    "-q",
    "node",
    "/srv/worker-runner.mjs",
    "/srv/app/backend/index.mjs",
  ];
}

export function runInContainer({ input, appID, version, workerTimeoutMs, ...options }) {
  const args = buildDockerArgs({ appID, version, timeoutMs: workerTimeoutMs, ...options });
  return new Promise((resolvePromise, reject) => {
    const child = spawn("docker", args, { stdio: ["pipe", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    const timer = setTimeout(() => {
      child.kill("SIGKILL");
      reject(new Error("App worker exceeded its execution deadline"));
    }, workerTimeoutMs);
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      stdout += chunk;
      if (stdout.length > 1_048_576) child.kill("SIGKILL");
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
      if (stderr.length > 65_536) child.kill("SIGKILL");
    });
    child.once("error", (error) => {
      clearTimeout(timer);
      reject(error);
    });
    child.once("close", (code) => {
      clearTimeout(timer);
      if (code !== 0) {
        reject(new Error(stderr.trim() || "App worker failed"));
        return;
      }
      try {
        resolvePromise(JSON.parse(stdout));
      } catch {
        reject(new Error("App worker returned invalid JSON"));
      }
    });
    child.stdin.end(JSON.stringify(input));
  });
}
