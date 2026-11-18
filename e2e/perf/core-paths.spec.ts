/**
 * Core-path benchmark (MUL-9539): the two paths people report as slow, on the
 * production UI against a real local API and a "large workspace" dataset.
 *
 * 1. `issue`  — open a big issue directly, scroll its history top to bottom
 *    while the images load, then toggle the left nav and the right sidebar.
 * 2. `inbox`  — click through twenty Inbox rows in the Work panel, then go
 *    back and forth between rows already visited.
 *
 * Every step reports two things: how long until the requested content is on
 * screen, and how the page stalled on the way (Long Animation Frames). See
 * `lib/frame-metrics.ts` for the exact clock and `lib/core-dataset.ts` for the
 * data. Opt in with PERF_SCENARIO=core-paths (both) or core-issue/core-inbox.
 */
import { expect, test, type Browser, type BrowserContext, type CDPSession, type Page } from "@playwright/test";
import { mkdirSync, writeFileSync } from "node:fs";
import { cpus, loadavg } from "node:os";
import { dirname } from "node:path";
import {
  installFrameMetrics, interactionLatency, lastClick, lastInput, median, pageNow, quantile, settle, stallsBetween,
  type StallSummary,
} from "./lib/frame-metrics";
import { DETAIL_ISSUES, ensureDataset, perfApi, resetInbox, type Manifest } from "./lib/core-dataset";

const scenario = process.env.PERF_SCENARIO ?? "";
const runIssue = scenario === "core-paths" || scenario === "core-issue";
const runInbox = scenario === "core-paths" || scenario === "core-inbox";
const REPEATS = Number(process.env.PERF_REPEATS ?? 5);
/** Unmeasured rounds first: they warm the API, database and OS caches. */
const WARMUP = Number(process.env.PERF_WARMUP ?? 1);
const VIEWPORT = { width: 1440, height: 900 };
/** Idle time after content appears, so the tail of the work lands in the window. */
const LOAD_TAIL_MS = 3000;
const SWITCH_DWELL_MS = 1000;
const SIDEBAR_TOGGLES = 10;
const SCROLL_PX_PER_FRAME = 120;
/**
 * CPU slowdown for the whole sample (CDP). 1 is the machine as is; 4 stands
 * in for a mid-range laptop, where the work an M-series Mac absorbs shows.
 */
const CPU_THROTTLE = Number(process.env.PERF_CPU_THROTTLE ?? 1);
/**
 * How many Inbox rows the Work panel shows: `20`, the largest section limit
 * a user can pick, or `all`, after "Show N more" mounts every row.
 */
const INBOX_ROWS = process.env.PERF_INBOX_ROWS === "all" ? "all" : "20";
/**
 * Where an Inbox switch ends: the notified comment in view (default), or just
 * the issue's title, for refs that do not scroll to the comment (before #10872).
 */
const INBOX_UNTIL = process.env.PERF_INBOX_UNTIL === "title" ? "title" : "comment";

test.describe.configure({ mode: "serial" });

let manifest: Manifest;
let token: string;
const report: { issue: Record<string, unknown>[]; inbox: Record<string, unknown>[] } = { issue: [], inbox: [] };

test.beforeAll(async () => {
  test.skip(!runIssue && !runInbox, "set PERF_SCENARIO=core-paths (or core-issue / core-inbox)");
  test.setTimeout(900_000);
  expect(process.env.MULTICA_DEV_PROFILE, "run through `make env-exec` against an isolated environment").toMatch(/^dev-/);
  const api = await perfApi();
  token = api.getToken()!;
  manifest = await ensureDataset(api);
});

