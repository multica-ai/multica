/**
 * Desktop-only resource scenarios (MUL-9612): what the Electron app costs the
 * machine while someone reads, and whether its memory keeps growing while
 * someone works through their Inbox.
 *
 * 1. `desktop-idle`   — open the big issue (300 comments, 70 runs), let it
 *    settle, then touch nothing for PERF_IDLE_SECONDS (300) and sample the
 *    app's CPU every second.
 * 2. `desktop-memory` — click through the Work panel's twenty Inbox rows,
 *    round and round, for PERF_MEMORY_SECONDS (600), sampling resident memory
 *    every five seconds, and report how fast it grows.
 *
 * CPU and memory come from `ps`, per process role (`lib/process-tree.ts`):
 * nothing in the app measures itself. The scenarios drive the same Desktop
 * renderer as `core-paths.spec.ts` with PERF_PLATFORM=desktop; run them
 * through `scripts/perf-desktop.mjs`. Opt in with PERF_SCENARIO=
 * desktop-resources (both), desktop-idle or desktop-memory.
 */
import { expect, test } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";
import { cpus, loadavg, totalmem } from "node:os";
import { dirname } from "node:path";
import { median, pageNow, quantile, settle, stallsBetween } from "./lib/frame-metrics";
import { ensureDataset, perfApi, resetInbox, type Manifest } from "./lib/core-dataset";
import { inboxStorage, openInbox, switchTo, waitForIssue } from "./lib/core-steps";
import { perfTarget, PLATFORM, type Target } from "./lib/perf-target";
import {
  cdpOwnerPid, cpuByRole, rssByRole, ROLES, slopePerMinute, snapshotTree, type TreeSnapshot,
} from "./lib/process-tree";

const scenario = process.env.PERF_SCENARIO ?? "";
const runIdle = scenario === "desktop-resources" || scenario === "desktop-idle";
const runMemory = scenario === "desktop-resources" || scenario === "desktop-memory";
const IDLE_SECONDS = Number(process.env.PERF_IDLE_SECONDS ?? 300);
/** Untouched time between the content appearing and the idle window, so the tail of loading is not counted as idle. */
const IDLE_SETTLE_MS = 15_000;
const MEMORY_SECONDS = Number(process.env.PERF_MEMORY_SECONDS ?? 600);
const MEMORY_SAMPLE_MS = 5000;
const SWITCH_DWELL_MS = 1000;
const VIEWPORT = { width: 1440, height: 900 };

test.describe.configure({ mode: "serial" });

let manifest: Manifest;
let token: string;
let target: Target;
let appPid: number;
const report: Record<string, unknown> = {};

test.beforeAll(async ({ browser }) => {
  test.skip(!runIdle && !runMemory, "set PERF_SCENARIO=desktop-resources (or desktop-idle / desktop-memory)");
  expect(PLATFORM, "these scenarios measure the Electron app: set PERF_PLATFORM=desktop").toBe("desktop");
  expect(process.env.MULTICA_DEV_PROFILE, "run through `make env-exec` against an isolated environment").toMatch(/^dev-/);
  test.setTimeout(900_000);
  const api = await perfApi();
  token = api.getToken()!;
  manifest = await ensureDataset(api);
  target = await perfTarget(browser, token);
  appPid = Number(process.env.PERF_DESKTOP_PID) || cdpOwnerPid(process.env.PERF_DESKTOP_CDP_URL || process.env.MULTICA_DEV_DESKTOP_CDP_URL!);
  snapshotTree(appPid);
});

test.afterAll(async () => {
  if (!manifest || !target || !process.env.PERF_REPORT_PATH) return;
  const path = process.env.PERF_REPORT_PATH;
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, JSON.stringify({
    ref: process.env.PERF_REF ?? null,
    label: process.env.PERF_LABEL ?? null,
    kind: "desktop-resources",
    platform: PLATFORM,
    build: process.env.PERF_DESKTOP_BUILD ?? null,
    dataset: { version: manifest.version, digest: manifest.digest, counts: manifest.counts },
    environment: {
      browser: await target.version(), viewport: `${VIEWPORT.width}x${VIEWPORT.height}`,
      cpu: cpus()[0]?.model, cores: cpus().length, memory_gb: Math.round(totalmem() / 2 ** 30), load_1m_end: loadavg()[0],
    },
    ...report,
  }, null, 2));
});

const r1 = (v: number) => Math.round(v * 10) / 10;
const byRole = <T>(f: (role: (typeof ROLES)[number] | "app") => T) =>
  Object.fromEntries([...ROLES, "app" as const].map((role) => [role, f(role)]));

