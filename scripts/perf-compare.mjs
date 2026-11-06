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
 *   node scripts/perf-compare.mjs --base <ref> --head <ref> [--out <dir>] [--scenario <name>]
 *
 * `--scenario` picks the measurement: `typing-under-live-runs` (default) or
 * `image-thread` (MUL-8182). Both write the same report path, so one run
 * measures exactly one of them.
 */
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { connect, createServer } from "node:net";

const repoRoot = execFileSync("git", ["rev-parse", "--show-toplevel"]).toString().trim();
const args = process.argv.slice(2);
const flag = (name, fallback) => {
  const at = args.indexOf(`--${name}`);
  return at >= 0 && args[at + 1] ? args[at + 1] : fallback;
};
const baseRef = flag("base");
const headRef = flag("head", "HEAD");
const outDir = resolve(flag("out", join(repoRoot, "perf-report")));
const SCENARIOS = {
  "typing-under-live-runs": {
    title: "Comment typing under live runs (MUL-7227)",
    spec: null,
    metrics: [],
  },
  "image-thread": {
    title: "Comment typing beside an image-heavy thread (MUL-8182)",
    spec: "image-heavy-thread.spec.ts",
    metrics: [
      ["layout_shift", "Layout shift (sum)"],
      ["images_mounted", "Images mounted"],
      ["originals_served", "Originals fetched"],
      ["keystrokes_over_frame", "Keystrokes >16.7ms"],
      ["keystroke_p95_ms", "Keystroke p95 (ms)"],
      ["cls", "CLS"],
      ["image_bytes_served", "Image bytes fetched"],
      ["js_heap_mb", "JS heap (MB)"],
    ],
  },
};
const scenarioName = flag("scenario", "typing-under-live-runs");
const scenario = SCENARIOS[scenarioName];
const repetitions = Number(flag("runs", "3"));
const imageCases = ["in-viewport", "after-scroll", "without-dimensions"];
if (!Number.isInteger(repetitions) || repetitions < 1) throw new Error("--runs must be a positive integer");
if (!baseRef || !scenario) {
  console.error(
    `usage: node scripts/perf-compare.mjs --base <ref> [--head <ref>] [--out <dir>] [--scenario ${Object.keys(SCENARIOS).join("|")}]`,
  );
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

/** True while any process in the group is still alive. */
const groupAlive = (pgid) => {
  try {
    process.kill(-pgid, 0);
    return true;
  } catch (error) {
    // ESRCH: no process left in the group. EPERM would mean one exists that
    // we may not signal — alive, as far as teardown is concerned.
    return error.code === "EPERM";
  }
};

/**
 * Whether anything still holds the port. Only a refused connection counts as
 * closed: a server that has stopped answering still accepts the handshake, and
 * a timeout proves nothing either way.
 */
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

/**
 * Stop a server and wait until every process it started is gone.
 *
 * The pnpm wrapper exiting proves nothing about `next-server` beneath it, so
 * this waits on the whole process group, escalating to SIGKILL if the group
 * outlives SIGTERM. The server stays registered for the last-resort handlers
 * until the group is confirmed gone: dropping it any earlier is what let a
 * surviving child outlive the script. Then the port must refuse connections —
 * not merely stop answering.
 *
 * Teardown is explicit rather than hung off `process.on("exit")`: a live child
 * keeps the event loop running, so `exit` would never fire and the child would
 * never be killed — on CI, until the job was cancelled at sixty minutes.
 */
/**
 * How long a server gets to exit on SIGTERM before it is killed. Next exits in
 * well under a second; the margin is for a loaded machine. Overridable so the
 * lifecycle test can prove the SIGKILL path without waiting on it.
 */
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

    // Every product uses the same spec, screenshots and browser from this tree.
    const measureStart = Date.now();
    const samples = [];
    const cases = scenarioName === "image-thread" ? imageCases : [null];
    const controls = scenarioName === "image-thread" && label === "head" ? [false, true] : [false];
    const runs = scenarioName === "image-thread" ? repetitions : 1;
    let scenarioExit = 0;
    for (const imageCase of cases) for (const control of controls) for (let n = 1; n <= runs; n++) {
      const name = imageCase ? `${control ? "control" : label}-${imageCase}-${n}` : label;
      const reportPath = join(outDir, `${name}.json`);
      rmSync(reportPath, { force: true });
      let sampleExit = 0;
      try {
        run("pnpm", ["exec", "playwright", "test", "--config=playwright.perf.config.ts", scenario.spec ?? "typing-under-live-runs.spec.ts"], {
          cwd: repoRoot,
          env: {
            ...process.env,
            PLAYWRIGHT_BASE_URL: `http://127.0.0.1:${port}`,
            PERF_REPORT_PATH: reportPath,
            PERF_SCENARIO: scenarioName,
            PERF_IMAGE_CASE: imageCase ?? "",
            PERF_NO_IMAGES: control ? "1" : "0",
            PERF_EXPECT_RESERVED_BOXES: label === "head" ? "1" : "0",
          },
        });
      } catch (error) {
        sampleExit = typeof error.status === "number" ? error.status : 1;
        scenarioExit = sampleExit;
      }
      let sample;
      try { sample = JSON.parse(readFileSync(reportPath, "utf8")); }
      catch { sample = { status: "invalid", invalid: ["the scenario produced no readable report"] }; }
      if (sampleExit !== 0) {
        sample.status = "invalid";
        sample.invalid = [...(sample.invalid ?? []), `scenario process exited ${sampleExit}`];
      }
      samples.push({ ...sample, name, repetition: n });
      // A missing/invalid sample is not a latency result; stop before wasting
      // more builds or silently replacing it with a successful retry.
      if (sample.status !== "ok") break;
    }
    const report = scenarioName === "image-thread"
      ? { status: samples.every((sample) => sample.status === "ok") ? "ok" : "invalid", samples }
      : samples[0];
    const measureS = seconds(measureStart);

    const failed = scenarioExit !== 0;
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

function imageMarkdown(base, head) {
  const median = (values) => {
    const sorted = values.filter((v) => typeof v === "number").sort((a, b) => a - b);
    if (!sorted.length) return "—";
    const mid = Math.floor(sorted.length / 2);
    return Number((sorted.length % 2 ? sorted[mid] : (sorted[mid - 1] + sorted[mid]) / 2).toFixed(3));
  };
  const rows = [];
  const metrics = [...METRICS, ...scenario.metrics];
  for (const imageCase of imageCases) {
    const groups = [base.samples?.filter((s) => s.image_case === imageCase), head.samples?.filter((s) => s.image_case === imageCase && !s.no_images), head.samples?.filter((s) => s.image_case === imageCase && s.no_images)];
    for (const [key, label] of metrics) rows.push(`| ${imageCase} | ${label} | ${groups.map((g) => median((g ?? []).map((s) => s[key]))).join(" | ")} |`);
  }
  const digestsMatch = imageCases.every((c) => base.samples?.find((s) => s.image_case === c)?.fixture?.sha256 === head.samples?.find((s) => s.image_case === c && !s.no_images)?.fixture?.sha256);
  return [
    `## ${scenario.title}`, "",
    `Median of ${repetitions} runs per cell; 4× CPU throttle; 172 keystrokes, 25ms inter-key delay; head hosts the no-image control.`,
    "Key latency is browser keydown timestamp to rAF + timer (post-paint proxy); one frame is 16.7ms. CLS excludes recent-input shifts; layout shift sum includes them. Raw samples and before/after PNGs are retained.",
    `Status: base ${base.status}, head ${head.status}. Matching image-fixture hashes: ${digestsMatch}.`, "",
    "| Scenario | Metric | Base | Head | No-image control |", "| --- | --- | ---: | ---: | ---: |", ...rows, "",
    `Base: ${base.sha}; head: ${head.sha}. Chromium ${head.samples?.[0]?.browser_version}; Node ${head.samples?.[0]?.node_version}.`,
    "Small differences require repeat measurements; this benchmark does not assert a speedup.",
  ].join("\n");
}

function markdown(base, head) {
  if (scenarioName === "image-thread") return imageMarkdown(base, head);
  const usable = base.status === "ok" && head.status === "ok";
  const rows = [...METRICS, ...scenario.metrics].map(([key, label]) => {
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
    `## ${scenario.title}`,
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
// The files this script writes, cleared up front: a comparison that aborts
// early must not leave the previous run's summary looking like its own.
for (const name of ["base.json", "head.json", "comparison.json", "comparison.md"]) {
  rmSync(join(outDir, name), { force: true });
}
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