test.afterAll(async ({ browser }) => {
  if (!manifest || !process.env.PERF_REPORT_PATH) return;
  const path = process.env.PERF_REPORT_PATH;
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, JSON.stringify({
    ref: process.env.PERF_REF ?? null,
    label: process.env.PERF_LABEL ?? null,
    dataset: { version: manifest.version, digest: manifest.digest, counts: manifest.counts },
    environment: {
      browser: browser.version(), viewport: `${VIEWPORT.width}x${VIEWPORT.height}`, cpu_throttle: CPU_THROTTLE,
      cpu: cpus()[0]?.model, cores: cpus().length, load_1m_end: loadavg()[0], repeats: REPEATS,
      inbox_rows: INBOX_ROWS, inbox_until: INBOX_UNTIL,
    },
    summary: summarize(),
    samples: report,
  }, null, 2));
});

async function openContext(browser: Browser): Promise<{ context: BrowserContext; page: Page; cdp: CDPSession; errors: string[] }> {
  const context = await browser.newContext({ viewport: VIEWPORT, locale: "en-US", colorScheme: "light" });
  const page = await context.newPage();
  await installFrameMetrics(page, token);
  const cdp = await context.newCDPSession(page);
  await cdp.send("Performance.enable");
  if (CPU_THROTTLE > 1) await cdp.send("Emulation.setCPUThrottlingRate", { rate: CPU_THROTTLE });
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  return { context, page, cdp, errors };
}

/** Main-thread task time so far (ms), from the renderer's own counters. */
async function taskTime(cdp: CDPSession): Promise<number> {
  const { metrics } = await cdp.send("Performance.getMetrics");
  return (metrics.find((m) => m.name === "TaskDuration")?.value ?? 0) * 1000;
}

type Toggle = { next_paint_ms: number; task_ms: number; stalls: StallSummary; open: boolean };

function summarizeToggles(toggles: Toggle[]) {
  return {
    next_paint_median_ms: median(toggles.map((t) => t.next_paint_ms)),
    next_paint_max_ms: Math.max(...toggles.map((t) => t.next_paint_ms)),
    task_median_ms: Math.round(median(toggles.map((t) => t.task_ms))),
    stalls: toggles.reduce((sum, t) => sum + t.stalls.stalls, 0),
    stall_max_ms: Math.max(...toggles.map((t) => t.stalls.stall_max_ms)),
    top_sources: toggles.flatMap((t) => t.stalls.top_sources).slice(0, 3),
  };
}

const detailSelector = '[data-mc="issue-detail"][data-view="page"]';

/** Resolves with page-clock marks once the issue's title and the target comment are on screen. */
async function waitForIssue(page: Page, title: string, commentId: string, mode: "mounted" | "in-view" | "title") {
  const handle = await page.waitForFunction(({ title, commentId, mode, detailSelector }) => {
    const marks = window.__corePerf.marks;
    const details = [...document.querySelectorAll(detailSelector)];
    const detail = details.find((d) => d.querySelector('[data-mc="issue-title"]')?.textContent?.includes(title));
    if (!detail) return false;
    marks.title ??= performance.now();
    if (mode === "title") return { title: marks.title, content: marks.title };
    const root = detail.querySelector<HTMLElement>("[data-tab-scroll-root]");
    const target = root?.querySelector<HTMLElement>(`[data-comment-content="${commentId}"]`);
    if (!root || !target || !target.textContent?.trim()) return false;
    const row = target.closest("[data-virtual-thread-mounted]");
    if (row?.getAttribute("data-virtual-thread-mounted") === "false") return false;
    const box = target.getBoundingClientRect();
    if (box.height === 0) return false;
    if (mode === "in-view") {
      const view = root.getBoundingClientRect();
      if (!(box.bottom > view.top && box.top < view.bottom)) return false;
    }
    marks.content ??= performance.now();
    return { title: marks.title, content: marks.content };
  }, { title, commentId, mode, detailSelector }, { polling: "raf", timeout: 60_000 });
  return (await handle.jsonValue()) as { title: number; content: number };
}

