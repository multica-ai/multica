import { spawn } from "node:child_process";
import { readFile } from "node:fs/promises";
import { dirname, extname, join, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

import { runInContainer } from "./container-launcher.mjs";

const UUID = "[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}";
const SEMVER = "(?:0|[1-9][0-9]*)\\.(?:0|[1-9][0-9]*)\\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?";
const FRONTEND_ROUTE = new RegExp(`^/apps/(${UUID})/(${SEMVER})(?:/(.*))?$`, "i");
const WORKER_ROUTE = new RegExp(`^/workers/(${UUID})/(${SEMVER})/invoke$`, "i");
const RUNNER = join(dirname(fileURLToPath(import.meta.url)), "worker-runner.mjs");
const SDK = join(dirname(fileURLToPath(import.meta.url)), "sdk.mjs");
const MIME = new Map([
  [".css", "text/css; charset=utf-8"],
  [".html", "text/html; charset=utf-8"],
  [".js", "text/javascript; charset=utf-8"],
  [".json", "application/json; charset=utf-8"],
  [".svg", "image/svg+xml"],
]);

const CORS = {
  "access-control-allow-origin": "null",
  "access-control-allow-credentials": "true",
  "access-control-allow-methods": "GET, POST, OPTIONS",
  "access-control-allow-headers": "authorization, content-type",
};

const json = (value, status = 200) =>
  new Response(JSON.stringify(value), { status, headers: { ...CORS, "content-type": "application/json; charset=utf-8" } });

function safePath(root, ...parts) {
  const target = resolve(root, ...parts);
  const prefix = resolve(root) + sep;
  return target.startsWith(prefix) ? target : null;
}

export function createAppsRuntime(options = {}) {
  const bundleRoot = resolve(options.bundleRoot ?? process.env.APP_BUNDLE_ROOT ?? "/var/lib/multica-apps");
  const workerTimeoutMs = options.workerTimeoutMs ?? Number(process.env.APP_WORKER_TIMEOUT_MS ?? 5_000);
  const workerMemoryMb = options.workerMemoryMb ?? Number(process.env.APP_WORKER_MEMORY_MB ?? 64);
  const workerCpus = options.workerCpus ?? Number(process.env.APP_WORKER_CPUS ?? 0.5);
  const workerImage = options.workerImage ?? process.env.APP_WORKER_IMAGE ?? "multica/cerebro-app-worker:latest";
  const workerNetwork = options.workerNetwork ?? process.env.APP_WORKER_NETWORK ?? "multica-apps";
  const frameAncestors = options.frameAncestors ?? process.env.MULTICA_FRAME_ANCESTORS ?? "'self'";
  // Every app gets its own container. "process" isolation exists for local dev
  // and tests only — a shared process is never acceptable in production, so it
  // is refused there rather than silently downgrading the isolation guarantee.
  const isolation = options.workerIsolation ?? process.env.APP_WORKER_ISOLATION ?? "container";
  if (isolation !== "container" && isolation !== "process") throw new Error(`Unknown app worker isolation ${isolation}`);
  if (isolation === "process" && process.env.NODE_ENV === "production") {
    throw new Error("App workers must run in their own container in production");
  }

  return {
    async fetch(request) {
      const url = new URL(request.url);
      if (request.method === "OPTIONS") return new Response(null, { status: 204, headers: CORS });
      if (url.pathname === "/healthz") return json({ status: "ok" });
      if (request.method === "GET" && url.pathname === "/sdk/multica.js") {
        return new Response(await readFile(SDK), { headers: { ...CORS, "cache-control": "public, max-age=31536000, immutable", "content-type": "text/javascript; charset=utf-8" } });
      }

      const frontend = FRONTEND_ROUTE.exec(url.pathname);
      if (request.method === "GET" && frontend) {
        const [, appID, version, requested = ""] = frontend;
        const relative = requested === "" || requested.endsWith("/") ? requested + "index.html" : requested;
        const path = safePath(bundleRoot, appID, version, "frontend", relative);
        if (!path) return json({ error: "Not found" }, 404);
        try {
          const body = await readFile(path);
          return new Response(body, {
            headers: {
              "cache-control": "public, max-age=31536000, immutable",
              ...CORS,
              "content-type": MIME.get(extname(path)) ?? "application/octet-stream",
              "content-security-policy": `default-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self' https://registry.firtal.com; frame-ancestors ${frameAncestors}`,
            },
          });
        } catch {
          return json({ error: "App frontend not found" }, 404);
        }
      }

      const worker = WORKER_ROUTE.exec(url.pathname);
      if (request.method === "POST" && worker) {
        const [, appID, version] = worker;
        const modulePath = safePath(bundleRoot, appID, version, "backend", "index.mjs");
        if (!modulePath) return json({ error: "App backend not found" }, 404);
        let input;
        try {
          input = await request.json();
        } catch {
          return json({ error: "Request body must be valid JSON" }, 400);
        }
        try {
          const output =
            isolation === "container"
              ? await runInContainer({
                  bundleRoot,
                  appID,
                  version,
                  input,
                  workerTimeoutMs,
                  image: workerImage,
                  network: workerNetwork,
                  memoryMb: workerMemoryMb,
                  cpus: workerCpus,
                  tokenEndpoint: process.env.MULTICA_APP_TOKEN_ENDPOINT ?? "",
                })
              : await runWorker({ modulePath, input, appID, version, workerTimeoutMs, workerMemoryMb });
          return json(output);
        } catch (error) {
          console.error("mini-app worker failed", { appID, version, error });
          return json({ error: "App worker failed" }, 502);
        }
      }

      return json({ error: "Not found" }, 404);
    },
  };
}

function runWorker({ modulePath, input, appID, version, workerTimeoutMs, workerMemoryMb }) {
  return new Promise((resolvePromise, reject) => {
    const child = spawn(process.execPath, [`--max-old-space-size=${workerMemoryMb}`, RUNNER, modulePath], {
      stdio: ["pipe", "pipe", "pipe"],
      env: {
        PATH: process.env.PATH ?? "",
        NODE_ENV: "production",
        MULTICA_APP_ID: appID,
        MULTICA_APP_VERSION: version,
        MULTICA_APP_TOKEN_ENDPOINT: process.env.MULTICA_APP_TOKEN_ENDPOINT ?? "",
      },
    });
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
