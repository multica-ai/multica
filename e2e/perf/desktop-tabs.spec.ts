/**
 * Desktop benchmark (MUL-9609): switching between tabs, and opening issues
 * from the Work panel's Inbox, in the real Electron app on the "large
 * workspace" dataset of the core-path benchmark.
 *
 * Tabs exist only on Desktop. Exactly one tab is mounted at a time, so a
 * switch tears down the outgoing page and mounts the incoming one from the
 * Query cache; neither the web benchmarks nor jsdom can see that cost.
 *
 * 1. `tabs`  — open six tabs (issue list, Inbox, four issues of different
 *    sizes; the long one read down to a comment 80% of the way), then click
 *    through the tab strip twice.
 * 2. `inbox` — the core-path Inbox scenario, inside Desktop: click twenty
 *    Work panel Inbox rows, then go back and forth between the first five.
 *
 * Every step reports the time from the click until the content is in view,
 * the next paint, main-thread task time, and Long Animation Frames, on the
 * page's own clock (`lib/frame-metrics.ts`). Opt in with
 * PERF_SCENARIO=desktop (both) or desktop-tabs / desktop-inbox; see README.
 */
import { _electron as electron, expect, test, type CDPSession, type ElectronApplication, type Page } from "@playwright/test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { cpus, loadavg, tmpdir } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import {
  attachFrameMetrics, interactionLatency, lastClick, median, pageNow, quantile, settle, stallsBetween,
} from "./lib/frame-metrics";
import { ensureDataset, perfApi, resetInbox, type Manifest } from "./lib/core-dataset";

const scenario = process.env.PERF_SCENARIO ?? "";
const runTabs = scenario === "desktop" || scenario === "desktop-tabs";
const runInbox = scenario === "desktop" || scenario === "desktop-inbox";
const REPEATS = Number(process.env.PERF_REPEATS ?? 5);
const WARMUP = Number(process.env.PERF_WARMUP ?? 1);
const CPU_THROTTLE = Number(process.env.PERF_CPU_THROTTLE ?? 1);
const WINDOW = { width: 1440, height: 900 };
const SWITCH_DWELL_MS = 1000;
const TAB_CYCLES = 2;
const DESKTOP_DIR = fileURLToPath(new URL("../../apps/desktop", import.meta.url));

/**
 * Builds to measure, `name=<electron-vite out dir>` pairs separated by
 * commas. Rounds alternate between them. An out dir must sit inside
 * apps/desktop (e.g. apps/desktop/.perf-builds/main) so the main process
 * resolves its externalized dependencies. Defaults to this checkout's build.
 */
const TARGETS = (process.env.PERF_DESKTOP_TARGETS ?? `head=${join(DESKTOP_DIR, "out")}`)
  .split(",").map((pair) => { const [name, dir] = pair.split("="); return { name: name!, dir: resolve(dir!) }; });

test.describe.configure({ mode: "serial" });

let manifest: Manifest;
let token: string;
const report: { tabs: Record<string, unknown>[]; inbox: Record<string, unknown>[] } = { tabs: [], inbox: [] };

test.beforeAll(async () => {
  test.skip(!runTabs && !runInbox, "set PERF_SCENARIO=desktop (or desktop-tabs / desktop-inbox)");
  test.setTimeout(900_000);
  expect(process.env.MULTICA_DEV_PROFILE, "run through `make env-exec` against an isolated environment").toMatch(/^dev-/);
  const api = await perfApi();
  token = api.getToken()!;
  manifest = await ensureDataset(api);
});

test.afterAll(async () => {
  if (!manifest || !process.env.PERF_REPORT_PATH) return;
  const path = process.env.PERF_REPORT_PATH;
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, JSON.stringify({
    dataset: { version: manifest.version, digest: manifest.digest, counts: manifest.counts },
    environment: {
      window: `${WINDOW.width}x${WINDOW.height}`, cpu_throttle: CPU_THROTTLE, cpu: cpus()[0]?.model,
      cores: cpus().length, load_1m_end: loadavg()[0], repeats: REPEATS, targets: TARGETS.map((t) => t.name),
    },
    summary: summarize(),
    samples: report,
  }, null, 2));
});

