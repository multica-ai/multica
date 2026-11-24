#!/usr/bin/env node
/**
 * Run the core-path benchmark and the Desktop resource scenarios in the
 * Electron app (MUL-9612).
 *
 *   make up C=api ARGS="--ephemeral"
 *   make env-exec ARGS="-- node scripts/perf-desktop.mjs --out perf/desktop"
 *
 * By default it builds Desktop for production against this environment's
 * API, starts that build with a DevTools port, runs the scenarios through
 * Playwright over CDP, and stops the app again. The renderer is the
 * production bundle, the one users run; `make up C=desktop` serves the
 * development bundle, whose React is several times slower and records
 * profiling data, so its numbers do not describe a release.
 *
 * `--attach` measures the Desktop that `make up C=desktop ARGS="--cdp"`
 * already runs instead (development bundle), for problems that only exist
 * there. That app is left running.
 *
 * Options:
 *   --out DIR            reports go here (default perf/desktop)
 *   --scenarios LIST     core-paths, desktop-resources, or their parts
 *                        (core-issue, core-inbox, desktop-idle, desktop-memory);
 *                        default core-paths,desktop-resources
 *   --skip-build         start the existing apps/desktop/out build
 *   --attach             measure the managed development Desktop
 *
 * PERF_* variables (PERF_REPEATS, PERF_IDLE_SECONDS, ...) pass through to the
 * specs. Writes <scenario>.json per scenario and report.md.
 */
import { execFileSync, spawn } from "node:child_process";
import { createWriteStream, mkdirSync, writeFileSync } from "node:fs";
import { createServer } from "node:net";
import { createRequire } from "node:module";
import { join, resolve } from "node:path";

const repoRoot = execFileSync("git", ["rev-parse", "--show-toplevel"]).toString().trim();
const desktopDir = join(repoRoot, "apps", "desktop");
const args = process.argv.slice(2);
const has = (name) => args.includes(`--${name}`);
const flag = (name, fallback) => {
  const at = args.indexOf(`--${name}`);
  return at >= 0 && args[at + 1] ? args[at + 1] : fallback;
};
const outDir = resolve(flag("out", join(repoRoot, "perf", "desktop")));
const scenarios = flag("scenarios", "core-paths,desktop-resources").split(",").filter(Boolean);
const attach = has("attach");
const SPECS = {
  "core-paths": "core-paths.spec.ts", "core-issue": "core-paths.spec.ts", "core-inbox": "core-paths.spec.ts",
  "desktop-resources": "desktop-resources.spec.ts", "desktop-idle": "desktop-resources.spec.ts", "desktop-memory": "desktop-resources.spec.ts",
};

function fail(message) {
  console.error(`[perf-desktop] ${message}`);
  process.exit(1);
}
const log = (message) => console.log(`[perf-desktop] ${message}`);
const sleep = (ms) => new Promise((done) => setTimeout(done, ms));
const CDP_TIMEOUT_MS = 120_000;

for (const s of scenarios) if (!SPECS[s]) fail(`unknown scenario ${s}; pick from ${Object.keys(SPECS).join(", ")}`);
const env = process.env;
if (!env.PORT || !/^dev-/.test(env.MULTICA_DEV_PROFILE ?? "")) {
  fail('run through `make env-exec ARGS="-- node scripts/perf-desktop.mjs ..."` so the specs reach this environment\'s API.');
}

function freePort() {
  return new Promise((done, reject) => {
    const server = createServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const { port } = server.address();
      server.close(() => done(port));
    });
  });
}

async function waitForCdp(url, deadline, app) {
  for (;;) {
    try {
      const response = await fetch(`${url}/json/version`);
      if (response.ok) return;
    } catch {
      // not listening yet
    }
    if (app && exited(app)) throw new Error(`Desktop exited (${app.exitCode ?? app.signalCode}) before serving CDP; see ${join(outDir, "desktop.log")}`);
    if (Date.now() > deadline) throw new Error(`Desktop never served CDP on ${url}`);
    await sleep(500);
  }
}

function run(command, commandArgs, options) {
  return new Promise((done) => {
    const child = spawn(command, commandArgs, { stdio: "inherit", ...options });
    child.on("exit", (code, signal) => done(signal ? 1 : code ?? 1));
  });
}

