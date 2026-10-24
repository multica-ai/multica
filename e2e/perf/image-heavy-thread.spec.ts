/**
 * Comment typing beside an image-heavy thread (MUL-8182).
 *
 * A long thread of replies that each carry a large screenshot sits in the
 * viewport while the reader types a comment. Every inline image that mounts
 * is fetched, decoded at full size and laid out on the main thread, and an
 * image without a reserved box shifts everything below it when it lands.
 *
 * Image responses are held until the first keystroke, so the images arrive,
 * decode and lay out while the user is typing — the moment the report
 * measures. Like the live-runs scenario, only HTTP and the WebSocket are
 * synthetic; the page, the composer and the CSS are the real build.
 *
 * Select it with `PERF_SCENARIO=image-thread` (perf-compare's
 * `--scenario image-thread` sets it): the live-runs scenario writes the same
 * report path, and the two must not overwrite each other.
 */
import { expect, test, type Page } from "@playwright/test";
import { writeFileSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import * as fx from "./fixtures/image-heavy-thread";

test.skip(process.env.PERF_SCENARIO !== "image-thread", "set PERF_SCENARIO=image-thread to run");
test.setTimeout(300_000);
const SCENARIO_LIMIT_MS = 60_000;
const SETUP_LIMIT_MS = 120_000;
/** CPU slowdown for the measured window only (setup runs at full speed). */
const CPU_THROTTLE = 4;

test.use({
  viewport: { width: 1280, height: 720 },
  locale: "en-US",
  colorScheme: "light",
  serviceWorkers: "block",
});

const workspace = {
  id: fx.WORKSPACE_ID, name: "Perf", slug: fx.WORKSPACE_SLUG,
  description: null, context: null,
  settings: { github_enabled: false }, repos: [],
  issue_prefix: "PERF", avatar_url: null,
  created_at: "2026-09-01T00:00:00Z", updated_at: "2026-09-01T00:00:00Z",
};

const me = {
  id: fx.USER_ID, name: "Perf User", email: "perf@example.test", avatar_url: null,
  language: null, onboarded_at: "2026-09-01T00:00:00Z",
  onboarding_questionnaire: { source: "other" },
  created_at: "2026-09-01T00:00:00Z", updated_at: "2026-09-01T00:00:00Z",
};

const issue = {
  id: fx.ISSUE_ID, workspace_id: fx.WORKSPACE_ID, number: 1,
  identifier: fx.ISSUE_IDENTIFIER, title: "Image-heavy thread",
  description: "Performance scenario fixture.", status: "in_progress",
  status_category: "in_progress", status_name: "In Progress", priority: "none",
  assignee_type: null, assignee_id: null, creator_type: "user", creator_id: fx.USER_ID,
  parent_issue_id: null, project_id: null, position: 0,
  start_date: null, due_date: null, stage: null, metadata: {}, properties: {},
  labels: [], reactions: [],
  created_at: "2026-09-01T00:00:00Z", updated_at: "2026-09-01T00:00:00Z",
};

function responseFor(pathname: string): unknown | undefined {
  switch (pathname) {
    case "/api/config": return { feature_flags: {}, allow_signup: true };
    case "/api/me": return me;
    case "/api/workspaces": return [workspace];
    case "/api/client-usage": return {};
    case "/api/agents": case "/api/runtimes": case "/api/squads":
    case "/api/agent-task-snapshot": case "/api/invitations": case "/api/inbox":
    case "/api/inbox/unread-summary": case "/api/pins": case "/api/chat/sessions":
      return [];
    case "/api/chat/pending-tasks/has-any": return { has_pending: false };
    case `/api/workspaces/${fx.WORKSPACE_ID}/members`: return [];
    case "/api/projects": return { projects: [], total: 0 };
    case "/api/issues": return { issues: [], total: 0 };
    case "/api/issue-statuses": return { statuses: [] };
    case "/api/properties": return { properties: [], total: 0 };
    case "/api/quick-actions": return { quick_actions: [] };
    case "/api/assignee-frequency": return [];
    case "/api/labels": return { labels: [], total: 0 };
    case "/api/issue-workflows": return { workflows: [], total: 0 };
    case `/api/issues/${fx.ISSUE_ID}/system-wakeups`: return [];
    case `/api/issues/${fx.ISSUE_ID}/duplicates`: return { duplicate_of: null, duplicates: [] };
    case `/api/issues/${fx.ISSUE_ID}/comments/trigger-preview`:
      return { agents: [], blocked: [] };
    case "/api/issues/child-progress": return { progress: [] };
    case `/api/issues/${fx.ISSUE_IDENTIFIER}`:
    case `/api/issues/${fx.ISSUE_ID}`: return issue;
    case `/api/issues/${fx.ISSUE_ID}/timeline`: return fx.buildTimelineEntries();
    case `/api/issues/${fx.ISSUE_ID}/task-runs`:
    case `/api/issues/${fx.ISSUE_ID}/subscribers`:
    case `/api/issues/${fx.ISSUE_ID}/wakeups`:
    case `/api/issues/${fx.ISSUE_ID}/attachments`: return [];
    case `/api/issues/${fx.ISSUE_ID}/labels`: return { labels: [] };
    case `/api/issues/${fx.ISSUE_ID}/children`: return { issues: [] };
    case `/api/issues/${fx.ISSUE_ID}/pull-requests`: return { pull_requests: [] };
    default: return undefined;
  }
}

/**
 * The local search copy syncs in the background on every page and is not part
 * of this one's cost, so it is declined rather than seeded.
 */
const DECLINED = new Set(["/api/search-index/manifest"]);

const nextFrame = (page: Page) =>
  page.evaluate(() => new Promise<void>((done) => {
    requestAnimationFrame(() => requestAnimationFrame(() => done()));
  }));

function emit(report: Record<string, unknown>) {
  const out = process.env.PERF_REPORT_PATH;
  if (out) {
    mkdirSync(dirname(resolve(out)), { recursive: true });
    writeFileSync(resolve(out), JSON.stringify(report, null, 2));
  }
  test.info().annotations.push({ type: "perf", description: JSON.stringify(report) });
  // eslint-disable-next-line no-console
  console.log(`PERF_REPORT ${JSON.stringify(report)}`);
}

test("comment typing stays responsive beside an image-heavy thread", async ({ page, context, baseURL }) => {
  const origin = new URL(baseURL ?? "http://localhost:3000").origin;
  const unknownRequests: string[] = [];
  const pageErrors: string[] = [];
  const external: string[] = [];
  const served = { originals: 0, previews: 0, bytes: 0 };
  const media = fx.mediaBytes();

  page.on("pageerror", (error) => pageErrors.push(error.message));
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "no-preference" });
  await context.addCookies([
    { name: "multica_logged_in", value: "1", url: origin },
    { name: "last_workspace_slug", value: fx.WORKSPACE_SLUG, url: origin },
  ]);
  await context.addInitScript(() => {
    window.localStorage.setItem("multica:chat:floatingChatEnabled", "false");
  });
  await context.addInitScript(() => {
    const tasks: number[] = [];
    let shift = 0;
    const w = window as unknown as Record<string, unknown>;
    w.__perfLongTasks = tasks;
    w.__perfShift = () => shift;
    w.__perfReset = () => { tasks.length = 0; shift = 0; };
    const longTasks = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) tasks.push(entry.duration);
    });
    longTasks.observe({ type: "longtask", buffered: true });
    // Typing is recent input, so `hadRecentInput` would excuse every shift in
    // the window; the reader still sees each one.
    const shifts = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) shift += (entry as unknown as { value: number }).value;
    });
    shifts.observe({ type: "layout-shift", buffered: true });
    w.__perfDrain = () => {
      for (const entry of longTasks.takeRecords()) tasks.push(entry.duration);
      for (const entry of shifts.takeRecords()) shift += (entry as unknown as { value: number }).value;
    };
  });

  let releaseMedia!: () => void;
  const mediaGate = new Promise<void>((done) => { releaseMedia = done; });
  await context.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    if (url.origin !== origin) {
      external.push(url.href);
      return route.abort();
    }
    if (url.pathname.startsWith(fx.MEDIA_PREFIX)) {
      await mediaGate;
      const preview = url.pathname.endsWith(".preview.png");
      const body = preview ? media.preview : media.original;
      if (preview) served.previews += 1; else served.originals += 1;
      served.bytes += body.length;
      return route.fulfill({ status: 200, contentType: "image/png", body });
    }
    if (!url.pathname.startsWith("/api/")) return route.continue();
    if (DECLINED.has(url.pathname)) {
      return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
    }
    const body = responseFor(url.pathname);
    if (body === undefined) {
      unknownRequests.push(`${route.request().method()} ${url.pathname}${url.search}`);
      return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
    }
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
  });
  await context.routeWebSocket((url) => url.pathname === "/ws", (ws) => {
    ws.onMessage(() => {});
  });

  const setupDeadline = Date.now() + SETUP_LIMIT_MS;
  const setupLeft = () => Math.max(1, setupDeadline - Date.now());
  const failSetup = (stage: string) => emit({
    status: "invalid",
    invalid: [`setup did not complete: ${stage}`],
    fixture: fx.fixtureDigest(),
    page_errors: pageErrors,
    unmocked_requests: unknownRequests,
    typing_elapsed_ms: null,
    scenario_elapsed_ms: null,
  });

  // ---- 1. Load, and put the thread in the viewport ---------------------------
  await page.goto(`${origin}/${fx.WORKSPACE_SLUG}/issues/${fx.ISSUE_IDENTIFIER}`, {
    waitUntil: "domcontentloaded", timeout: setupLeft(),
  });
  const root = page.locator(`[id="comment-${fx.rootCommentId}"]`);
  try {
    await expect(root).toBeVisible({ timeout: setupLeft() });
    // Every build mounts at least the root and the latest reply.
    await expect(page.locator(`[id="comment-${fx.commentIds.at(-1)}"]`)).toBeAttached({ timeout: setupLeft() });
  } catch (error) { failSetup("the thread never rendered"); throw error; }
  await page.evaluate(() => document.fonts?.ready);
  // Scroll the thread's replies into view: the scroll parent is the nearest
  // ancestor that scrolls.
  await root.evaluate((el) => {
    let scroller: HTMLElement | null = el.parentElement;
    while (scroller && !/(auto|scroll)/.test(getComputedStyle(scroller).overflowY)) {
      scroller = scroller.parentElement;
    }
    if (!scroller) throw new Error("no scroll container");
    const offset = el.getBoundingClientRect().top - scroller.getBoundingClientRect().top;
    scroller.scrollTop += offset + 200;
  });
  await nextFrame(page);

  // ---- 2. Focus the composer -------------------------------------------------
  const shell = page.getByTestId("comment-composer-shell");
  await expect(shell).toBeVisible();
  const composer = await page
    .locator('div:has(> [data-testid="comment-composer-shell"])')
    .first()
    .elementHandle();
  if (!composer) throw new Error("comment composer container not found");
  await shell.click();
  const editor = await composer.waitForSelector('.ProseMirror[contenteditable="true"]', {
    timeout: setupLeft(),
  });
  await editor.click();
  expect(await editor.evaluate((node) => node === document.activeElement)).toBe(true);
  expect((await editor.innerText()).trim()).toBe("");

  const cdp = await context.newCDPSession(page);
  await cdp.send("Performance.enable");
  const readMetrics = async () => {
    const { metrics } = await cdp.send("Performance.getMetrics");
    return Object.fromEntries(metrics.map((m) => [m.name, m.value]));
  };
  // A fast workstation absorbs this page without a dropped frame; the stall
  // was reported from ordinary laptops. Same factor on both builds.
  await cdp.send("Emulation.setCPUThrottlingRate", { rate: CPU_THROTTLE });
  const before = await readMetrics();
  await page.evaluate(() => (window as unknown as { __perfReset: () => void }).__perfReset());

  // ---- 3. Release the images and type at the same time -----------------------
  const startedAt = Date.now();
  releaseMedia();
  const typing = page.keyboard.type(fx.TYPING_TEXT, { delay: fx.KEY_DELAY_MS });
  typing.catch(() => {});
  const remainingMs = () => Math.max(0, startedAt + SCENARIO_LIMIT_MS - Date.now());
  let timedOutAt: string | null = null;
  async function beforeDeadline<T>(work: Promise<T>, label: string): Promise<boolean> {
    const budget = remainingMs();
    if (budget <= 0) { timedOutAt ??= label; return false; }
    let timer: NodeJS.Timeout | undefined;
    const expired = new Promise<false>((done) => { timer = setTimeout(() => done(false), budget); });
    const finished = await Promise.race([work.then(() => true, () => false), expired]);
    clearTimeout(timer);
    if (!finished) timedOutAt ??= label;
    return finished;
  }

  let typingElapsedMs: number | null = null;
  if (await beforeDeadline(typing, "keystrokes")) {
    const verified = await beforeDeadline(
      expect.poll(async () => (await editor.innerText()).trim(), { timeout: remainingMs() })
        .toBe(fx.TYPING_TEXT),
      "editor content",
    );
    if (verified) {
      await beforeDeadline(nextFrame(page), "paint");
      typingElapsedMs = Date.now() - startedAt;
    }
  }

  // Settled once every image that is in or near the viewport has painted.
  let scenarioElapsedMs: number | null = null;
  if (!timedOutAt) {
    const settled = await beforeDeadline(
      page.waitForFunction((prefix) => [...document.images]
        .filter((img) => img.currentSrc.includes(prefix))
        .filter((img) => {
          const r = img.getBoundingClientRect();
          return r.bottom > -800 && r.top < window.innerHeight + 800;
        })
        .every((img) => img.complete), fx.MEDIA_PREFIX, { timeout: remainingMs() }),
      "images",
    );
    if (settled) {
      await beforeDeadline(nextFrame(page), "final paint");
      scenarioElapsedMs = Date.now() - startedAt;
    }
  }
  const abortedAfterMs = timedOutAt ? Date.now() - startedAt : null;

  // ---- 4. Collect ------------------------------------------------------------
  const COLLECT_BUDGET_MS = 20_000;
  const collect = async <T>(work: Promise<T>, fallback: T): Promise<T> => {
    let timer: NodeJS.Timeout | undefined;
    const expired = new Promise<T>((done) => { timer = setTimeout(() => done(fallback), COLLECT_BUDGET_MS); });
    const value = await Promise.race([work.catch(() => fallback), expired]);
    clearTimeout(timer);
    return value;
  };
  const observed = await collect(page.evaluate((prefix) => {
    const w = window as unknown as {
      __perfDrain: () => void; __perfLongTasks: number[]; __perfShift: () => number;
    };
    w.__perfDrain();
    return {
      longTasks: [...w.__perfLongTasks],
      shift: w.__perfShift(),
      mounted: [...document.images].filter((img) => img.currentSrc.includes(prefix)).length,
    };
  }, fx.MEDIA_PREFIX), { longTasks: [] as number[], shift: 0, mounted: 0 });
  const after = await collect(readMetrics(), {} as Record<string, number>);
  const metricsRead = Object.keys(after).length > 0;
  const deltaMs = (name: string) =>
    metricsRead ? Math.round(((after[name] ?? 0) - (before[name] ?? 0)) * 1000) : null;

  const invalid: string[] = [];
  if (unknownRequests.length) invalid.push(`unmocked requests: ${unknownRequests.join(", ")}`);
  if (external.length) invalid.push(`external requests: ${external.join(", ")}`);
  if (pageErrors.length) invalid.push(`page errors: ${pageErrors.join(" | ")}`);
  if (!timedOutAt && !metricsRead) invalid.push("performance metrics unavailable");
  if (!timedOutAt && served.originals + served.previews === 0) invalid.push("no image was requested");
  const status = timedOutAt ? "timeout" : invalid.length ? "invalid" : "ok";

  const longTasks = observed.longTasks;
  emit({
    status, invalid,
    scenario: "image-thread",
    timed_out_at: timedOutAt,
    aborted_after_ms: abortedAfterMs,
    fixture: fx.fixtureDigest(),
    typing_elapsed_ms: typingElapsedMs,
    scenario_elapsed_ms: scenarioElapsedMs,
    recalc_style_ms: deltaMs("RecalcStyleDuration"),
    layout_ms: deltaMs("LayoutDuration"),
    task_ms: deltaMs("TaskDuration"),
    long_task_count: longTasks.length,
    long_task_total_ms: Math.round(longTasks.reduce((sum, t) => sum + t, 0)),
    long_task_max_ms: Math.round(Math.max(0, ...longTasks)),
    layout_shift: Math.round(observed.shift * 1000) / 1000,
    images_mounted: observed.mounted,
    originals_served: served.originals,
    previews_served: served.previews,
    image_bytes_served: served.bytes,
    dom_nodes: metricsRead ? Math.round(after.Nodes ?? 0) : null,
    js_heap_mb: metricsRead ? Math.round((after.JSHeapUsedSize ?? 0) / 1e6) : null,
    cpu_throttle: CPU_THROTTLE,
    typed_chars: fx.TYPING_TEXT.length,
    node_version: process.version,
    browser_version: page.context().browser()?.version() ?? "unknown",
  });

  expect(invalid, invalid.join(" | ")).toEqual([]);
  expect(timedOutAt, `scenario exceeded ${SCENARIO_LIMIT_MS}ms while waiting for ${timedOutAt}`).toBeNull();
});
