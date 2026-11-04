import { expect, test, type Locator, type Page } from "@playwright/test";

// Real production UI, synthetic API/WS only. Run with CHAT_LIST_BASELINE=1
// against the unmodified build to measure the same workload without the
// window-size assertions. Never send these fixtures to a real backend.
const baseline = process.env.CHAT_LIST_BASELINE === "1";
const count = 2000;
const wsId = "11111111-1111-4111-8111-111111111111";
const userId = "22222222-2222-4222-8222-222222222222";
const agentId = "33333333-3333-4333-8333-333333333333";
const at = "2026-01-01T00:00:00Z";
const projectId = "55555555-5555-4555-8555-555555555555";
const otherProjectId = "55555555-5555-4555-8555-555555555556";
const projects = [projectId, otherProjectId].map((id, i) => ({
  id, workspace_id: wsId, title: `Project ${i === 0 ? "Alpha" : "Beta"}`,
  description: null, status: "planned", priority: "none", lead_id: null,
  lead_type: null, start_date: null, target_date: null,
  created_at: at, updated_at: at,
}));
const workspace = {
  id: wsId, name: "Perf", slug: "perf", description: null, context: null,
  settings: { github_enabled: false }, repos: [], issue_prefix: "PERF",
  avatar_url: null, created_at: at, updated_at: at,
};

function sessions(archived: boolean, size = count) {
  return Array.from({ length: size }, (_, i) => ({
    id: `${archived ? "bbbb" : "aaaa"}${String(i).padStart(4, "0")}-4444-4444-8444-444444444444`,
    workspace_id: wsId, agent_id: agentId, creator_id: userId,
    title: `${archived ? "Archived" : "Session"} ${String(i).padStart(4, "0")}`,
    status: archived ? "archived" : "active", pinned: false,
    has_unread: false, unread_count: 0, last_message: null,
    created_at: at, updated_at: new Date(Date.parse(at) - i * 1000).toISOString(),
  }));
}

test.use({ viewport: { width: 1280, height: 800 }, locale: "en-US", serviceWorkers: "block" });

function responseFor(path: string, rows: ReturnType<typeof sessions>, mixedProjects = false): unknown {
  switch (path) {
    case "/api/config": return { feature_flags: {}, allow_signup: true };
    case "/api/me": return {
      id: userId, name: "Perf User", email: "perf@example.test", avatar_url: null,
      language: "en", onboarded_at: at, onboarding_questionnaire: { source: "other" },
      created_at: at, updated_at: at,
    };
    case "/api/workspaces": return [workspace];
    case "/api/client-usage": return {};
    case "/api/chat/sessions": return rows;
    case "/api/chat/pending-tasks": return { tasks: [] };
    case "/api/chat/pending-tasks/has-any": return { has_pending: false };
    case "/api/projects": return { projects: mixedProjects ? projects : [], total: mixedProjects ? 2 : 0 };
    case "/api/issue-view-preferences": return { scope_type: "workspace", scope_id: null,
      ordered_view_ids: [], hidden_view_ids: [], default_view_id: null };
    case "/api/issue-workflows": return { workflows: [] };
    case "/api/autopilots": return { autopilots: [], total: 0 };
    case "/api/issues": return { issues: [], total: 0 };
    case "/api/issue-statuses": return { statuses: [] };
    case "/api/properties": return { properties: [], total: 0 };
    case "/api/quick-actions": return { quick_actions: [] };
    case "/api/search-index/manifest": return {
      cursor: "0", issue_count: 0, comment_count: 0, project_count: 0, text_bytes: 0,
    };
    case "/api/search-index/snapshot": return {
      issues: [], comments: [], projects: [], next_after_number: 0, done: true,
    };
    case "/api/search-index/changes": return {
      issues: [], comments: [], projects: [], deleted: { issues: [], comments: [], projects: [] },
      cursor: "0", has_more: false,
    };
    case `/api/workspaces/${wsId}/members`:
    case "/api/agents": case "/api/runtimes": case "/api/squads":
    case "/api/agent-task-snapshot": case "/api/invitations": case "/api/inbox":
    case "/api/inbox/unread-summary": case "/api/pins": case "/api/assignee-frequency":
    case "/api/skills": case "/api/issue-views": case "/api/issue-wakeup-summaries":
    case "/api/agent-run-counts": case "/api/agent-activity-30d":
      return [];
  }
  const match = /^\/api\/chat\/sessions\/([^/]+)(.*)$/.exec(path);
  if (!match) return undefined;
  const session = rows.find((row) => row.id === match[1]);
  if (!session) return undefined;
  switch (match[2]) {
    case "/messages/page": return { messages: [], limit: 50, has_more: false, next_cursor: null };
    case "/messages": return [];
    case "/pending-task": case "/read": return {};
    case "/draft-restores": return { restores: [] };
    case "": return session;
    default: return undefined;
  }
}

