#!/usr/bin/env node
/**
 * Run the comment-typing performance scenario against two refs and report the
 * difference (MUL-7227).
 *
 * Each ref is checked out, installed and built on its own, then measured once,
 * sequentially, on this machine — one build must never be measured while the
 * other is compiling. The spec, the fixture and the browser always come from
 * the working tree this script runs in, so the two products are the only thing
 * that differs; the base ref does not need to contain the test at all.
 *
 * One sample per ref is a report, not a verdict. A difference near the noise
 * floor means "run it again by hand", not "regression".
 *
 *   node scripts/perf-compare.mjs --base <ref> --head <ref> [--out <dir>]
 */
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync, readFileSync, writeFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { createServer } from "node:net";

const repoRoot = execFileSync("git", ["rev-parse", "--show-toplevel"]).toString().trim();
const args = process.argv.slice(2);
const flag = (name, fallback) => {
  const at = args.indexOf(`--${name}`);
  return at >= 0 && args[at + 1] ? args[at + 1] : fallback;
};
const baseRef = flag("base");
const headRef = flag("head", "HEAD");
const outDir = resolve(flag("out", join(repoRoot, "perf-report")));
if (!baseRef) {
  console.error("usage: node scripts/perf-compare.mjs --base <ref> [--head <ref>] [--out <dir>]");
  process.exit(2);
}

const run = (cmd, cmdArgs, opts = {}) =>
  execFileSync(cmd, cmdArgs, { stdio: "inherit", ...opts });

const delay = (ms) => new Promise((done) => setTimeout(done, ms));

/**
 * Servers and checkouts still alive. Each side stops its own before the next
 * one starts; these sets exist only for the last-resort handlers below.
 */
const liveServers = new Set();
const liveCheckouts = new Set();

const killGroup = (child, signal) => {
  // Detached, so the server leads its own process group: signalling the group
  // reaches `next-server` as well as the pnpm wrapper that started it.
  try { process.kill(-child.pid, signal); } catch { /* already gone */ }
};

function removeCheckout(checkout) {
  liveCheckouts.delete(checkout);
  try { run("git", ["worktree", "remove", "--force", checkout], { cwd: repoRoot, stdio: "ignore" }); }
  catch { rmSync(checkout, { recursive: true, force: true }); }
}

/**
 * Stop a server and wait until it is really gone.
 *
 * Teardown used to hang off `process.on("exit")`, which cannot work: a live
 * child keeps the event loop running, so `exit` never fires, so the child is
 * never killed. The report would be written and the process would then wait
 * forever — on CI until the job was cancelled at sixty minutes. Stopping is
 * explicit now, and it is only done when the port has stopped answering.
 */
async function stopServer(child, port) {
  liveServers.delete(child);
  if (child.exitCode === null && child.signalCode === null) {
    const exited = new Promise((done) => child.once("exit", done));
    killGroup(child, "SIGTERM");
    const graceful = await Promise.race([exited.then(() => true), delay(10_000).then(() => false)]);
    if (!graceful) {
      killGroup(child, "SIGKILL");
      await Promise.race([exited, delay(5_000)]);
    }
  }
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    try {
      await fetch(`http://127.0.0.1:${port}/`, { signal: AbortSignal.timeout(1_000) });
    } catch {
      return; // refused: nothing is listening any more
    }
    await delay(250);
  }
  throw new Error(`frontend on :${port} still answering after it was stopped`);
}

// Last resort only, for a signal (a cancelled CI job) or an exit that happens
// before a side finished tearing itself down. Both handlers run synchronously
// and cannot await, which is exactly why the normal path does not use them.
const emergencyStop = () => {
  for (const child of liveServers) killGroup(child, "SIGKILL");
  liveServers.clear();
  for (const checkout of [...liveCheckouts]) removeCheckout(checkout);
};
process.on("exit", emergencyStop);
for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => { emergencyStop(); process.exit(130); });
}