test("open, scroll and toggle the sidebar on a big issue", async ({ browser }) => {
  test.skip(!runIssue, "issue scenario not selected");
  test.setTimeout(1_800_000);
  const keys = DETAIL_ISSUES.filter((key) => !process.env.PERF_ISSUES || process.env.PERF_ISSUES.split(",").includes(key));
  for (const key of keys) {
    const issue = manifest.issues[key]!;
    for (let round = -WARMUP; round < REPEATS; round++) {
      const { context, page, cdp, errors } = await openContext(browser);
      try {
        let apiRequests = 0;
        let imagesFetched = 0;
        page.on("request", (request) => { if (new URL(request.url()).pathname.startsWith("/api/")) apiRequests++; });
        page.on("requestfinished", (request) => { if (/\/api\/attachments\/[^/]+\/download/.test(request.url())) imagesFetched++; });
        await page.goto(`/${manifest.workspace.slug}/issues/${issue.id}`, { waitUntil: "commit" });
        const marks = await waitForIssue(page, issue.title, issue.firstCommentId, "mounted");
        await settle(page, LOAD_TAIL_MS);
        const load = await stallsBetween(page, 0, await pageNow(page));
        const requestsAtLoad = apiRequests;
        const imagesAtLoad = imagesFetched;

        // Scroll the history top to bottom at a steady pace; images load and decode on the way.
        const scrollStart = await pageNow(page);
        const scroll = await page.evaluate(async ({ detailSelector, step, lastId }) => {
          const root = document.querySelector<HTMLElement>(`${detailSelector} [data-tab-scroll-root]`)!;
          const start = performance.now();
          let bottomFrames = 0;
          while (performance.now() - start < 90_000 && bottomFrames < 30) {
            await new Promise((done) => requestAnimationFrame(done));
            root.scrollTop += step;
            bottomFrames = root.scrollTop + root.clientHeight >= root.scrollHeight - 2 ? bottomFrames + 1 : 0;
          }
          return {
            ms: performance.now() - start,
            height: root.scrollHeight,
            reachedEnd: !!root.querySelector(`[data-comment-content="${lastId}"]`),
          };
        }, { detailSelector, step: SCROLL_PX_PER_FRAME, lastId: issue.lastCommentId });
        await settle(page, 1000);
        const scrollStalls = await stallsBetween(page, scrollStart, await pageNow(page));
        expect(scroll.reachedEnd, "scrolling reached the last comment").toBe(true);

        // Toggle the left nav and the right sidebar with the reader in the middle of the
        // history, where the most rows are mounted; each toggle is one interaction.
        await page.evaluate((sel) => {
          const root = document.querySelector<HTMLElement>(`${sel} [data-tab-scroll-root]`)!;
          root.scrollTop = Math.round(root.scrollHeight / 2);
        }, detailSelector);
        await settle(page, 1500);
        const panels = {
          left: {
            act: () => page.keyboard.press("ControlOrMeta+b"),
            open: () => page.evaluate(() => document.querySelector('[data-slot="space-panel"][data-state]')?.getAttribute("data-state") === "expanded"),
          },
          right: {
            act: () => page.locator(detailSelector).getByRole("button", { name: "Toggle right sidebar" }).click(),
            open: () => page.evaluate(() => (document.querySelector("[data-right-sidebar-panel]")?.getBoundingClientRect().width ?? 0) > 1),
          },
        };
        const sidebar: Record<string, ReturnType<typeof summarizeToggles>> = {};
        for (const [name, panel] of Object.entries(panels)) {
          const toggles: Toggle[] = [];
          for (let i = 0; i < SIDEBAR_TOGGLES; i++) {
            const workBefore = await taskTime(cdp);
            await panel.act();
            await settle(page, 600);
            const input = await lastInput(page);
            const end = await pageNow(page);
            toggles.push({
              // Event Timing only reports interactions of 16 ms or more.
              next_paint_ms: (await interactionLatency(page, input, end)) ?? 16,
              task_ms: (await taskTime(cdp)) - workBefore,
              stalls: await stallsBetween(page, input - 1, end),
              open: await panel.open(),
            });
          }
          expect(new Set(toggles.map((t) => t.open)).size, `the ${name} sidebar both collapsed and expanded`).toBe(2);
          sidebar[name] = summarizeToggles(toggles);
        }
        expect(errors).toEqual([]);

        if (round >= 0) {
          const record = {
            issue: key, round,
            title_ms: Math.round(marks.title), content_ms: Math.round(marks.content),
            load, api_requests_at_load: requestsAtLoad,
            images_fetched_at_load: imagesAtLoad,
            scroll: { ms: Math.round(scroll.ms), height_px: scroll.height, images_fetched: imagesFetched - imagesAtLoad, ...scrollStalls },
            sidebar,
            load_1m: loadavg()[0],
          };
          report.issue.push(record);
          console.log(JSON.stringify(record));
        }
      } finally {
        await context.close();
      }
    }
  }
});

