#!/usr/bin/env node
/**
 * Serve this environment's web app from a production build, on the
 * environment's own web port, then put the dev server back (MUL-9796).
 *
 *   make web-prod                                             # serve until Ctrl-C
 *   make web-prod ARGS="-- node scripts/web-nav-timing.mjs"   # serve, run, restore
 *   make web-prod ARGS="--skip-build -- <command>"            # reuse apps/web/.next
 *
 * Navigation timings taken on `next dev` describe neither the number users
 * see nor its bottleneck: dev does not prefetch routes, and it serves every
 * module unbundled. MUL-9781 measured "open a doc" at 0.66s there; the
 * production build showed the doc page's JS chunk waiting on the route's
 * RSC payload, which dev never exposed.
 *
 * The build is same-origin, as the deployed web app is: the browser calls
 * /api, /ws and /collab on the page's own origin and `next start` proxies
 * them to this environment's API (REMOTE_API_URL). It is served on the
 * environment's web port, not a free one, because the API accepts realtime
 * WebSockets only from FRONTEND_ORIGIN; on any other port /ws is refused
 * with 403. So the dev web server is stopped for the duration and started
 * again afterwards, also on Ctrl-C and when the command fails. The build
 * rewrites apps/web/next-env.d.ts, which is restored after it.
 *
 * With a command after `--`, the command runs once the server answers, with
 * WEB_PROD_URL set to it, and its exit code becomes this script's. Without
 * one, the server runs until Ctrl-C.
 *
 * Run through `make web-prod`, which is `make env-exec` around this script.
 * It refuses an environment whose web app `multica env start web` supervises:
 * the daemon ends that environment once its web port stops answering.
 */