interface Desktop {
  app: ElectronApplication;
  page: Page;
  cdp: CDPSession;
  errors: string[];
  close: () => Promise<void>;
}

let launches = 0;

/**
 * Launches a signed-in Desktop on the perf workspace. Each launch gets its own
 * app-data directory (DESKTOP_APP_SUFFIX) and HOME, so it never shares a
 * single-instance lock, CLI profile or daemon with an installed Multica or an
 * earlier launch still shutting down, and daemon auto-start is off: no agent
 * daemon runs during a sample.
 */
async function launchDesktop(outDir: string, label: string): Promise<Desktop> {
  const home = mkdtempSync(join(tmpdir(), "multica-perf-desktop-"));
  mkdirSync(join(home, ".multica"));
  writeFileSync(join(home, ".multica", "desktop_prefs.json"), JSON.stringify({ autoStart: false, autoStop: false }));
  const executablePath = createRequire(join(DESKTOP_DIR, "package.json"))("electron") as unknown as string;
  const app = await electron.launch({
    executablePath,
    args: [join(outDir, "main", "index.js"), "--use-mock-keychain"],
    env: {
      ...process.env,
      HOME: home,
      DESKTOP_APP_SUFFIX: `perf-${label}-${process.pid}-${++launches}`,
      MULTICA_DEV_AUTH_TOKEN: token,
      ELECTRON_RENDERER_URL: "",
    },
  });
  const userData = await app.evaluate(({ app }) => app.getPath("userData"));
  // Only a dev launch takes the suffix; anything else is a real install's data.
  if (!basename(userData).includes(`perf-${label}-`)) {
    await app.close().catch(() => {});
    rmSync(home, { recursive: true, force: true });
    throw new Error(`refusing to use app data outside this run: ${userData}`);
  }
  const close = async () => {
    // A dev launch registers itself as the multica:// handler; hand it back.
    await app.evaluate(({ app }) => app.removeAsDefaultProtocolClient("multica", process.execPath, [app.getAppPath()])).catch(() => {});
    await app.close().catch(() => {});
    rmSync(userData, { recursive: true, force: true });
    rmSync(home, { recursive: true, force: true });
  };
  let page = await app.firstWindow();
  try {
    // The main window is the one that shows the tab strip once signed in.
    await expect.poll(async () => {
      for (const candidate of app.windows()) {
        if (await candidate.locator("[data-tab-id]").count().catch(() => 0)) { page = candidate; return true; }
      }
      return false;
    }, { timeout: 60_000, intervals: [250] }).toBe(true);
    await app.evaluate(({ BrowserWindow }, size) => {
      const win = BrowserWindow.getAllWindows()[0]!;
      win.setContentSize(size.width, size.height);
      win.center();
    }, WINDOW);
    // Pin the Inbox section's size and keep Chat closed; a reload applies both
    // from storage, as for a returning user.
    await page.evaluate((slug) => {
      localStorage.setItem(`multica_sidebar_tree:${slug}`, JSON.stringify({ state: { inboxSettings: { limit: 20 } }, version: 0 }));
      localStorage.setItem("multica:chat:isOpen", "false");
    }, manifest.workspace.slug);
    await navigate(page, `/${manifest.workspace.slug}/issues`, "push");
    await page.reload();
    await expect(page.locator("li[data-inbox-row]").first()).toBeVisible({ timeout: 60_000 });
    await attachFrameMetrics(page);
    const cdp = await page.context().newCDPSession(page);
    await cdp.send("Performance.enable");
    if (CPU_THROTTLE > 1) await cdp.send("Emulation.setCPUThrottlingRate", { rate: CPU_THROTTLE });
    const errors: string[] = [];
    page.on("pageerror", (error) => errors.push(error.message));
    return { app, page, cdp, errors, close };
  } catch (error) {
    const urls = app.windows().map((w) => w.url());
    await page.screenshot({ path: test.info().outputPath(`launch-${label}.png`) }).catch(() => {});
    await close();
    throw new Error(`Desktop did not come up signed in (windows: ${urls.join(", ")})`, { cause: error });
  }
}

