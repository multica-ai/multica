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

/** Everything this script starts, so a failure cannot leave it behind. */
const cleanups = [];
const cleanup = () => {
  while (cleanups.length) {
    try { cleanups.pop()(); } catch { /* best effort */ }
  }
};
process.on("exit", cleanup);
for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => { cleanup(); process.exit(130); });
}

const run = (cmd, cmdArgs, opts = {}) =>
  execFileSync(cmd, cmdArgs, { stdio: "inherit", ...opts });

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
  console.log(`\n=== ${label}: ${ref} (${sha.slice(0, 9)}) ===`);
  run("git", ["worktree", "add", "--detach", checkout, sha], { cwd: repoRoot });
  cleanups.push(() => {
    try { run("git", ["worktree", "remove", "--force", checkout], { cwd: repoRoot, stdio: "ignore" }); }
    catch { rmSync(checkout, { recursive: true, force: true }); }
  });

  // Each product installs against its own lockfile: a build measured with the
  // other ref's dependency tree is not that ref.
  const installStart = Date.now();
  run("pnpm", ["install", "--frozen-lockfile"], { cwd: checkout });
  const installS = seconds(installStart);

  // REMOTE_API_URL is a runtime setting. Passing it to the build breaks
  // prerendering, and turbo filters it out of the build env anyway.
  const buildStart = Date.now();
  run("pnpm", ["exec", "turbo", "build", "--filter=@multica/web"], { cwd: checkout });
  const buildS = seconds(buildStart);

  const port = await freePort();
  const startStart = Date.now();
  const server = spawn("pnpm", ["--filter", "@multica/web", "start"], {
    cwd: checkout,
    env: { ...process.env, PORT: String(port), REMOTE_API_URL: "http://127.0.0.1:1" },
    stdio: "ignore",
    detached: true,
  });
  cleanups.push(() => { try { process.kill(-server.pid, "SIGKILL"); } catch { /* gone */ } });
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
    ...report, ref, sha, spec_failed: failed,
    timings_s: { install: installS, build: buildS, start: startS, measure: measureS },
  };
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
  const timing = (r) => `install ${r.timings_s.install}s · build ${r.timings_s.build}s · start ${r.timings_s.start}s · measure ${r.timings_s.measure}s`;
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
const base = await measure(baseRef, "base");
const head = await measure(headRef, "head");
const totalS = seconds(totalStart);

const summary = markdown(base, head);
writeFileSync(join(outDir, "comparison.json"), JSON.stringify({ base, head, total_s: totalS }, null, 2));
writeFileSync(join(outDir, "comparison.md"), `${summary}\n\n- total wall clock: ${totalS}s\n`);
console.log(`\n${summary}\n\n- total wall clock: ${totalS}s`);
console.log(`\nreports written to ${outDir}`);

// The comparison itself only fails when a sample is not usable. A slower head
// is information for the reviewer, not a failure of this script.
if (base.status !== "ok" || head.status !== "ok") process.exit(1);