async function setup(page: Page, baseURL: string, withArchive: boolean, size = count, mixedProjects = false, failSessions = false) {
  const origin = new URL(baseURL).origin;
  const rows = [...sessions(false, size), ...(withArchive ? sessions(true, size) : [])];
  if (mixedProjects) rows.forEach((row, i) => Object.assign(row, { project_id: i % 2 ? otherProjectId : projectId }));
  const errors: string[] = [];
  const unexpected: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.context().addCookies([
    { name: "multica_logged_in", value: "1", url: origin },
    { name: "last_workspace_slug", value: "perf", url: origin },
  ]);
  await page.addInitScript(() => {
    localStorage.setItem("multica:chat:floatingChatEnabled", "true");
  });
  await page.routeWebSocket(() => true, (ws) => { ws.onMessage(() => {}); });
  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    if (url.origin !== origin) {
      unexpected.push(url.origin + url.pathname);
      return route.abort();
    }
    if (!url.pathname.startsWith("/api/")) return route.continue();
    if (url.pathname === "/api/chat/sessions" && failSessions) {
      return route.fulfill({ status: 503, contentType: "application/json", body: "{}" });
    }
    const body = responseFor(url.pathname, rows, mixedProjects);
    if (body === undefined) {
      unexpected.push(`${route.request().method()} ${url.pathname}${url.search}`);
      return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
    }
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
  });
  return { errors, unexpected, recover: () => { failSessions = false; } };
}

// Find the actual scroll owner, so both the original full list and the
// virtualized list receive the same user-visible scroll operation.
async function scrollToEnd(row: Locator) {
  await row.evaluate((el) => {
    for (let parent = el.parentElement; parent; parent = parent.parentElement) {
      if (parent.scrollHeight > parent.clientHeight && /auto|scroll/.test(getComputedStyle(parent).overflowY)) {
        parent.scrollTop = parent.scrollHeight;
        return;
      }
    }
    throw new Error("No scrollable list ancestor");
  });
}

async function record(page: Page, surface: string, started: number, rows: Locator) {
  await page.evaluate(() => new Promise<void>((done) => requestAnimationFrame(() => requestAnimationFrame(() => done()))));
  const mounted = await rows.count();
  const report = { surface, baseline, sessions: count, mounted_rows: mounted, ready_ms: Date.now() - started };
  await test.info().attach(surface, { body: JSON.stringify(report, null, 2), contentType: "application/json" });
  // Like the other performance scenario, emit measurements for the comparison log.
  console.log(JSON.stringify(report));
  if (!baseline) expect(mounted).toBeLessThan(40);
  else expect(mounted).toBe(count);
}