// A child killed by a signal keeps exitCode null and sets signalCode instead.
const exited = (child) => child.exitCode !== null || child.signalCode !== null;

async function stop(app) {
  if (exited(app)) return;
  app.kill("SIGTERM");
  for (let i = 0; i < 40 && !exited(app); i++) await sleep(250);
  if (!exited(app)) app.kill("SIGKILL");
}

async function main() {
  mkdirSync(outDir, { recursive: true });
  let app = null;
  let cdpUrl;
  // Registered before Desktop starts, so an interrupt while it boots or while
  // a scenario runs still stops the app this script launched.
  const stopApp = () => (app ? stop(app) : Promise.resolve());
  const onSignal = (signal, code) => process.once(signal, () => {
    log(`${signal}: stopping Desktop`);
    stopApp().finally(() => process.exit(code));
  });
  onSignal("SIGINT", 130);
  onSignal("SIGTERM", 143);
  if (attach) {
    cdpUrl = env.MULTICA_DEV_DESKTOP_CDP_URL;
    if (!cdpUrl) fail('--attach needs the managed Desktop with a DevTools port: make up C=desktop ARGS="--cdp"');
    await waitForCdp(cdpUrl, Date.now() + 30_000, null);
  } else if (!has("skip-build")) {
    log("building Desktop (production bundle)");
    const code = await run("pnpm", ["--filter", "@multica/desktop", "build"], {
      cwd: repoRoot,
      env: {
        ...env,
        VITE_API_URL: `http://localhost:${env.PORT}`,
        VITE_WS_URL: `ws://localhost:${env.PORT}/ws`,
        VITE_APP_URL: `http://localhost:${env.FRONTEND_PORT ?? 3000}`,
      },
    });
    if (code !== 0) fail("the Desktop build failed");
  }

  const reports = [];
  let failed = false;
  try {
    if (!attach) {
      const electron = createRequire(join(desktopDir, "package.json"))("electron");
      const port = await freePort();
      cdpUrl = `http://127.0.0.1:${port}`;
      const appEnv = { ...env, DESKTOP_APP_SUFFIX: `Perf ${env.MULTICA_DEV_ENV ?? env.MULTICA_DEV_PROFILE}` };
      // Without these the main process loads the built renderer from disk and
      // starts signed out; the specs sign in themselves.
      delete appEnv.ELECTRON_RENDERER_URL;
      delete appEnv.MULTICA_DEV_AUTH_TOKEN;
      const appLog = createWriteStream(join(outDir, "desktop.log"));
      app = spawn(electron, [desktopDir, `--remote-debugging-port=${port}`], { env: appEnv, stdio: ["ignore", "pipe", "pipe"] });
      app.stdout.pipe(appLog);
      app.stderr.pipe(appLog);
      log(`started Desktop (pid ${app.pid}, CDP ${cdpUrl})`);
      await waitForCdp(cdpUrl, Date.now() + CDP_TIMEOUT_MS, app);
    }

    for (const scenario of scenarios) {
      const report = join(outDir, `${scenario}.json`);
      log(`running ${scenario}`);
      const code = await run("pnpm", ["exec", "playwright", "test", "--config=playwright.perf.config.ts", SPECS[scenario]], {
        cwd: repoRoot,
        env: {
          ...env,
          PERF_PLATFORM: "desktop",
          PERF_SCENARIO: scenario,
          PERF_REPORT_PATH: report,
          PERF_DESKTOP_CDP_URL: cdpUrl,
          PERF_DESKTOP_BUILD: attach ? "development" : "production",
          ...(app ? { PERF_DESKTOP_PID: String(app.pid) } : {}),
          PERF_REF: env.PERF_REF ?? execFileSync("git", ["rev-parse", "--short", "HEAD"], { cwd: repoRoot }).toString().trim(),
        },
      });
      if (code === 0) reports.push(`desktop=${report}`);
      else failed = true;
    }
  } finally {
    await stopApp();
  }

  if (reports.length > 0) {
    const table = execFileSync("node", [join(repoRoot, "scripts", "perf-core-report.mjs"), ...reports]).toString();
    writeFileSync(join(outDir, "report.md"), table);
    console.log(table);
  }
  if (failed) fail("a scenario failed; its report was not written");
  log(`reports in ${outDir}`);
}

main().catch((error) => fail(error?.stack || String(error)));