test("switch between Inbox rows in the Work panel", async ({ browser }) => {
  test.skip(!runInbox, "inbox scenario not selected");
  test.setTimeout(1_800_000);
  const sequence: { key: string; phase: "first-visit" | "revisit" }[] = manifest.inbox.slice(0, 20).map((n) => ({ key: n.key, phase: "first-visit" }));
  for (let i = 0; i < 10; i++) sequence.push({ key: manifest.inbox[i % 5]!.key, phase: "revisit" });

  for (let round = -WARMUP; round < REPEATS; round++) {
    await resetInbox(manifest);
    const { context, page, errors } = await openContext(browser);
    try {
      await page.addInitScript((slug) => {
        localStorage.setItem(`multica_sidebar_tree:${slug}`, JSON.stringify({ state: { inboxSettings: { limit: 20 } }, version: 0 }));
      }, manifest.workspace.slug);
      await page.goto(`/${manifest.workspace.slug}/issues`);
      const rows = page.locator("li[data-inbox-row]");
      await expect(rows.first()).toBeVisible({ timeout: 60_000 });
      const firstTitle = manifest.issues[manifest.inbox[0]!.key]!.title;
      await expect(rows.filter({ hasText: firstTitle })).toHaveCount(1);
      if (INBOX_ROWS === "all") {
        const showMore = page.getByRole("button", { name: /^Show \d+ more/ }).first();
        const hidden = Number((await showMore.textContent())?.match(/\d+/)?.[0] ?? 0);
        expect(hidden, "the Inbox section hides hundreds of rows").toBeGreaterThan(500);
        await showMore.click();
        // A virtualized list mounts only the rows near the viewport, so the
        // expansion is read from the toggle rather than from the row count.
        await expect(page.getByRole("button", { name: "Show fewer" }).first()).toHaveAttribute("aria-expanded", "true");
      }
      await expect.poll(() => rows.count(), "the Inbox section shows the twenty measured rows").toBeGreaterThanOrEqual(20);
      await settle(page, 2000);
      const mountedRows = await rows.count();

      const switches: Record<string, unknown>[] = [];
      for (const [step, { key, phase }] of sequence.entries()) {
        const issue = manifest.issues[key]!;
        await page.evaluate(() => { window.__corePerf.marks = {}; });
        await rows.filter({ hasText: issue.title }).locator("a").first().click();
        const marks = await waitForIssue(page, issue.title, issue.targetCommentId, INBOX_UNTIL === "title" ? "title" : "in-view");
        const click = await lastClick(page);
        await settle(page, SWITCH_DWELL_MS);
        const end = await pageNow(page);
        const stalls = await stallsBetween(page, click - 1, end);
        switches.push({
          step, phase, issue: key, comments: issue.comments,
          next_paint_ms: (await interactionLatency(page, click, end)) ?? 16,
          title_ms: Math.round(marks.title - click), ready_ms: Math.round(marks.content - click), ...stalls,
        });
      }
      const session = await context.newCDPSession(page);
      await session.send("HeapProfiler.collectGarbage");
      const heap = (await session.send("Performance.enable"), await session.send("Performance.getMetrics"))
        .metrics.find((m) => m.name === "JSHeapUsedSize")?.value ?? 0;
      const domNodes = await page.evaluate(() => document.getElementsByTagName("*").length);
      expect(errors).toEqual([]);
      if (round >= 0) {
        const record = { round, inbox_rows: INBOX_ROWS, mounted_rows: mountedRows, switches, heap_mb_after_gc: Math.round(heap / 1e6), dom_nodes_end: domNodes, load_1m: loadavg()[0] };
        report.inbox.push(record);
        console.log(JSON.stringify({ round, ready_ms: switches.map((s) => s.ready_ms), heap_mb_after_gc: record.heap_mb_after_gc }));
      }
    } finally {
      await context.close();
    }
  }
});