/** The app's own internal-link entry point (what a link click or ⌘-click dispatches). */
async function navigate(page: Page, path: string, disposition: "push" | "foreground-tab") {
  await page.evaluate(({ path, disposition }) => {
    window.dispatchEvent(new CustomEvent("multica:navigate", { detail: { path, disposition } }));
  }, { path, disposition });
}

const TRACE_DIR = process.env.PERF_TRACE_DIR;

/** Starts a Chrome trace and a sampling CPU profile; the returned function writes both. */
async function startTrace({ cdp }: Desktop): Promise<(path: string) => Promise<void>> {
  const events: unknown[] = [];
  const collect = (e: { value: unknown[] }) => events.push(...e.value);
  cdp.on("Tracing.dataCollected", collect);
  await cdp.send("Tracing.start", {
    categories: "devtools.timeline,disabled-by-default-devtools.timeline,v8.execute,blink.user_timing,loading",
    transferMode: "ReportEvents",
  } as never);
  await cdp.send("Profiler.enable");
  await cdp.send("Profiler.setSamplingInterval", { interval: 100 });
  await cdp.send("Profiler.start");
  return async (path) => {
    const { profile } = await cdp.send("Profiler.stop");
    const done = new Promise<void>((resolve) => cdp.once("Tracing.tracingComplete", () => resolve()));
    await cdp.send("Tracing.end");
    await done;
    cdp.off("Tracing.dataCollected", collect);
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(`${path}.cpuprofile`, JSON.stringify(profile));
    writeFileSync(`${path}.trace.json`, JSON.stringify({ traceEvents: events }));
  };
}

async function taskTime(cdp: CDPSession): Promise<number> {
  const { metrics } = await cdp.send("Performance.getMetrics");
  return (metrics.find((m) => m.name === "TaskDuration")?.value ?? 0) * 1000;
}

/** `text`: a page whose content is a fixed message, such as the Inbox page while the Work panel lists the notifications. */
type TabSpec = { key: string; path: string; kind: "issue" | "list" | "text"; title?: string };

/**
 * Resolves with the page-clock time of the first frame in which the tab's
 * content is laid out inside the main area's viewport: for an issue, its
 * title and a piece of its history (description or comment); for a list,
 * a row; for a text page, its message. The Work panel's own rows do not count.
 */
async function waitForContent(page: Page, tab: TabSpec): Promise<number> {
  const handle = await page.waitForFunction(({ kind, title }) => {
    const inPanel = (el: Element) => !!el.closest('[data-slot="space-panel"]');
    const inView = (el: Element, root: Element | null) => {
      const box = el.getBoundingClientRect();
      if (box.height === 0) return false;
      const view = (root ?? document.documentElement).getBoundingClientRect();
      return box.bottom > view.top && box.top < view.bottom && box.top < window.innerHeight;
    };
    let ready = false;
    if (kind === "issue") {
      const detail = [...document.querySelectorAll('[data-mc="issue-detail"][data-view="page"]')]
        .find((d) => !inPanel(d) && d.querySelector('[data-mc="issue-title"]')?.textContent?.includes(title!));
      const root = detail?.querySelector("[data-tab-scroll-root]") ?? null;
      ready = !!detail && [...detail.querySelectorAll("[data-comment-content]")].some((el) => el.textContent?.trim() && inView(el, root));
    } else if (kind === "text") {
      ready = [...document.querySelectorAll("p")].some((el) => el.textContent?.trim() === title && !inPanel(el) && inView(el, null));
    } else {
      ready = [...document.querySelectorAll('[data-mc="issue-row"], [data-mc="board-card"]')].some((el) => !inPanel(el) && inView(el, null));
    }
    if (!ready) return false;
    return performance.now();
  }, { kind: tab.kind, title: tab.title }, { polling: "raf", timeout: 60_000 }).catch(async (error) => {
    await page.screenshot({ path: test.info().outputPath(`timeout-${tab.key}.png`) }).catch(() => {});
    throw error;
  });
  return (await handle.jsonValue()) as number;
}

