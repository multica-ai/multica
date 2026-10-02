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

function responseFor(path: string, rows: ReturnType<typeof sessions>): unknown {
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
    case "/api/projects": return { projects: [], total: 0 };
    case "/api/issues": return { issues: [], total: 0 };
    case "/api/issue-statuses": return { statuses: [] };
    case "/api/properties": return { properties: [], total: 0 };
    case "/api/quick-actions": return { quick_actions: [] };
    case `/api/workspaces/${wsId}/members`:
    case "/api/agents": case "/api/runtimes": case "/api/squads":
    case "/api/agent-task-snapshot": case "/api/invitations": case "/api/inbox":
    case "/api/inbox/unread-summary": case "/api/pins": case "/api/assignee-frequency":
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

async function setup(page: Page, baseURL: string, withArchive: boolean, size = count) {
  const origin = new URL(baseURL).origin;
  const rows = [...sessions(false, size), ...(withArchive ? sessions(true, size) : [])];
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
    const body = responseFor(url.pathname, rows);
    if (body === undefined) {
      unexpected.push(`${route.request().method()} ${url.pathname}${url.search}`);
      return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
    }
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) });
  });
  return { errors, unexpected };
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
