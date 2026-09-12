/**
 * Comment typing under live agent runs (MUL-7227).
 *
 * #8172 keyed the run summary by message seq, so every streamed message
 * replaced a DOM node and the page paid a style recalculation for it. Typing a
 * comment on a long thread went from seconds to tens of seconds. #8260 restored
 * a stable node, and its unit test pins that the node is reused — but a jsdom
 * test cannot see style recalculation, layout, or main-thread contention. This
 * scenario measures what the user actually waits for.
 *
 * The whole page is real: app shell, sidebar, shared CSS, the ProseMirror
 * composer, inline runs. Only the HTTP and WebSocket boundaries are synthetic,
 * so no API, database, daemon or account is involved. Nothing here disables
 * animations or trims the DOM to reduce noise — the cost being measured lives
 * in exactly that machinery.
 *
 * The result is a report, not a gate: one sample per build cannot separate a
 * small regression from machine noise. The test still fails outright when the
 * scenario did not happen — messages that never reached the UI or a composer
 * that did not receive every keystroke must never read as "fast".
 */
import { expect, test, type Page, type WebSocketRoute } from "@playwright/test";
import { writeFileSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import * as fx from "./fixtures/typing-under-live-runs";

/** Playwright's own cap. The scenario's validity cap is SCENARIO_LIMIT_MS. */
test.setTimeout(300_000);
/** Beyond this the sample is reported as a timeout rather than as a duration. */
const SCENARIO_LIMIT_MS = 60_000;
/** Loading, seeding and focusing share one budget, kept clear of the scenario's. */
const SETUP_LIMIT_MS = 120_000;

test.use({
  viewport: { width: 1280, height: 720 },
  locale: "en-US",
  colorScheme: "light",
  // A worker could serve responses the route table never saw.
  serviceWorkers: "block",
});

const json = (body: unknown) => ({
  status: 200,
  contentType: "application/json",
  body: JSON.stringify(body),
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
  identifier: fx.ISSUE_IDENTIFIER, title: "Typing under live runs",
  description: "Performance scenario fixture.", status: "in_progress",
  status_category: "in_progress", status_name: "In Progress", priority: "none",
  assignee_type: null, assignee_id: null, creator_type: "user", creator_id: fx.USER_ID,
  parent_issue_id: null, project_id: null, position: 0,
  start_date: null, due_date: null, stage: null, metadata: {}, properties: {},
  labels: [], reactions: [],
  created_at: "2026-09-01T00:00:00Z", updated_at: "2026-09-01T00:00:00Z",
};

/**
 * Every request the page is allowed to make. Anything else invalidates the
 * sample: a business call answered by a catch-all would quietly change what is
 * being measured.
 */
function responseFor(pathname: string): unknown | undefined {
  const messages = /^\/api\/tasks\/([0-9a-f-]+)\/messages$/.exec(pathname);
  if (messages) {
    const index = fx.runIds.indexOf(messages[1]!);
    return index >= 0 ? fx.buildInitialMessages(index) : undefined;
  }
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
    // Typing debounces a trigger preview. It is part of the cost being
    // measured, so it is answered rather than blocked.
    case `/api/issues/${fx.ISSUE_ID}/comments/trigger-preview`:
      return { agents: [], blocked: [] };
    case "/api/issues/child-progress": return { progress: [] };
    case `/api/issues/${fx.ISSUE_IDENTIFIER}`:
    case `/api/issues/${fx.ISSUE_ID}`: return issue;
    case `/api/issues/${fx.ISSUE_ID}/timeline`: return fx.buildTimelineEntries();
    case `/api/issues/${fx.ISSUE_ID}/task-runs`: return fx.buildTaskRuns();
    case `/api/issues/${fx.ISSUE_ID}/subscribers`:
    case `/api/issues/${fx.ISSUE_ID}/attachments`: return [];
    case `/api/issues/${fx.ISSUE_ID}/labels`: return { labels: [] };
    case `/api/issues/${fx.ISSUE_ID}/children`: return { issues: [] };
    case `/api/issues/${fx.ISSUE_ID}/pull-requests`: return { pull_requests: [] };
    default: return undefined;
  }
}

const nextFrame = (page: Page) =>
  page.evaluate(() => new Promise<void>((done) => {
    requestAnimationFrame(() => requestAnimationFrame(() => done()));
  }));

/** Writes the report wherever the run can see it, whatever the outcome. */
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

test("comment typing stays responsive while runs stream", async ({ page, context, baseURL }) => {
  const origin = new URL(baseURL ?? "http://localhost:3000").origin;
  const unknownRequests: string[] = [];
  const pageErrors: string[] = [];
  const external: string[] = [];

  page.on("pageerror", (error) => pageErrors.push(error.message));
  // Motion must be on: the regression this scenario exists for is skipped
  // under reduced motion, so measuring with it enabled would measure nothing.
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "no-preference" });

  await context.addCookies([
    { name: "multica_logged_in", value: "1", url: origin },
    { name: "last_workspace_slug", value: fx.WORKSPACE_SLUG, url: origin },
  ]);
  // The floating chat mounts its own queries on every page; this scenario is
  // about the issue thread, and those requests are not part of it.
  await context.addInitScript(() => {
    window.localStorage.setItem("multica:chat:floatingChatEnabled", "false");
  });
  // Long tasks are read from the browser's own observer rather than a
  // threshold invented here. Records still queued at the end are drained.
  await context.addInitScript(() => {
    const store: { duration: number }[] = [];
    (window as unknown as { __perfLongTasks: typeof store }).__perfLongTasks = store;
    const observer = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) store.push({ duration: entry.duration });
    });
    observer.observe({ type: "longtask", buffered: true });
    (window as unknown as { __perfDrain: () => void }).__perfDrain = () => {
      for (const entry of observer.takeRecords()) store.push({ duration: entry.duration });
    };
  });

  await context.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    if (url.origin !== origin) {
      external.push(url.href);
      return route.abort();
    }
    if (!url.pathname.startsWith("/api/")) return route.continue();
    const body = responseFor(url.pathname);
    if (body === undefined) {
      unknownRequests.push(`${route.request().method()} ${url.pathname}${url.search}`);
      return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
    }
    return route.fulfill(json(body));
  });

  let socket!: WebSocketRoute;
  let onSocket!: () => void;
  const socketReady = new Promise<void>((done) => { onSocket = done; });
  await context.routeWebSocket((url) => url.pathname === "/ws", (ws) => {
    socket = ws;
    // Cookie mode: the client sends nothing and treats open as authenticated.
    ws.onMessage(() => {});
    onSocket();
  });

  const push = (runIndex: number, tick: number) => {
    for (const payload of fx.buildTickMessages(runIndex, tick)) {
      socket.send(JSON.stringify({
        type: "task:message", payload, actor_id: "", actor_type: "system",
      }));
    }
  };

  const setupDeadline = Date.now() + SETUP_LIMIT_MS;
  const setupLeft = () => Math.max(1, setupDeadline - Date.now());
  const failSetup = (stage: string) => {
    emit({
      status: "invalid",
      invalid: [`setup did not complete: ${stage}`],
      fixture: fx.fixtureDigest(),
      page_errors: pageErrors,
      unmocked_requests: unknownRequests,
      typing_elapsed_ms: null,
      scenario_elapsed_ms: null,
    });
  };

  // ---- 1. Load, and prove the fixture is actually on screen -----------------
  await page.goto(
    `${origin}/${fx.WORKSPACE_SLUG}/issues/${fx.ISSUE_IDENTIFIER}#comment-${fx.rootCommentId}`,
    { waitUntil: "domcontentloaded", timeout: setupLeft() },
  );
  // A page that error-boundaries never opens a socket; without a bound this
  // wait would hang past every budget below it.
  await Promise.race([
    socketReady,
    new Promise<void>((_, reject) =>
      setTimeout(() => reject(new Error("the app never opened a WebSocket")), setupLeft()),
    ),
  ]).catch((error: Error) => {
    failSetup(`${error.message}${pageErrors.length ? ` | ${pageErrors.join(" | ")}` : ""}`);
    throw error;
  });

  const summaries = page.locator("[data-run-summary]");
  try {
    await expect(summaries).toHaveCount(fx.RUN_COUNT, { timeout: setupLeft() });
  } catch (error) { failSetup("run summaries never rendered"); throw error; }
  const bodies = page.locator("[data-comment-block]");
  try {
    await expect
      .poll(() => bodies.count(), { timeout: setupLeft() })
      .toBeGreaterThanOrEqual(fx.ROOT_COMMENT_COUNT + fx.REPLY_COMMENT_COUNT);
  } catch (error) { failSetup("comment bodies never rendered"); throw error; }
  // Activity detail stays collapsed: the row shows a summary, not a step list.
  await expect(page.locator("[data-run-summary] >> nth=0")).toBeVisible();
  await page.evaluate(() => document.fonts?.ready);
  await nextFrame(page);

  // ---- 2. Preparation: prove messages reach the real render path ------------
  for (let runIndex = 0; runIndex < fx.RUN_COUNT; runIndex += 1) push(runIndex, 0);
  try {
    for (let runIndex = 0; runIndex < fx.RUN_COUNT; runIndex += 1) {
      await expect(summaries.nth(runIndex))
        .toHaveText(fx.commandForTick(0), { timeout: setupLeft() });
    }
  } catch (error) { failSetup("streamed message never reached a summary"); throw error; }

  // ---- 3. Focus the composer -----------------------------------------------
  const shell = page.getByTestId("comment-composer-shell");
  await expect(shell).toBeVisible();
  // Resolve the composer's container before activating it: the shell is removed
  // once the real editor mounts, and the issue description is a second editable
  // editor on this page — keystrokes must be proven to land in the composer.
  const composer = await page
    .locator('div:has(> [data-testid="comment-composer-shell"])')
    .first()
    .elementHandle();
  if (!composer) throw new Error("comment composer container not found");
  await shell.click();
  const editor = await composer.waitForSelector('.ProseMirror[contenteditable="true"]', {
    timeout: setupLeft(),
  });
  expect(await composer.$$('.ProseMirror[contenteditable="true"]')).toHaveLength(1);
  await editor.click();
  expect(await editor.evaluate((node) => node === document.activeElement)).toBe(true);
  expect((await editor.innerText()).trim()).toBe("");

  const cdp = await context.newCDPSession(page);
  await cdp.send("Performance.enable");
  const readMetrics = async () => {
    const { metrics } = await cdp.send("Performance.getMetrics");
    return Object.fromEntries(metrics.map((m) => [m.name, m.value]));
  };
  const before = await readMetrics();
  await page.evaluate(() => {
    (window as unknown as { __perfLongTasks: unknown[] }).__perfLongTasks.length = 0;
  });

  // ---- 4. Stream and type at the same time ---------------------------------
  // The schedule lives in Node: a stalling page must not be allowed to slow the
  // message rate down and measure less work than the other build.
  const startedAt = Date.now();
  const sentAt: number[] = [];
  const streaming = (async () => {
    for (let tick = 1; tick < fx.TICK_COUNT; tick += 1) {
      const due = startedAt + tick * fx.TICK_INTERVAL_MS;
      const wait = due - Date.now();
      if (wait > 0) await new Promise((done) => setTimeout(done, wait));
      for (let runIndex = 0; runIndex < fx.RUN_COUNT; runIndex += 1) push(runIndex, tick);
      sentAt.push(Date.now() - startedAt);
    }
  })();
  const typing = page.keyboard.type(fx.TYPING_TEXT, { delay: fx.KEY_DELAY_MS });
  // A build slow enough to blow the budget must still produce a report saying
  // so. Letting Playwright's own timeout kill the run would leave the reader
  // with no measurement at all, which is the one outcome a regression must not
  // be allowed to produce.
  streaming.catch(() => {});
  typing.catch(() => {});
  const remainingMs = () => Math.max(0, startedAt + SCENARIO_LIMIT_MS - Date.now());
  let timedOutAt: string | null = null;
  async function beforeDeadline<T>(work: Promise<T>, label: string): Promise<boolean> {
    const budget = remainingMs();
    if (budget <= 0) { timedOutAt ??= label; return false; }
    let timer: NodeJS.Timeout | undefined;
    const expired = new Promise<false>((done) => {
      timer = setTimeout(() => done(false), budget);
    });
    const finished = await Promise.race([work.then(() => true, () => false), expired]);
    clearTimeout(timer);
    if (!finished) timedOutAt ??= label;
    return finished;
  }

  let typingElapsedMs: number | null = null;
  if (await beforeDeadline(typing, "keystrokes")) {
    // The wait is over only once the editor holds every character.
    const verified = await beforeDeadline(
      expect
        .poll(async () => (await editor.innerText()).trim(), { timeout: remainingMs() })
        .toBe(fx.TYPING_TEXT),
      "editor content",
    );
    if (verified) {
      await beforeDeadline(nextFrame(page), "paint");
      typingElapsedMs = Date.now() - startedAt;
    }
  }

  let scenarioElapsedMs: number | null = null;
  if (!timedOutAt && await beforeDeadline(streaming, "stream")) {
    let settled = true;
    for (let runIndex = 0; runIndex < fx.RUN_COUNT && settled; runIndex += 1) {
      settled = await beforeDeadline(
        expect(summaries.nth(runIndex))
          .toHaveText(fx.endMarkerFor(runIndex), { timeout: remainingMs() }),
        `run ${runIndex} end marker`,
      );
    }
    if (settled) {
      await beforeDeadline(nextFrame(page), "final paint");
      scenarioElapsedMs = Date.now() - startedAt;
    }
  }
  const abortedAfterMs = timedOutAt ? Date.now() - startedAt : null;

  // ---- 5. Collect --------------------------------------------------------
  // A jammed renderer cannot answer `evaluate` either, so collection gets its
  // own bounded budget and reports nothing rather than hanging.
  const COLLECT_BUDGET_MS = 20_000;
  const collect = async <T>(work: Promise<T>, fallback: T): Promise<T> => {
    let timer: NodeJS.Timeout | undefined;
    const expired = new Promise<T>((done) => {
      timer = setTimeout(() => done(fallback), COLLECT_BUDGET_MS);
    });
    const value = await Promise.race([work.catch(() => fallback), expired]);
    clearTimeout(timer);
    return value;
  };
  await collect(
    page.evaluate(() => (window as unknown as { __perfDrain: () => void }).__perfDrain()),
    undefined,
  );
  const longTasks = await collect(
    page.evaluate(
      () => (window as unknown as { __perfLongTasks: { duration: number }[] }).__perfLongTasks,
    ),
    [] as { duration: number }[],
  );
  const after = await collect(readMetrics(), {} as Record<string, number>);
  const metricsRead = Object.keys(after).length > 0;
  const deltaMs = (name: string) =>
    metricsRead ? Math.round(((after[name] ?? 0) - (before[name] ?? 0)) * 1000) : null;
  const domNodes = metricsRead ? Math.round(after.Nodes ?? 0) : null;

  const invalid: string[] = [];
  if (unknownRequests.length) invalid.push(`unmocked requests: ${unknownRequests.join(", ")}`);
  if (external.length) invalid.push(`external requests: ${external.join(", ")}`);
  if (pageErrors.length) invalid.push(`page errors: ${pageErrors.join(" | ")}`);
  if (!timedOutAt && sentAt.length !== fx.TICK_COUNT - 1) {
    invalid.push(`sent ${sentAt.length} ticks`);
  }
  if (!timedOutAt && !metricsRead) invalid.push("performance metrics unavailable");
  const status = timedOutAt ? "timeout" : invalid.length ? "invalid" : "ok";

  const report = {
    status, invalid,
    timed_out_at: timedOutAt,
    aborted_after_ms: abortedAfterMs,
    fixture: fx.fixtureDigest(),
    typing_elapsed_ms: typingElapsedMs,
    scenario_elapsed_ms: scenarioElapsedMs,
    recalc_style_ms: deltaMs("RecalcStyleDuration"),
    layout_ms: deltaMs("LayoutDuration"),
    task_ms: deltaMs("TaskDuration"),
    long_task_count: longTasks.length,
    long_task_total_ms: Math.round(longTasks.reduce((sum, t) => sum + t.duration, 0)),
    long_task_max_ms: Math.round(Math.max(0, ...longTasks.map((t) => t.duration))),
    long_tasks_observed: longTasks.length > 0 || metricsRead,
    dom_nodes: domNodes,
    ticks_sent: sentAt.length + 1,
    stream_span_ms: sentAt.length ? sentAt[sentAt.length - 1]! : 0,
    typed_chars: fx.TYPING_TEXT.length,
    node_version: process.version,
    browser_version: page.context().browser()?.version() ?? "unknown",
  };

  emit(report);

  expect(invalid, invalid.join(" | ")).toEqual([]);
  expect(
    timedOutAt,
    `scenario exceeded ${SCENARIO_LIMIT_MS}ms while waiting for ${timedOutAt}`,
  ).toBeNull();
});