type Num = number;
const pick = (records: Record<string, unknown>[], path: string): Num[] =>
  records.map((r) => path.split(".").reduce<unknown>((v, k) => (v as Record<string, unknown>)?.[k], r) as Num).filter((v) => typeof v === "number");
const round1 = (v: number) => Math.round(v * 10) / 10;

/** Medians across samples; p95/max across individual switches. */
function summarize() {
  const issue: Record<string, unknown> = {};
  for (const key of DETAIL_ISSUES) {
    const rs = report.issue.filter((r) => r.issue === key);
    if (rs.length === 0) continue;
    issue[key] = {
      open_content_ms: median(pick(rs, "content_ms")),
      open_title_ms: median(pick(rs, "title_ms")),
      open_stalls: median(pick(rs, "load.stalls")),
      open_stall_max_ms: median(pick(rs, "load.stall_max_ms")),
      open_blocking_ms: median(pick(rs, "load.blocking_ms")),
      scroll_stalls: median(pick(rs, "scroll.stalls")),
      scroll_stall_max_ms: median(pick(rs, "scroll.stall_max_ms")),
      scroll_blocking_ms: median(pick(rs, "scroll.blocking_ms")),
      ...Object.fromEntries(["left", "right"].flatMap((side) => [
        [`${side}_toggle_next_paint_ms`, median(pick(rs, `sidebar.${side}.next_paint_median_ms`))],
        [`${side}_toggle_next_paint_max_ms`, median(pick(rs, `sidebar.${side}.next_paint_max_ms`))],
        [`${side}_toggle_task_ms`, median(pick(rs, `sidebar.${side}.task_median_ms`))],
        [`${side}_toggle_stalls`, median(pick(rs, `sidebar.${side}.stalls`))],
        [`${side}_toggle_stall_max_ms`, median(pick(rs, `sidebar.${side}.stall_max_ms`))],
      ])),
    };
  }
  const inbox: Record<string, unknown> = {};
  for (const phase of ["first-visit", "revisit"]) {
    const all = report.inbox.flatMap((r) => (r.switches as Record<string, unknown>[]).filter((s) => s.phase === phase));
    if (all.length === 0) continue;
    const ready = pick(all, "ready_ms");
    inbox[phase] = {
      switches: all.length,
      ready_median_ms: median(ready), ready_p95_ms: round1(quantile(ready, 0.95)), ready_max_ms: Math.max(...ready),
      next_paint_median_ms: median(pick(all, "next_paint_ms")), next_paint_max_ms: Math.max(...pick(all, "next_paint_ms")),
      stalls_per_switch: round1(pick(all, "stalls").reduce((a, b) => a + b, 0) / all.length),
      stall_max_ms: Math.max(...pick(all, "stall_max_ms")),
      freezes: pick(all, "freezes").reduce((a, b) => a + b, 0),
    };
  }
  if (report.inbox.length > 0) {
    inbox.heap_mb_after_gc = median(pick(report.inbox, "heap_mb_after_gc"));
    const sources = new Map<string, number>();
    for (const s of report.inbox.flatMap((r) => r.switches as { top_sources: string[] }[])) {
      for (const line of s.top_sources) {
        const [ms, ...rest] = line.split(" ");
        sources.set(rest.join(" "), (sources.get(rest.join(" ")) ?? 0) + Number.parseInt(ms!, 10));
      }
    }
    inbox.top_sources = [...sources.entries()].sort((a, b) => b[1] - a[1]).slice(0, 5).map(([s, ms]) => `${ms}ms ${s}`);
  }
  return { issue, inbox };
}
