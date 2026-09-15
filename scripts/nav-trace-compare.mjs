#!/usr/bin/env node
/**
 * Run the MUL-7095 navigation-trace recorder against two refs and report the
 * difference (PR #8092 Revision 3).
 *
 * Each ref is checked out, installed and built on its own, then measured
 * sequentially on this machine — one build must never be measured while the
 * other is compiling. The spec, the fixture and the browser always come from
 * the working tree this script runs in, so the two products are the only
 * thing that differs; neither ref needs to contain the spec.
 *
 * Prerequisite: a live backend + database (e.g. `make up`), with its
 * environment in this process (run under `make env-exec` or equivalent).
 * This script passes its own environment through to each ref's install,
 * build and server — including NEXT_PUBLIC_API_URL / DATABASE_URL — and
 * only overrides PORT (per-ref frontend) and, for the spec run,
 * PLAYWRIGHT_BASE_URL + NAV_TRACE_REPORT_PATH. It deliberately does NOT
 * stub REMOTE_API_URL: the recorder creates real issues through the API.
 *
 * Raw samples are always reported. The runner additionally applies a
 * configurable relative guardrail to the primary click-to-populated metric; the
 * guardrail is expressed as a ratio, never as a machine-specific millisecond
 * threshold. A difference inside that allowance remains reviewer data.
 *
 *   node scripts/nav-trace-compare.mjs --base <ref> [--head <ref>] [--out <dir>] [--repeats N] [--project chromium|webkit] [--max-regression R]
 */
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { connect, createServer } from "node:net";
import { invalidForTiming } from "./nav-trace-timing.mjs";

const repoRoot = execFileSync("git", ["rev-parse", "--show-toplevel"]).toString().trim();
const args = process.argv.slice(2);
const flag = (name, fallback) => {
  const at = args.indexOf(`--${name}`);
  return at >= 0 && args[at + 1] ? args[at + 1] : fallback;
};
const baseRef = flag("base");
const headRef = flag("head", "HEAD");
const spec = flag("spec", "e2e/description-navigation-trace.spec.ts");
const repeats = Math.max(1, Number(flag("repeats", "1")) || 1);
const project = flag("project", "chromium");
const maxRelativeRegression = Number(
  flag(
    "max-regression",
    process.env.NAV_TRACE_MAX_RELATIVE_REGRESSION ?? "0.25",
  ),
);
const outDir = resolve(flag("out", join(repoRoot, "nav-trace-report")));
if (
  !baseRef ||
  !["chromium", "webkit"].includes(project) ||
  !Number.isFinite(maxRelativeRegression) ||
  maxRelativeRegression < 0
) {
  console.error("usage: node scripts/nav-trace-compare.mjs --base <ref> [--head <ref>] [--out <dir>] [--repeats N] [--project chromium|webkit] [--max-regression R]");
  process.exit(2);
}

const run = (cmd, cmdArgs, opts = {}) =>
  execFileSync(cmd, cmdArgs, { stdio: "inherit", ...opts });

const delay = (ms) => new Promise((done) => setTimeout(done, ms));

/** Servers and checkouts still alive — last-resort handlers only. */
const liveServers = new Set();
const liveCheckouts = new Set();

const killGroup = (child, signal) => {
  try { process.kill(-child.pid, signal); } catch { /* already gone */ }
};

function removeCheckout(checkout) {
  liveCheckouts.delete(checkout);
  try { run("git", ["worktree", "remove", "--force", checkout], { cwd: repoRoot, stdio: "ignore" }); }
  catch { rmSync(checkout, { recursive: true, force: true }); }
}

const groupAlive = (pgid) => {
  try {
    process.kill(-pgid, 0);
    return true;
  } catch (error) {
    return error.code === "EPERM";
  }
};

const portBound = (port) =>
  new Promise((done) => {
    const socket = connect({ port, host: "127.0.0.1" });
    const settle = (bound) => { socket.destroy(); done(bound); };
    socket.setTimeout(1_000, () => settle(true));
    socket.once("connect", () => settle(true));
    socket.once("error", (error) => settle(error.code !== "ECONNREFUSED"));
  });

async function groupExitWithin(pgid, ms) {
  const deadline = Date.now() + ms;
  while (groupAlive(pgid)) {
    if (Date.now() > deadline) return false;
    await delay(100);
  }
  return true;
}