async function measureStep(desktop: Desktop, act: () => Promise<void>, tab: TabSpec) {
  const { page, cdp } = desktop;
  await settle(page, 200);
  const workBefore = await taskTime(cdp);
  await act();
  const ready = await waitForContent(page, tab);
  const click = await lastClick(page);
  await settle(page, SWITCH_DWELL_MS);
  const end = await pageNow(page);
  return {
    ready_ms: Math.round(ready - click),
    next_paint_ms: (await interactionLatency(page, click, end)) ?? 16,
    task_ms: Math.round((await taskTime(cdp)) - workBefore),
    ...(await stallsBetween(page, click - 1, end)),
    dom_nodes: await page.evaluate(() => document.getElementsByTagName("*").length),
  };
}

function tabSet(): TabSpec[] {
  const slug = manifest.workspace.slug;
  const issue = (key: string): TabSpec => {
    const i = manifest.issues[key]!;
    return { key, path: `/${slug}/issues/${i.id}`, kind: "issue", title: i.title };
  };
  return [
    { key: "issue-list", path: `/${slug}/issues`, kind: "list" },
    issue("large"),
    { key: "inbox", path: `/${slug}/inbox`, kind: "text", title: "Select a notification to view details" },
    issue("p99"),
    issue("medium-0"),
    issue("small-4"),
  ];
}

test("switch between Desktop tabs", async () => {
  test.skip(!runTabs, "tabs scenario not selected");
  test.setTimeout(3_600_000);
  const tabs = tabSet();
  for (let round = -WARMUP; round < REPEATS; round++) {
    for (const target of round % 2 ? TARGETS : [...TARGETS].reverse()) {
      const desktop = await launchDesktop(target.dir, target.name);
      try {
        const { page } = desktop;
        // The launch tab already shows the issue list; open the rest in new tabs.
        const opens: Record<string, unknown>[] = [];
        for (const tab of tabs.slice(1)) {
          await settle(page, 200);
          const before = await pageNow(page);
          await navigate(page, tab.path, "foreground-tab");
          const ready = await waitForContent(page, tab);
          await settle(page, SWITCH_DWELL_MS);
          opens.push({ tab: tab.key, ready_ms: Math.round(ready - before), ...(await stallsBetween(page, before, await pageNow(page))) });
          if (tab.key === "large") {
            // Read down to the notified comment (80% of the way), so its restore has
            // real depth and every sample restores the same rows.
            const reached = await page.evaluate(async (commentId) => {
              const root = document.querySelector<HTMLElement>('[data-mc="issue-detail"][data-view="page"] [data-tab-scroll-root]')!;
              for (let i = 0; i < 400; i++) {
                const target = root.querySelector<HTMLElement>(`[data-comment-content="${commentId}"]`);
                if (target) {
                  target.scrollIntoView({ block: "start" });
                  return true;
                }
                root.scrollTop += root.clientHeight;
                await new Promise((done) => requestAnimationFrame(() => requestAnimationFrame(done)));
              }
              return false;
            }, manifest.issues.large!.targetCommentId);
            expect(reached, "scrolled to the notified comment").toBe(true);
            await settle(page, 1500);
          }
        }
        const tabIds = await page.locator("[data-tab-id]").evaluateAll((els) => els.map((el) => el.getAttribute("data-tab-id")!));
        expect(tabIds, "one tab per destination").toHaveLength(tabs.length);
        const switches: Record<string, unknown>[] = [];
        for (let cycle = 0; cycle < TAB_CYCLES; cycle++) {
          // PERF_TRACE_DIR records the last cycle of the last round: a Chrome trace and a CPU profile.
          const trace = TRACE_DIR && round === REPEATS - 1 && cycle === TAB_CYCLES - 1 ? await startTrace(desktop) : null;
          for (const [index, tab] of tabs.entries()) {
            const frame = page.locator(`[data-tab-id="${tabIds[index]}"]`);
            const step = await measureStep(desktop, () => frame.click(), tab);
            switches.push({ cycle, tab: tab.key, ...step });
          }
          await trace?.(`${TRACE_DIR}/${target.name}-tabs`);
        }
        expect(desktop.errors).toEqual([]);
        if (round >= 0) {
          const record = { target: target.name, round, opens, switches, load_1m: loadavg()[0] };
          report.tabs.push(record);
          console.log(JSON.stringify({ target: target.name, round, ready_ms: switches.map((s) => `${s.tab}:${s.ready_ms}`).join(" "), task_ms: switches.map((s) => s.task_ms).join(" ") }));
        }
      } finally {
        await desktop.close();
      }
    }
  }
});

