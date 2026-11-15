import { expect, test, type Page } from "@playwright/test";

// Real shared renderer, synthetic HTTP/WS only. No API server, account or run
// execution. Use the same scenario on main with AGENT_WORK_BASELINE=1.
const baseline = process.env.AGENT_WORK_BASELINE === "1";
const count = 1000;
const wsId = "11111111-1111-4111-8111-111111111111";
const userId = "22222222-2222-4222-8222-222222222222";
const agentId = "33333333-3333-4333-8333-333333333333";
const at = "2026-11-01T00:00:00Z";
const workspace = {
  id: wsId, name: "Perf", slug: "perf", description: null, context: null,
  settings: { github_enabled: false }, repos: [], issue_prefix: "PERF",
  avatar_url: null, created_at: at, updated_at: at,
};
const agent = {
  id: agentId, workspace_id: wsId, runtime_id: null, name: "Demo agent",
  description: "Synthetic recent work", instructions: "", avatar_url: null,
  runtime_mode: "local", runtime_config: {}, custom_args: [],
  visibility: "workspace", permission_mode: "public_to",
  invocation_targets: [{ target_type: "workspace", target_id: null }],
  status: "idle", max_concurrent_tasks: 1, model: "", owner_id: userId, skills: [],
  created_at: at, updated_at: at, archived_at: null, archived_by: null,
};
const tasks = Array.from({ length: count }, (_, i) => ({
  id: `aaaa${String(i).padStart(4, "0")}-4444-4444-8444-444444444444`,
  agent_id: agentId, runtime_id: "", issue_id: `PERF-${i + 1}`,
  status: i % 7 === 0 ? "failed" : "completed", priority: 0,
  started_at: new Date(Date.parse(at) - i * 60_000 - 12_000).toISOString(),
  completed_at: new Date(Date.parse(at) - i * 60_000).toISOString(),
  created_at: new Date(Date.parse(at) - i * 60_000 - 15_000).toISOString(),
  trigger_summary: `Synthetic run ${i + 1}`,
}));

function responseFor(path: string): unknown {
  switch (path) {
    case "/api/config": return { feature_flags: {}, allow_signup: true };
    case "/api/me": return {
      id: userId, name: "Demo User", email: "demo@example.test", avatar_url: null,
      language: "en", onboarded_at: at, onboarding_questionnaire: { source: "other" },
      created_at: at, updated_at: at,
    };
    case "/api/workspaces": return [workspace];
    case "/api/agents": return [agent];
    case `/api/agents/${agentId}`: return agent;
    case "/api/agent-activity-30d": return [{
      agent_id: agentId, bucket_at: at, task_count: count,
      completed_count: 857, failed_count: 143, cancelled_count: 0,
      duration_ms: count * 12_000, duration_count: count,
    }];
    case "/api/client-usage": return {};
    case "/api/projects": return { projects: [], total: 0 };
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
    case `/api/workspaces/${wsId}/members`: return [{
      id: userId, user_id: userId, workspace_id: wsId, role: "owner", name: "Demo User",
      email: "demo@example.test", avatar_url: null, joined_at: at,
    }];
    case "/api/chat/sessions": case "/api/runtimes": case "/api/squads":
    case "/api/agent-task-snapshot": case "/api/invitations": case "/api/inbox":
    case "/api/inbox/unread-summary": case "/api/pins": case "/api/assignee-frequency":
    case "/api/skills": case "/api/autopilots": case "/api/agent-run-counts":
      return [];
    case "/api/chat/pending-tasks/has-any": return { has_pending: false };
  }
  if (new RegExp(`^/api/workspaces/${wsId}/(lark|slack|dingtalk|wecom|telegram)/installations$`).test(path)) {
    return { installations: [], configured: false };
  }
  const issue = /^\/api\/issues\/PERF-(\d+)$/.exec(path);
  if (issue) return {
    id: `PERF-${issue[1]}`, workspace_id: wsId, identifier: `PERF-${issue[1]}`,
    number: Number(issue[1]), title: `Synthetic work ${String(issue[1]).padStart(4, "0")}`,
    description: null, status: "done", priority: "none", assignee_type: null, assignee_id: null,
    creator_type: "user", creator_id: userId, parent_issue_id: null, project_id: null,
    position: 0, start_date: null, due_date: null, metadata: {}, properties: {},
    created_at: at, updated_at: at,
  };
  const messages = /^\/api\/tasks\/(aaaa\d{4}-4444-4444-8444-444444444444)\/messages$/.exec(path);
  if (messages) return [{
    task_id: messages[1], issue_id: "", seq: 1, type: "text",
    content: `Synthetic transcript for run ${Number(messages[1]!.slice(4, 8)) + 1}.`, created_at: at,
  }];
  return undefined;
}