for (const withArchive of [false, true]) {
  test(`chat history with${withArchive ? "" : "out"} an archive footer`, async ({ page, baseURL }) => {
    const state = await setup(page, baseURL!, withArchive);
    const started = Date.now();
    await page.goto("/perf/chat");
    const first = page.getByText("Session 0000", { exact: true });
    await expect(first).toBeVisible();
    await record(page, `history-${withArchive}`, started, page.getByText(/^Session \d{4}$/));
    await page.screenshot({ path: test.info().outputPath("history.png") });
    await scrollToEnd(first);
    const last = page.getByText("Session 1999", { exact: true });
    await expect(last).toBeVisible();
    await last.locator("xpath=ancestor::*[@tabindex='0'][1]").press("Enter");
    await expect(page).toHaveURL(/session=aaaa1999/);
    if (withArchive) {
      const archiveStarted = Date.now();
      await page.getByRole("button", { name: /Archived.*2000/ }).click();
      const archived = page.getByText("Archived 0000", { exact: true });
      await expect(archived).toBeVisible();
      await record(page, "archived", archiveStarted, page.getByText(/^Archived \d{4}$/));
      await scrollToEnd(archived);
      await expect(page.getByText("Archived 1999", { exact: true })).toBeVisible();
    }
    expect(state.errors).toEqual([]);
    expect(state.unexpected).toEqual([]);
  });
}

test("floating chat history scrolls to an offscreen session", async ({ page, baseURL }) => {
  const state = await setup(page, baseURL!, false);
  await page.addInitScript(() => localStorage.setItem("multica:chat:isOpen", "true"));
  await page.goto("/perf/inbox");
  const started = Date.now();
  await page.getByRole("button", { name: "New chat", exact: true }).click();
  const history = page.getByRole("group", { name: "Chat history" });
  await expect(history).toBeVisible();
  await record(page, "dropdown", started, history.getByText(/^Session \d{4}$/));
  await page.screenshot({ path: test.info().outputPath("dropdown.png") });
  await scrollToEnd(history.getByText("Session 0000", { exact: true }));
  const last = history.getByText("Session 1999", { exact: true });
  await expect(last).toBeVisible();
  await last.click();
  await expect(history).not.toBeVisible();
  await expect(page.getByRole("button", { name: "Session 1999", exact: true })).toBeVisible();
  expect(state.errors).toEqual([]);
  expect(state.unexpected).toEqual([]);
});

test("compact chat list can select its last session", async ({ page, baseURL }) => {
  const state = await setup(page, baseURL!, false);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/perf/chat");
  const first = page.getByText("Session 0000", { exact: true });
  await expect(first).toBeVisible();
  await scrollToEnd(first);
  await page.getByText("Session 1999", { exact: true }).click();
  await expect(page).toHaveURL(/session=aaaa1999/);
  expect(state.errors).toEqual([]);
  expect(state.unexpected).toEqual([]);
});

for (const size of [0, 1]) {
  test(`floating history with ${size} sessions`, async ({ page, baseURL }) => {
    const state = await setup(page, baseURL!, false, size);
    await page.addInitScript(() => localStorage.setItem("multica:chat:isOpen", "true"));
    await page.goto("/perf/inbox");
    await page.getByRole("button", { name: "New chat", exact: true }).click();
    if (size === 0) {
      await expect(page.getByText("No previous chats", { exact: true })).toBeVisible();
    } else {
      const history = page.getByRole("group", { name: "Chat history" });
      await expect(history.getByText("Session 0000", { exact: true })).toBeVisible();
      const bounds = await history.boundingBox();
      expect(bounds!.height).toBeGreaterThan(44);
      expect(bounds!.height).toBeLessThan(160);
    }
    expect(state.errors).toEqual([]);
    expect(state.unexpected).toEqual([]);
  });
}