const STOP_GRACE_MS = Number(process.env.PERF_STOP_GRACE_MS) || 10_000;

async function stopServer(child, port) {
  const pgid = child.pid;
  killGroup(child, "SIGTERM");
  if (!(await groupExitWithin(pgid, STOP_GRACE_MS))) {
    killGroup(child, "SIGKILL");
    if (!(await groupExitWithin(pgid, 5_000))) {
      throw new Error(`server process group ${pgid} survived SIGKILL`);
    }
  }
  liveServers.delete(child);

  const deadline = Date.now() + 5_000;
  while (await portBound(port)) {
    if (Date.now() > deadline) {
      throw new Error(`port ${port} still bound after its server's process group exited`);
    }
    await delay(200);
  }
}

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

// MUL-7095: measurement vs acceptance split. The runner collects the base
// side in `collect` mode (`NAV_TRACE_ACCEPTANCE=collect`), where the spec
// skips the blank-free head-acceptance assertion but still records the
// full timing trace. The known-bad base blanks by design; its correctness
// failure must not make the performance sample unusable. A `collect`-mode
// trace whose timing fields are complete stays usable for the guardrail
// despite a non-zero scenario exit: only runs where NO trace was written
// (or the trace lacks the primary timing) are dropped — plus one narrower
// exclusion: a sampler-loop failure invalidates the sample in BOTH modes
// (measured by the new `samplerErrors` gate inside `invalidForTiming`), so
// `collect` forgives only the known-bad base's blank-frame miss, never a
// broken measurement loop. The head side runs
// the default `accept` mode, so
// head acceptance failures (scenario exit ≠ 0) still exclude its samples.
async function measure(ref, label, { collect = false } = {}) {
  const sha = execFileSync("git", ["rev-parse", ref], { cwd: repoRoot }).toString().trim();
  const checkout = mkdtempSync(join(tmpdir(), `nav-trace-${label}-`));
  liveCheckouts.add(checkout);
  let server;
  let port;
  // Everything this side started is torn down before it returns or throws.
  try {
    console.log(`\n=== ${label}: ${ref} (${sha.slice(0, 9)}) ===`);
    run("git", ["worktree", "add", "--detach", checkout, sha], { cwd: repoRoot });

    const installStart = Date.now();
    run("pnpm", ["install", "--frozen-lockfile"], { cwd: checkout });
    const installS = seconds(installStart);

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
    // Own environment passes through (backend/database wiring included);
    // only the frontend port is overridden per side.
    server = spawn("pnpm", ["--filter", "@multica/web", "start"], {
      cwd: checkout,
      env: { ...process.env, PORT: String(port) },
      stdio: "ignore",
      detached: true,
    });
    liveServers.add(server);
    await waitForServer(port);
    const startS = seconds(startStart);

    // The spec, fixture and browser come from this working tree, not the ref's.
    const traces = [];
    let scenarioFailed = false;
    for (let i = 0; i < repeats; i++) {
      const reportPath = join(outDir, `${project}-${label}-${i}.json`);
      // Only a report this run wrote may be read back.
      rmSync(reportPath, { force: true });
      const measureStart = Date.now();
      let scenarioExit = 0;
      try {
        run(
          "pnpm",
          [
            "exec",
            "playwright",
            "test",
            "--config=playwright.config.ts",
            "--project",
            project,
            spec,
          ],
          {
            cwd: repoRoot,
            env: {
              ...process.env,
              PLAYWRIGHT_BASE_URL: `http://127.0.0.1:${port}`,
              NAV_TRACE_REPORT_PATH: reportPath,
              // Explicit on BOTH sides: the head must never inherit a
              // parent `NAV_TRACE_ACCEPTANCE=collect` (which would skip
              // its blank-free acceptance), and the base side is pinned
              // to `collect` regardless of the parent environment.
              NAV_TRACE_ACCEPTANCE: collect ? "collect" : "accept",
            },
          },
        );
      } catch (error) {
        scenarioExit = typeof error.status === "number" ? error.status : 1;
        scenarioFailed = true;
        console.error(`scenario run ${i} for ${label} exited ${scenarioExit}`);
      }
      const measureS = seconds(measureStart);
      let trace = null;
      try {
        trace = JSON.parse(readFileSync(reportPath, "utf8"));
      } catch { /* no readable report for this repeat */ }
      // MUL-7095: `collect`-mode traces (base side) skip the blank-free
      // head-acceptance assertion by design, so their scenario exit is
      // expected to be non-zero on a known-bad base. `invalidForTiming`
      // (defined above `measure`) decides sample usability from the
      // timing content + mode — not from the exit code alone — so what
      // survives here as `ok` can still count for the guardrail there.
      // Only `accept`-mode (`head`) keeps the old strict rule: an `ok`
      // report from a failed process is not a sample.
      if (
        scenarioExit !== 0 &&
        trace?.status === "ok" &&
        trace?.acceptance !== "collect"
      ) {
        trace.status = "invalid";
        trace.invalid = [
          ...(trace.invalid ?? []),
          `the scenario reported ok but its process exited ${scenarioExit}`,
        ];
      }
      traces.push({ repeat: i, measure_s: measureS, scenario_exit: scenarioExit, trace });
    }
    const usable = traces.filter((t) => !invalidForTiming(t));
    return {
      ref, sha, spec_failed: scenarioFailed, build_cached: buildCached,
      repeats, usable_repeats: usable.length, project,
      status: usable.length > 0 ? "ok" : "invalid",
      traces,
      timings_s: { install: installS, build: buildS, start: startS },
    };
  } finally {
    if (server) await stopServer(server, port);
    removeCheckout(checkout);
  }
}