async function setup(page: Page, baseURL: string) {
  const origin = new URL(baseURL).origin;
  const errors: string[] = [];
  const unexpected: string[] = [];
  const pages: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.context().addCookies([
    { name: "multica_logged_in", value: "1", url: origin },
    { name: "last_workspace_slug", value: "perf", url: origin },
  ]);
  await page.addInitScript(() => {
    localStorage.setItem("multica:chat:floatingChatEnabled", "false");
  });
  await page.routeWebSocket(() => true, (socket) => socket.onMessage(() => {}));
  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    if (url.origin !== origin) {
      unexpected.push(url.origin + url.pathname);
      return route.abort();
    }
    if (!url.pathname.startsWith("/api/")) return route.continue();
    const syntheticPost = route.request().method() === "POST" &&
      ["/api/client-usage", "/api/search-index/changes"].includes(url.pathname);
    if (route.request().method() !== "GET" && !syntheticPost) {
      unexpected.push(`${route.request().method()} ${url.pathname}`);
      return route.abort();
    }
    if (url.pathname === `/api/agents/${agentId}/tasks`) {
      const before = url.searchParams.get("before");
      pages.push(before ?? "first");
      const start = before ? Number(before) : 0;
      const limit = Number(url.searchParams.get("limit"));
      const end = Math.min(start + limit, count);
      return route.fulfill({ json: tasks.slice(start, end), headers: end < count
        ? { "X-Agent-Tasks-Next-Cursor": String(end) } : {} });
    }
    const body = responseFor(url.pathname);
    if (body === undefined) {
      unexpected.push(`${route.request().method()} ${url.pathname}${url.search}`);
      return route.fulfill({ status: 404, json: {} });
    }
    return route.fulfill({ json: body });
  });
  return { errors, unexpected, pages };
}

test.use({ viewport: { width: 1440, height: 900 }, locale: "en-US", serviceWorkers: "block", actionTimeout: 10_000 });

for (const compact of [false, true]) {
  test(`recent work retains pagination and bounded rendering (${compact ? "compact" : "wide"})`, async ({ page, baseURL }) => {
    if (compact) await page.setViewportSize({ width: 390, height: 844 });
    const state = await setup(page, baseURL!);
    await page.goto(`/perf/agents/${agentId}`);
    const recent = page.locator("section").filter({ has: page.getByRole("heading", { name: "Recent work", exact: true, includeHidden: true }) });
    const rows = recent.getByRole("button", { name: "View transcript", exact: true });
    await expect(recent.getByText("Synthetic work 0001", { exact: true })).toBeVisible();
    expect(state.pages).toEqual(["first"]);
    expect(await rows.count()).toBeLessThanOrEqual(10);
    const more = recent.getByRole("button", { name: /^Show more/ });
    for (let i = 0; i < 50; i++) {
      await more.click({ timeout: 10_000 });
      if (i < 49) await expect(more).toBeEnabled();
    }
    await expect(more).toHaveCount(0);
    await expect(recent.getByText("Synthetic work 1000", { exact: true })).toBeVisible();
    expect(state.pages).toEqual(["first", "200", "400", "600", "800"]);
    const mounted = await rows.count();
    if (baseline) expect(mounted).toBe(count);
    else expect(mounted).toBeLessThan(50);
    const maxMountedWhileScrolling = await recent.evaluate(async (section) => {
      let owner = section.parentElement;
      while (owner && !/auto|scroll/.test(getComputedStyle(owner).overflowY)) owner = owner.parentElement;
      if (!owner) throw new Error("Missing page scroll owner");
      const end = section.getBoundingClientRect().bottom - owner.getBoundingClientRect().top + owner.scrollTop - owner.clientHeight;
      owner.scrollTop = 0;
      let max = 0;
      for (let frame = 0; frame <= 90; frame++) {
        owner.scrollTop = end * frame / 90;
        await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
        max = Math.max(max, section.querySelectorAll('button[aria-label="View transcript"]').length);
      }
      return max;
    });
    if (!baseline) expect(maxMountedWhileScrolling).toBeLessThan(50);
    await expect(recent.getByText("Synthetic work 1000", { exact: true })).toBeInViewport();
    await expect(recent.getByRole("link", { name: "Open issue", exact: true }).last()).toHaveAttribute("href", "/perf/issues/PERF-1000");
    await page.screenshot({ path: test.info().outputPath("recent-work.png") });
    const last = rows.last();
    await last.click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await expect(page.getByText("Synthetic transcript for run 1000.", { exact: true })).toBeVisible();
    // Model a scroll restoration/resize recycling the opener while its modal
    // is open. A normal modal prevents pointer scrolling of the page below it.
    await recent.evaluate((section) => {
      for (let parent = section.parentElement; parent; parent = parent.parentElement) {
        if (/auto|scroll/.test(getComputedStyle(parent).overflowY) && parent.clientHeight > 0) {
          parent.scrollTop = 0;
          return;
        }
      }
      throw new Error("Missing page scroll owner");
    });
    if (!baseline) await expect(recent.getByText("Synthetic work 1000", { exact: true })).toHaveCount(0);
    await expect(page.getByRole("dialog")).toBeVisible();
    await page.screenshot({ path: test.info().outputPath("transcript-retained.png") });
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(recent.getByText("Synthetic work 0001", { exact: true })).toBeVisible();
    await rows.first().press("Enter");
    await expect(page.getByText("Synthetic transcript for run 1.", { exact: true })).toBeVisible();
    await page.evaluate(() => window.dispatchEvent(new CustomEvent("multica:navigate")));
    await expect(page.getByRole("dialog")).toHaveCount(0);
    const report = { baseline, compact, revealed_rows: count, mounted_rows: mounted, max_mounted_while_scrolling: maxMountedWhileScrolling, task_pages: state.pages, errors: state.errors, unexpected_requests: state.unexpected };
    console.log(JSON.stringify(report));
    await test.info().attach("recent-work", { body: JSON.stringify(report, null, 2), contentType: "application/json" });
    expect(state.errors).toEqual([]);
    expect(state.unexpected).toEqual([]);
  });
}