for (const compact of [false, true]) {
  test(`project filter preserves windowing and archive footer${compact ? " at 390px" : ""}`, async ({ page, baseURL }) => {
    const state = await setup(page, baseURL!, true, count, true);
    if (compact) await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/perf/chat");
    const first = page.getByText("Session 0000", { exact: true });
    await expect(first).toBeVisible();
    await page.getByRole("combobox", { name: "Project", exact: true }).click();
    await page.getByRole("option", { name: "Project Alpha", exact: true }).click();
    await expect(page.getByText("Session 0001", { exact: true })).toHaveCount(0);
    const mounted = page.getByText(/^Session \d{4}$/);
    expect(await mounted.count()).toBeLessThan(40);
    await scrollToEnd(first);
    await expect(page.getByText("Session 1998", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: /Archived.*1000/ }).click();
    const archived = page.getByText("Archived 0000", { exact: true });
    await expect(archived).toBeVisible();
    expect(await page.getByText(/^Archived \d{4}$/).count()).toBeLessThan(40);
    await scrollToEnd(archived);
    await expect(page.getByText("Archived 1998", { exact: true })).toBeVisible();
    await page.screenshot({ path: test.info().outputPath("filtered-archive.png") });
    await page.getByRole("button", { name: "Clear project filter" }).click();
    await expect(page.getByRole("combobox", { name: "Project", exact: true })).toContainText("All projects");
    expect(state.errors).toEqual([]);
    expect(state.unexpected).toEqual([]);
  });
}

for (const compact of [false, true]) {
  test(`chat list recovers from an error into a bounded virtual list${compact ? " at 390px" : ""}`, async ({ page, baseURL }) => {
    const state = await setup(page, baseURL!, true, count, true, true);
    if (compact) await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/perf/chat");
    await expect(page.getByRole("status").filter({ hasText: "Loading chats" })).toBeVisible();
    await expect(page.getByRole("alert").filter({ hasText: "Couldn't load chats" })).toBeVisible({ timeout: 20_000 });
    state.recover();
    await page.getByRole("button", { name: "Retry", exact: true }).click();
    const first = page.getByText("Session 0000", { exact: true });
    await expect(first).toBeVisible();
    expect(await page.getByText(/^Session \d{4}$/).count()).toBeLessThan(40);
    await scrollToEnd(first);
    await expect(page.getByText("Session 1999", { exact: true })).toBeVisible();
    expect(state.errors).toEqual([]);
    expect(state.unexpected).toEqual([]);
  });
}

test("collapsing and reopening the list retains its project filter", async ({ page, baseURL }) => {
  const state = await setup(page, baseURL!, true, count, true);
  await page.goto("/perf/chat");
  const filter = page.getByRole("combobox", { name: "Project", exact: true });
  await filter.click();
  await page.getByRole("option", { name: "Project Alpha", exact: true }).click();
  await scrollToEnd(page.getByText("Session 0000", { exact: true }));
  await expect(page.getByText("Session 1998", { exact: true })).toBeVisible();
  await page.locator('[data-slot="space-panel-resize"]').click();
  const column = page.locator('[data-slot="list-column"]');
  await expect(column).toHaveAttribute("data-state", "collapsed");
  await page.locator('[data-slot="sidebar-trigger"]:not([inert] *)').last().click();
  await expect(column).toHaveAttribute("data-state", "expanded");
  await expect(filter).toContainText("Project Alpha");
  await expect(page.getByText("Session 1998", { exact: true })).toBeVisible();
  await page.screenshot({ path: test.info().outputPath("reopened-filter.png") });
  expect(state.errors).toEqual([]);
  expect(state.unexpected).toEqual([]);
});

for (const compact of [false, true]) {
  test(`empty chat keeps the no-agent guidance${compact ? " at 390px" : ""}`, async ({ page, baseURL }) => {
    const state = await setup(page, baseURL!, false, 0);
    if (compact) await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/perf/chat");
    await expect(page.getByText("No agent to chat with yet", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "New agent", exact: true })).toBeVisible();
    await page.screenshot({ path: test.info().outputPath("no-agent.png") });
    expect(state.errors).toEqual([]);
    expect(state.unexpected).toEqual([]);
  });
}