const pick = (traces, key) => {
  const values = traces
    .filter((t) => !invalidForTiming(t) && typeof t.trace[key] === "number")
    .map((t) => t.trace[key]);
  if (values.length === 0) return null;
  return values.reduce((a, b) => a + b, 0) / values.length;
};

const relativeRegression = (baseValue, headValue, allowance) => {
  if (
    typeof baseValue !== "number" ||
    typeof headValue !== "number" ||
    !Number.isFinite(baseValue) ||
    !Number.isFinite(headValue) ||
    baseValue <= 0
  ) {
    return { available: false, ratio: null, failed: false };
  }
  const ratio = headValue / baseValue;
  return { available: true, ratio, failed: ratio > 1 + allowance };
};

function markdown(base, head, regression) {
  const usable = base.status === "ok" && head.status === "ok";
  const rows = [
    ["clickToPopulatedMs", "Click → first populated (mean)"],
    ["clickToCommitMs", "Click → first commit (mean)"],
    ["maxOverlapMs", "Clipped LongTask max overlap (mean)"],
    ["totalOverlapMs", "Clipped LongTask total overlap (mean)"],
  ].map(([key, label]) => {
    const a = pick(base.traces, key);
    const b = pick(head.traces, key);
    if (a === null || b === null) return `| ${label} | ${a ?? "—"} | ${b ?? "—"} | — | — |`;
    const delta = b - a;
    const ratio = a === 0 ? "N/A" : `${((b / a - 1) * 100).toFixed(1)}%`;
    return `| ${label} | ${a.toFixed(1)} | ${b.toFixed(1)} | ${delta >= 0 ? "+" : ""}${delta.toFixed(1)} | ${ratio} |`;
  });
  const timing = (r) =>
    `install ${r.timings_s.install}s · build ${r.timings_s.build}s${r.build_cached ? " (restored from cache — not a real build)" : ""} · start ${r.timings_s.start}s`;
  return [
    "## Navigation trace A/B (MUL-7095)",
    "",
    usable
      ? "Raw samples (or the mean of --repeats) stay in the report; only the configured relative guardrail can fail this runner."
      : `**Not a usable comparison.** base: \`${base.status}\`, head: \`${head.status}\`.`,
    "",
    "| Metric | base | head | Δ | ratio |",
    "| --- | ---: | ---: | ---: | ---: |",
    ...rows,
    "",
    `- project \`${project}\`; relative click-to-populated guardrail: ${(maxRelativeRegression * 100).toFixed(1)}%${regression.invalid ? ` (INVALID: ${regression.invalid})` : regression.available ? ` (ratio ${regression.ratio.toFixed(3)}; ${regression.failed ? "FAILED" : "passed"})` : " (not evaluated)"}`,
    `- base \`${base.ref}\` (${base.sha.slice(0, 9)}) — ${base.status} (${base.usable_repeats}/${base.repeats} usable)${base.spec_failed ? "; scenario failed" : ""}`,
    `- head \`${head.ref}\` (${head.sha.slice(0, 9)}) — ${head.status} (${head.usable_repeats}/${head.repeats} usable)${head.spec_failed ? "; scenario failed" : ""}`,
    `- spec \`${spec}\` from the working tree; raw traces: \`${project}-base-*.json\`, \`${project}-head-*.json\``,
    `- base timings: ${timing(base)}`,
    `- head timings: ${timing(head)}`,
  ].join("\n");
}