const freePort = () =>
  new Promise((done, fail) => {
    const probe = createServer();
    probe.on("error", fail);
    probe.listen(0, "127.0.0.1", () => {
      const { port } = probe.address();
      probe.close(() => done(port));
    });
  });

const waitForServer = async (port, timeoutMs = 120_000) => {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`http://127.0.0.1:${port}/`);
      if (response.status > 0) return;
    } catch { /* not up yet */ }
    await new Promise((done) => setTimeout(done, 500));
  }
  throw new Error(`frontend on :${port} did not become reachable`);
};

const seconds = (from) => Math.round((Date.now() - from) / 100) / 10;

async function measure(ref, label) {
  const sha = execFileSync("git", ["rev-parse", ref], { cwd: repoRoot }).toString().trim();
  const checkout = mkdtempSync(join(tmpdir(), `perf-${label}-`));
  liveCheckouts.add(checkout);
  let server;
  let port;
  // Everything this side started is torn down before it returns or throws, so
  // the next side is measured on a machine with nothing of this one running.
  try {
    console.log(`\n=== ${label}: ${ref} (${sha.slice(0, 9)}) ===`);
    run("git", ["worktree", "add", "--detach", checkout, sha], { cwd: repoRoot });

    // Each product installs against its own lockfile: a build measured with the
    // other ref's dependency tree is not that ref.
    const installStart = Date.now();
    run("pnpm", ["install", "--frozen-lockfile"], { cwd: checkout });
    const installS = seconds(installStart);

    // REMOTE_API_URL is a runtime setting. Passing it to the build breaks
    // prerendering, and turbo filters it out of the build env anyway.
    //
    // The cache is left on. A restored build is byte-identical to the one that
    // produced it, so it cannot move the numbers this script collects — only the
    // build time it reports. Saying which side was cached costs nothing; forcing
    // two real builds to avoid the ambiguity costs minutes on every local run,
    // and buys nothing on CI, where the runner is always cold anyway.
    const buildStart = Date.now();
    const buildLog = execFileSync(
      "pnpm",
      ["exec", "turbo", "build", "--filter=@multica/web"],
      { cwd: checkout, encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] },
    );
    process.stdout.write(buildLog);
    const buildS = seconds(buildStart);
    const buildCached = /cache hit/.test(buildLog);

    port = await freePort();
    const startStart = Date.now();
    server = spawn("pnpm", ["--filter", "@multica/web", "start"], {
      cwd: checkout,
      env: { ...process.env, PORT: String(port), REMOTE_API_URL: "http://127.0.0.1:1" },
      stdio: "ignore",
      detached: true,
    });
    liveServers.add(server);
    await waitForServer(port);
    const startS = seconds(startStart);

    // The spec, fixture and browser come from this working tree, not the ref's.
    const reportPath = join(outDir, `${label}.json`);
    const measureStart = Date.now();
    let failed = false;
    try {
      run("pnpm", ["exec", "playwright", "test", "--config=playwright.perf.config.ts"], {
        cwd: repoRoot,
        env: {
          ...process.env,
          PLAYWRIGHT_BASE_URL: `http://127.0.0.1:${port}`,
          PERF_REPORT_PATH: reportPath,
        },
      });
    } catch {
      // A failing scenario still wrote its reasons; the comparison reports them.
      failed = true;
    }
    const measureS = seconds(measureStart);

    const report = existsSync(reportPath)
      ? JSON.parse(readFileSync(reportPath, "utf8"))
      : { status: "invalid", invalid: ["the scenario produced no report"] };
    return {
      ...report, ref, sha, spec_failed: failed, build_cached: buildCached,
      timings_s: { install: installS, build: buildS, start: startS, measure: measureS },
    };
  } finally {
    if (server) await stopServer(server, port);
    removeCheckout(checkout);
  }
}

const METRICS = [
  ["typing_elapsed_ms", "Typing elapsed (primary)"],
  ["scenario_elapsed_ms", "Scenario elapsed"],
  ["recalc_style_ms", "Style recalculation"],
  ["layout_ms", "Layout"],
  ["task_ms", "Main-thread task time"],
  ["long_task_count", "Long tasks"],
  ["long_task_total_ms", "Long task total"],
  ["long_task_max_ms", "Long task max"],
];