test("idle on a big issue", async () => {
  test.skip(!runIdle, "idle scenario not selected");
  test.setTimeout(IDLE_SECONDS * 1000 + 300_000);
  const issue = manifest.issues.large!;
  const sample = await target.newSample({ token, viewport: VIEWPORT, cpuThrottle: 1 });
  const { page, cdp, errors } = sample;
  try {
    await sample.open(`/${manifest.workspace.slug}/issues/${issue.id}`);
    await waitForIssue(page, issue.title, issue.firstCommentId, "first-screen");
    await settle(page, IDLE_SETTLE_MS);
    // A hidden or occluded window is throttled by Chromium and would read as idle.
    expect(await page.evaluate(() => document.visibilityState), "the Desktop window is visible").toBe("visible");

    const taskBefore = (await cdp.send("Performance.getMetrics")).metrics.find((m) => m.name === "TaskDuration")?.value ?? 0;
    const pageStart = await pageNow(page);
    const snapshots: TreeSnapshot[] = [snapshotTree(appPid)];
    const until = snapshots[0]!.at + IDLE_SECONDS * 1000;
    while (Date.now() < until) {
      await new Promise((done) => setTimeout(done, Math.min(1000, until - Date.now())));
      snapshots.push(snapshotTree(appPid));
    }
    const taskAfter = (await cdp.send("Performance.getMetrics")).metrics.find((m) => m.name === "TaskDuration")?.value ?? 0;
    const stalls = await stallsBetween(page, pageStart, await pageNow(page));
    expect(errors).toEqual([]);

    const first = snapshots[0]!;
    const last = snapshots.at(-1)!;
    const seconds = (last.at - first.at) / 1000;
    const perSecond = snapshots.slice(1).map((s, i) => cpuByRole(snapshots[i]!, s));
    const whole = cpuByRole(first, last);
    report.idle = {
      issue: "large",
      seconds: Math.round(seconds),
      cpu_pct: byRole((role) => ({
        mean: r1(whole[role]),
        p95: r1(quantile(perSecond.map((c) => c[role]), 0.95)),
        max: r1(Math.max(...perSecond.map((c) => c[role]))),
      })),
      renderer_main_thread_busy_pct: r1(((taskAfter - taskBefore) / seconds) * 100),
      rss_mb: byRole((role) => ({ start: Math.round(rssByRole(first)[role]), end: Math.round(rssByRole(last)[role]) })),
      processes: Object.fromEntries(ROLES.map((role) => [role, last.processes.filter((p) => p.role === role).length])),
      stalls,
      series: perSecond.map((c) => ({ app: r1(c.app), renderer: r1(c.renderer), gpu: r1(c.gpu), main: r1(c.main) })),
    };
    console.log(JSON.stringify({ idle_cpu_pct: (report.idle as { cpu_pct: unknown }).cpu_pct }));
  } finally {
    await sample.close();
  }
});

test("memory while switching Inbox rows", async () => {
  test.skip(!runMemory, "memory scenario not selected");
  test.setTimeout(MEMORY_SECONDS * 1000 + 300_000);
  await resetInbox(manifest);
  const sample = await target.newSample({ token, viewport: VIEWPORT, cpuThrottle: 1, storage: inboxStorage(manifest.workspace.slug) });
  const { page, cdp, errors } = sample;
  try {
    const rows = await openInbox(sample, manifest, "20");
    const order = manifest.inbox.slice(0, 20).map((n) => manifest.issues[n.key]!);
    const snapshots: TreeSnapshot[] = [snapshotTree(appPid)];
    const switches: { at_s: number; ready_ms: number }[] = [];
    const start = snapshots[0]!.at;
    const until = start + MEMORY_SECONDS * 1000;
    for (let i = 0; Date.now() < until; i++) {
      const issue = order[i % order.length]!;
      const marks = await switchTo(page, rows, issue, "in-view");
      const click = await page.evaluate(() => window.__corePerf.clicks.at(-1)!);
      switches.push({ at_s: Math.round((Date.now() - start) / 1000), ready_ms: Math.round(marks.content - click) });
      await settle(page, SWITCH_DWELL_MS);
      if (Date.now() - snapshots.at(-1)!.at >= MEMORY_SAMPLE_MS) snapshots.push(snapshotTree(appPid));
    }
    snapshots.push(snapshotTree(appPid));
    // After the measured window, so a forced GC cannot shape the RSS series.
    const heap = async () => Math.round(((await cdp.send("Performance.getMetrics")).metrics
      .find((m) => m.name === "JSHeapUsedSize")?.value ?? 0) / 1e6);
    const heapEnd = await heap();
    await cdp.send("HeapProfiler.collectGarbage");
    const heapAfterGc = await heap();
    expect(errors).toEqual([]);

    const minutes = snapshots.map((s) => (s.at - start) / 60_000);
    const rss = snapshots.map(rssByRole);
    const half = Math.floor(snapshots.length / 2);
    const minuteOf = (from: number, to: number, role: keyof (typeof rss)[number]) =>
      median(rss.filter((_, i) => minutes[i]! >= from && minutes[i]! < to).map((r) => r[role]));
    const total = minutes.at(-1)!;
    const firstSwitches = switches.slice(0, 40).map((s) => s.ready_ms);
    const lastSwitches = switches.slice(-40).map((s) => s.ready_ms);
    report.memory = {
      minutes: r1(total),
      switches: switches.length,
      rss_mb: byRole((role) => ({
        first_minute: Math.round(minuteOf(0, 1, role)),
        last_minute: Math.round(minuteOf(total - 1, Infinity, role)),
        slope_mb_per_min: r1(slopePerMinute(minutes, rss.map((r) => r[role]))),
        // The first minutes include caches filling; the second half is the steady state.
        slope_second_half_mb_per_min: r1(slopePerMinute(minutes.slice(half), rss.slice(half).map((r) => r[role]))),
      })),
      js_heap_mb_end: heapEnd,
      js_heap_mb_after_gc: heapAfterGc,
      switch_ready_ms: { first_40_median: median(firstSwitches), last_40_median: median(lastSwitches) },
      series: snapshots.map((s, i) => ({ min: r1(minutes[i]!), app: Math.round(rss[i]!.app), renderer: Math.round(rss[i]!.renderer) })),
    };
    console.log(JSON.stringify({ memory_rss_mb: (report.memory as { rss_mb: unknown }).rss_mb }));
  } finally {
    await sample.close();
  }
});