test("click Work panel Inbox rows on Desktop", async () => {
  test.skip(!runInbox, "inbox scenario not selected");
  test.setTimeout(3_600_000);
  const sequence: { key: string; phase: "first-visit" | "revisit" }[] = manifest.inbox.slice(0, 20).map((n) => ({ key: n.key, phase: "first-visit" }));
  for (let i = 0; i < 10; i++) sequence.push({ key: manifest.inbox[i % 5]!.key, phase: "revisit" });
  for (let round = -WARMUP; round < REPEATS; round++) {
    for (const target of round % 2 ? TARGETS : [...TARGETS].reverse()) {
      await resetInbox(manifest);
      const desktop = await launchDesktop(target.dir, target.name);
      try {
        const { page } = desktop;
        const rows = page.locator("li[data-inbox-row]");
        await expect.poll(() => rows.count(), "the Inbox section shows the twenty measured rows").toBeGreaterThanOrEqual(20);
        await settle(page, 2000);
        const switches: Record<string, unknown>[] = [];
        for (const [step, { key, phase }] of sequence.entries()) {
          const issue = manifest.issues[key]!;
          const tab: TabSpec = { key, path: "", kind: "issue", title: issue.title };
          const result = await measureStep(desktop, () => rows.filter({ hasText: issue.title }).locator("a").first().click(), tab);
          switches.push({ step, phase, issue: key, comments: issue.comments, ...result });
        }
        expect(desktop.errors).toEqual([]);
        if (round >= 0) {
          report.inbox.push({ target: target.name, round, switches, load_1m: loadavg()[0] });
          console.log(JSON.stringify({ target: target.name, round, ready_ms: switches.map((s) => s.ready_ms).join(" ") }));
        }
      } finally {
        await desktop.close();
      }
    }
  }
});

type Row = Record<string, unknown>;
const nums = (rows: Row[], key: string) => rows.map((r) => r[key]).filter((v): v is number => typeof v === "number");
const round1 = (v: number) => Math.round(v * 10) / 10;

function stepSummary(rows: Row[]) {
  const ready = nums(rows, "ready_ms");
  const sources = new Map<string, number>();
  for (const line of rows.flatMap((r) => r.top_sources as string[])) {
    const [ms, ...rest] = line.split(" ");
    sources.set(rest.join(" "), (sources.get(rest.join(" ")) ?? 0) + Number.parseInt(ms!, 10));
  }
  return {
    steps: rows.length,
    ready_median_ms: median(ready), ready_p95_ms: round1(quantile(ready, 0.95)), ready_max_ms: Math.max(...ready),
    next_paint_median_ms: median(nums(rows, "next_paint_ms")), next_paint_max_ms: Math.max(...nums(rows, "next_paint_ms")),
    task_median_ms: median(nums(rows, "task_ms")),
    stalls_per_step: round1(nums(rows, "stalls").reduce((a, b) => a + b, 0) / rows.length),
    stall_max_ms: Math.max(...nums(rows, "stall_max_ms")),
    freezes: nums(rows, "freezes").reduce((a, b) => a + b, 0),
    top_sources: [...sources.entries()].sort((a, b) => b[1] - a[1]).slice(0, 4).map(([s, ms]) => `${ms}ms ${s}`),
  };
}

/** Per target: tab switches overall and per destination; Inbox first visits and revisits. */
function summarize() {
  const out: Record<string, unknown> = {};
  for (const { name } of TARGETS) {
    const switches = report.tabs.filter((r) => r.target === name).flatMap((r) => r.switches as Row[]);
    const inbox = report.inbox.filter((r) => r.target === name).flatMap((r) => r.switches as Row[]);
    out[name] = {
      tabs: switches.length === 0 ? null : {
        all: stepSummary(switches),
        by_tab: Object.fromEntries(tabSet().map((t) => [t.key, stepSummary(switches.filter((s) => s.tab === t.key))])),
      },
      inbox: inbox.length === 0 ? null : {
        "first-visit": stepSummary(inbox.filter((s) => s.phase === "first-visit")),
        revisit: stepSummary(inbox.filter((s) => s.phase === "revisit")),
      },
    };
  }
  return out;
}