function markdown(base, head) {
  const usable = base.status === "ok" && head.status === "ok";
  const rows = METRICS.map(([key, label]) => {
    const a = base[key];
    const b = head[key];
    if (typeof a !== "number" || typeof b !== "number") return `| ${label} | ${a ?? "—"} | ${b ?? "—"} | — | — |`;
    const delta = b - a;
    const ratio = a === 0 ? "N/A" : `${((b / a - 1) * 100).toFixed(1)}%`;
    return `| ${label} | ${a} | ${b} | ${delta >= 0 ? "+" : ""}${delta} | ${ratio} |`;
  });
  const timing = (r) =>
    `install ${r.timings_s.install}s · build ${r.timings_s.build}s${r.build_cached ? " (restored from cache — not a real build)" : ""} · start ${r.timings_s.start}s · measure ${r.timings_s.measure}s`;
  return [
    "## Comment typing under live runs (MUL-7227)",
    "",
    usable
      ? "One sample per ref. This is a report, not a merge gate — read a small difference as noise until a second run says otherwise."
      : `**Not a usable comparison.** base: \`${base.status}\`, head: \`${head.status}\`.`,
    "",
    "| Metric | base | head | Δ | ratio |",
    "| --- | ---: | ---: | ---: | ---: |",
    ...rows,
    "",
    `- base \`${base.ref}\` (${base.sha.slice(0, 9)}) — ${base.status}${base.invalid?.length ? `: ${base.invalid.join("; ")}` : ""}`,
    `- head \`${head.ref}\` (${head.sha.slice(0, 9)}) — ${head.status}${head.invalid?.length ? `: ${head.invalid.join("; ")}` : ""}`,
    `- fixture \`${head.fixture?.version}\` sha256 \`${head.fixture?.sha256}\` (${head.fixture?.bytes} bytes); same digest on base: ${base.fixture?.sha256 === head.fixture?.sha256}`,
    `- DOM nodes: base ${base.dom_nodes ?? "—"}, head ${head.dom_nodes ?? "—"}; ticks sent: base ${base.ticks_sent ?? "—"}, head ${head.ticks_sent ?? "—"}`,
    `- node ${head.node_version ?? "—"}, chromium ${head.browser_version ?? "—"}`,
    `- base timings: ${timing(base)}`,
    `- head timings: ${timing(head)}`,
  ].join("\n");
}

mkdirSync(outDir, { recursive: true });
const totalStart = Date.now();
let exitCode = 1;
let base;
let head;
try {
  base = await measure(baseRef, "base");
  head = await measure(headRef, "head");
  const totalS = seconds(totalStart);

  const summary = markdown(base, head);
  writeFileSync(join(outDir, "comparison.json"), JSON.stringify({ base, head, total_s: totalS }, null, 2));
  writeFileSync(join(outDir, "comparison.md"), `${summary}\n\n- total wall clock: ${totalS}s\n`);
  console.log(`\n${summary}\n\n- total wall clock: ${totalS}s`);
  console.log(`\nreports written to ${outDir}`);

  // The comparison itself only fails when a sample is not usable. A slower
  // head is information for the reviewer, not a failure of this script.
  exitCode = base.status === "ok" && head.status === "ok" ? 0 : 1;
} catch (error) {
  const message = error instanceof Error ? error.message : String(error);
  console.error(`\ncomparison aborted: ${message}`);
  // Keep whatever was measured: the artifact is uploaded on failure too.
  writeFileSync(
    join(outDir, "comparison.json"),
    JSON.stringify({ error: message, base: base ?? null, head: head ?? null }, null, 2),
  );
}

// Explicit, whatever happened above. Each side has already stopped its server
// and removed its checkout; exiting here makes sure nothing left over — a
// keep-alive socket, a stray timer — can hold the process open again.
process.exit(exitCode);