mkdirSync(outDir, { recursive: true });
for (const name of [
  `${project}-base.json`,
  `${project}-head.json`,
  `${project}-comparison.json`,
  `${project}-comparison.md`,
]) {
  rmSync(join(outDir, name), { force: true });
}
const totalStart = Date.now();
let exitCode = 1;
let base;
let head;
try {
  // The base side is collected in `collect` mode: its blank-free
  // correctness failure must not void its timing samples. The head side
  // runs `accept` mode, so head acceptance still gates the comparison.
  base = await measure(baseRef, "base", { collect: true });
  writeFileSync(join(outDir, `${project}-base.json`), JSON.stringify(base, null, 2));
  head = await measure(headRef, "head");
  writeFileSync(join(outDir, `${project}-head.json`), JSON.stringify(head, null, 2));
  const totalS = seconds(totalStart);

  // MUL-7095 BLOCKER ②: the old guardrail measured click→commit only, so
  // deferred parse/view work landing after the route commit never failed
  // the comparison. The primary metric is click→populated; click→commit
  // stays as a diagnostic row. Both sides must report the SAME metric:
  // comparing base-commit against head-populated would pass/fail on data
  // that never shared a scale, so a mismatch invalidates the comparison.
  const primaryKey = (traces) =>
    pick(traces, "clickToPopulatedMs") !== null ? "clickToPopulatedMs" : "clickToCommitMs";
  const baseKey = primaryKey(base.traces);
  const headKey = primaryKey(head.traces);
  const metricMismatch = baseKey !== headKey;
  const regression = metricMismatch
    ? { available: false, ratio: null, failed: false, invalid: `primary metric mismatch: base ${baseKey} vs head ${headKey}` }
    : relativeRegression(
        pick(base.traces, baseKey),
        pick(head.traces, headKey),
        maxRelativeRegression,
      );
  const summary = markdown(base, head, regression);
  writeFileSync(
    join(outDir, `${project}-comparison.json`),
    JSON.stringify(
      {
        base,
        head,
        project,
        max_relative_regression: maxRelativeRegression,
        regression,
        total_s: totalS,
      },
      null,
      2,
    ),
  );
  writeFileSync(join(outDir, `${project}-comparison.md`), `${summary}\n\n- total wall clock: ${totalS}s\n`);
  console.log(`\n${summary}\n\n- total wall clock: ${totalS}s`);
  console.log(`\nreports written to ${outDir}`);

  // A sample must be usable and every scenario must exit zero. A slower head
  // only fails when it exceeds the configured relative guardrail; raw values
  // remain report data and no absolute machine-time threshold is used.
  // A primary-metric mismatch is never a passing comparison: it means the
  // two sides measured different things (one pre-revision, one populated).
  if (metricMismatch) {
    console.error(`comparison invalid: ${regression.invalid}`);
  }
  // MUL-7095: base `spec_failed` no longer gates the exit — the base side
  // is collected in `collect` mode where the blank-free acceptance miss is
  // EXPECTED on a known-bad base (its samples stay usable via
  // `invalidForTiming`). Only head acceptance failures fail the run.
  // `base.status` still gates: with zero timing-usable base samples there
  // is no baseline to compare against.
  exitCode =
    base.status === "ok" &&
    head.status === "ok" &&
    !head.spec_failed &&
    !metricMismatch &&
    !regression.failed
      ? 0
      : 1;
} catch (error) {
  const message = error instanceof Error ? error.message : String(error);
  console.error(`\ncomparison aborted: ${message}`);
  writeFileSync(
    join(outDir, `${project}-comparison.json`),
    JSON.stringify({ error: message, base: base ?? null, head: head ?? null }, null, 2),
  );
}

process.exit(exitCode);