import { execFileSync, spawn, spawnSync } from "node:child_process";
import { closeSync, existsSync, openSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const repoRoot = execFileSync("git", ["rev-parse", "--show-toplevel"]).toString().trim();
const webDir = join(repoRoot, "apps", "web");
const devEnv = join(repoRoot, "scripts", "dev-env.sh");
const nextEnvFile = join(webDir, "next-env.d.ts");

const argv = process.argv.slice(2);
const separator = argv.indexOf("--");
const own = separator >= 0 ? argv.slice(0, separator) : argv;
const command = separator >= 0 ? argv.slice(separator + 1) : [];
const log = (message) => console.log(`[web-prod] ${message}`);
function fail(message) {
  console.error(`[web-prod] ${message}`);
  process.exit(1);
}
for (const flag of own) if (flag !== "--skip-build") fail(`unknown option ${flag}; see the header of scripts/web-prod.mjs`);
const skipBuild = own.includes("--skip-build");
if (separator >= 0 && command.length === 0) fail("nothing to run after --");

const env = process.env;
if (!env.PORT || !env.FRONTEND_PORT || !/^dev-/.test(env.MULTICA_DEV_PROFILE ?? "")) {
  fail("run through `make web-prod` so it serves this environment's web port against its API.");
}
const webPort = Number(env.FRONTEND_PORT);
const apiUrl = `http://localhost:${env.PORT}`;
const webUrl = `http://localhost:${webPort}`;
const sleep = (ms) => new Promise((done) => setTimeout(done, ms));

function status() {
  const result = spawnSync("bash", [devEnv, "status", "--json"], { encoding: "utf8" });
  try {
    return JSON.parse(result.stdout);
  } catch {
    fail(`no environment for this checkout; start one with \`make up\`.\n${result.stderr}`);
  }
}

function devEnvRun(...args) {
  return spawnSync("bash", [devEnv, ...args], { stdio: ["ignore", "inherit", "inherit"] }).status === 0;
}

function claimedByDaemon() {
  const claim = join(repoRoot, ".multica", "agent-environments", "web.claim");
  if (!existsSync(claim)) return false;
  const pid = Number(readFileSync(claim, "utf8").trim());
  if (!pid) return false;
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

function listenerPid(port) {
  const result = spawnSync("lsof", ["-nP", `-iTCP:${port}`, "-sTCP:LISTEN", "-t"], { encoding: "utf8" });
  return result.stdout.trim().split("\n").filter(Boolean)[0] ?? "";
}

async function build(track) {
  const original = readFileSync(nextEnvFile);
  log("building apps/web for production (same-origin API)");
  try {
    // REMOTE_API_URL is a runtime setting: at build time it breaks
    // prerendering. Empty rather than unset, so next.config.ts's dotenv
    // does not read it back from the root .env.
    const buildEnv = { ...env, NEXT_PUBLIC_API_URL: "", NEXT_PUBLIC_WS_URL: "", REMOTE_API_URL: "", NEXT_TELEMETRY_DISABLED: "1" };
    const builder = spawn("pnpm", ["--filter", "@multica/web", "build"], { cwd: repoRoot, env: buildEnv, stdio: "inherit" });
    track(builder);
    const code = await new Promise((done) => {
      builder.on("error", () => done(127));
      builder.on("exit", (exitCode, signal) => done(signal ? 1 : exitCode ?? 1));
    });
    if (code !== 0) throw new Error("the web build failed");
  } finally {
    writeFileSync(nextEnvFile, original);
  }
}

const exited = (child) => child.exitCode !== null || child.signalCode !== null;
const READY_TIMEOUT_MS = 60_000;

async function main() {
  if (claimedByDaemon()) {
    fail("this web app is supervised by `multica env start web`, which ends the environment once its port stops answering. Measure in a `make up` environment instead.");
  }
  const before = status();
  const webState = before.components?.web?.state ?? "stopped";
  if (webState === "mismatch") {
    fail(`port ${webPort} is held by a process outside this environment; see \`make status\`.`);
  }
  if (before.components?.api?.state !== "running") fail("the API is not running; run `make up` first.");
  if (skipBuild && !existsSync(join(webDir, ".next", "BUILD_ID"))) fail("--skip-build: apps/web/.next holds no production build");

  let stoppedDev = false;
  let server = null;
  // The build, then the command: whichever runs now gets a signal passed on,
  // and its exit lets main() reach cleanup().
  let child = null;
  let cleaning = null;
  const cleanup = () => {
    cleaning ??= (async () => {
      if (server?.pid && !exited(server)) {
        try { process.kill(-server.pid, "SIGTERM"); } catch { /* already gone */ }
        for (let i = 0; i < 40 && !exited(server); i++) await sleep(250);
        if (!exited(server)) try { process.kill(-server.pid, "SIGKILL"); } catch { /* already gone */ }
      }
      if (server?.pid || stoppedDev) {
        for (let i = 0; i < 40 && listenerPid(webPort); i++) await sleep(250);
      }
      if (stoppedDev) {
        log("starting the dev web server again");
        if (!devEnvRun("up", "--components", "web")) {
          console.error("[web-prod] the dev web server did not come back; run `make up C=web`.");
          process.exitCode = 1;
        }
      }
    })();
    return cleaning;
  };
  let interrupted = 0;
  let wakeHold = () => {};
  for (const [signal, code] of [["SIGINT", 130], ["SIGTERM", 143]]) {
    process.on(signal, () => {
      interrupted = code;
      process.exitCode = code;
      // A terminal's Ctrl-C reaches the child too; a SIGTERM sent to this
      // process alone does not.
      if (child && !exited(child)) child.kill(signal);
      wakeHold();
    });
  }

  try {
    if (!skipBuild) await build((builder) => { child = builder; });
    if (interrupted) return;
    if (webState !== "stopped") {
      log("stopping the dev web server");
      stoppedDev = true;
      if (!devEnvRun("down", "--components", "web")) throw new Error("could not stop the dev web server");
    }
    for (let i = 0; i < 40 && listenerPid(webPort); i++) await sleep(250);
    if (listenerPid(webPort)) throw new Error(`port ${webPort} is still in use`);

    const serverLog = join(before.logs, "web-prod.log");
    const out = openSync(serverLog, "w");
    let serverError = null;
    server = spawn(join(webDir, "node_modules", ".bin", "next"), ["start", "--port", String(webPort)], {
      cwd: webDir,
      env: { ...env, PORT: String(webPort), REMOTE_API_URL: apiUrl, NEXT_PUBLIC_API_URL: "", NEXT_PUBLIC_WS_URL: "" },
      stdio: ["ignore", out, out],
      // Its own process group, so Ctrl-C reaches it only through cleanup().
      detached: true,
    });
    server.on("error", (error) => { serverError = error; });
    closeSync(out);
    const deadline = Date.now() + READY_TIMEOUT_MS;
    for (let ready = false; !ready; ) {
      if (serverError) throw new Error(`could not start next: ${serverError.message}`);
      if (exited(server)) throw new Error(`next start exited (${server.exitCode ?? server.signalCode}); see ${serverLog}`);
      if (interrupted) return;
      if (Date.now() > deadline) throw new Error(`next start did not answer on ${webUrl} within ${READY_TIMEOUT_MS / 1000}s; see ${serverLog}`);
      try {
        ready = (await fetch(`${webUrl}/`, { redirect: "manual" })).status > 0;
      } catch {
        await sleep(250);
      }
    }
    log(`production build on ${webUrl} (pid ${server.pid}, log ${serverLog})`);

    if (command.length === 0) {
      log("Ctrl-C stops it and starts the dev web server again");
      await new Promise((done) => {
        wakeHold = done;
        server.on("exit", done);
      });
      if (!interrupted) throw new Error(`next start exited (${server.exitCode ?? server.signalCode}); see ${serverLog}`);
      return;
    }
    child = spawn(command[0], command.slice(1), { stdio: "inherit", env: { ...env, WEB_PROD_URL: webUrl } });
    const code = await new Promise((done) => {
      child.on("error", (error) => {
        console.error(`[web-prod] ${error.message}`);
        done(127);
      });
      child.on("exit", (exitCode, signal) => done(signal ? 128 : exitCode ?? 1));
    });
    process.exitCode = interrupted || code;
  } catch (error) {
    if (!interrupted) {
      console.error(`[web-prod] ${error.message}`);
      process.exitCode = 1;
    }
  } finally {
    await cleanup();
  }
}

main();
