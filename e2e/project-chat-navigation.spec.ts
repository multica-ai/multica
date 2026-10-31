import { expect, test } from "@playwright/test";

// Production shared UI with synthetic HTTP/WS boundaries; no backend or account.
const wsId = "11111111-1111-4111-8111-111111111111";
const userId = "22222222-2222-4222-8222-222222222222";
const agentId = "33333333-3333-4333-8333-333333333333";
const sessionId = "44444444-4444-4444-8444-444444444444";
const taskId = "77777777-7777-4777-8777-777777777777";
const timestamp = "2026-10-20T00:00:00Z";
const session = {
  project_id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
  id: sessionId, workspace_id: wsId, agent_id: agentId, creator_id: userId,
  title: "Alpha recent", status: "active", has_unread: false, unread_count: 0,
  last_message: null, pinned: false, created_at: timestamp, updated_at: timestamp,
};
const agent = {
  id: agentId, workspace_id: wsId, runtime_id: "55555555-5555-4555-8555-555555555555",
  name: "Fixture Agent", description: "", instructions: "", avatar_url: null,
  runtime_mode: "local", runtime_config: {}, custom_args: [], visibility: "workspace",
  permission_mode: "public_to", invocation_targets: [{ target_type: "workspace", target_id: null }],
  status: "idle", max_concurrent_tasks: 1, model: "", owner_id: userId, skills: [],
  runtime_availability: "online", created_at: timestamp, updated_at: timestamp,
  archived_at: null, archived_by: null,
};

const projectId = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const otherProject = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const project = { id: projectId, workspace_id: wsId, title: "Project Alpha", description: null, icon: null,
  status: "in_progress", priority: "none", lead_type: null, lead_id: null, start_date: null, due_date: null,
  issue_count: 0, done_count: 0, resource_count: 0, created_at: timestamp, updated_at: timestamp };

test.use({ viewport: { width: 1440, height: 1000 }, locale: "en-US", serviceWorkers: "block" });

for (const scenario of ["discovery and scoped actions", "new unscoped conversation"] as const) {
test(`project Chats ${scenario} with synthetic HTTP and WS`, async ({ page, context, baseURL }, testInfo) => {
  test.setTimeout(180_000);
  const baseline = process.env.PROJECT_CHAT_BASELINE === "1";
  const origin = new URL(baseURL ?? "http://localhost:3000").origin;
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await context.addCookies([{ name: "multica_logged_in", value: "1", url: origin }, { name: "last_workspace_slug", value: "perf", url: origin }]);
  await context.addInitScript(() => { localStorage.setItem("multica:chat:isOpen", "false"); localStorage.setItem("multica:chat:floatingChatEnabled", "false"); });
  let projects = [project, { ...project, id: otherProject, title: "Project Beta" }];
  let sessions = [
    { ...session, last_message: { role: "assistant", content: "Latest Alpha summary", created_at: "2026-10-23T00:00:00Z" } },
    { ...session, id: "44444444-4444-4444-8444-444444444445", title: "Alpha pinned", pinned: true },
    { ...session, id: "44444444-4444-4444-8444-444444444446", title: "Beta conversation", project_id: otherProject },
    { ...session, id: "44444444-4444-4444-8444-444444444447", title: "Workspace conversation", project_id: null },
    { ...session, id: "44444444-4444-4444-8444-444444444448", title: "Alpha archived", status: "archived" },
  ];
  const responseFor = (path: string): unknown => {
    switch (path) {
      case "/api/config": return { feature_flags: {}, allow_signup: true };
      case "/api/me": return {
        id: userId, name: "Fixture User", email: "fixture@example.test", avatar_url: null,
        language: "en", onboarded_at: timestamp, onboarding_questionnaire: { source: "other" },
        created_at: timestamp, updated_at: timestamp,
      };
      case "/api/workspaces": return [{
        id: wsId, name: "Perf", slug: "perf", description: null, context: null,
        settings: { github_enabled: false }, repos: [], issue_prefix: "PERF", avatar_url: null,
        created_at: timestamp, updated_at: timestamp,
      }];
      case "/api/agents": return [agent];
      case "/api/chat/sessions": return sessions;
      case "/api/chat/pending-tasks/has-any": return { has_pending: false };
      case `/api/tasks/${taskId}/messages`: return [];
      case "/api/projects": return { projects, total: projects.length };
      case `/api/projects/${projectId}`: return projects[0];
      case `/api/projects/${projectId}/resources`: return [];
      case "/api/issues/table/groups": return { query_fingerprint: "fixture", total: 0, groups: [], next_cursor: null };
      case "/api/issues/table/rows": return { query_fingerprint: "fixture", total: 0, rows: [], branch_total: 0, next_cursor: null };
      case "/api/issues/table/facets": return { query_fingerprint: "fixture", total: 0, facets: [] };
      case "/api/issues/child-progress": return {};
      case "/api/issues": return { issues: [], total: 0 };
      case "/api/issue-statuses": return { statuses: [] };
      case "/api/properties": return { properties: [], total: 0 };
      case "/api/quick-actions": return { quick_actions: [] };
      case "/api/client-usage": return {};
      case "/api/issue-view-preferences": return { scope_type: "workspace", scope_id: null,
        ordered_view_ids: [], hidden_view_ids: [], default_view_id: null };
      case "/api/issue-workflows": return { workflows: [] };
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
        id: "member-1", user_id: userId, workspace_id: wsId, role: "owner",
        name: "Fixture User", email: "fixture@example.test", avatar_url: null,
      }];
      case "/api/autopilots": return { autopilots: [], total: 0 };
      case "/api/skills":
      case "/api/runtimes": case "/api/squads": case "/api/agent-task-snapshot":
      case "/api/invitations": case "/api/inbox": case "/api/inbox/unread-summary":
      case "/api/pins": case "/api/chat/pending-tasks": case "/api/assignee-frequency":
      case "/api/issue-views": case "/api/issue-wakeup-summaries":
      case "/api/agent-run-counts": case "/api/agent-activity-30d":
        return [];
      default: return undefined;
    }
  };
  const messages = new Map<string, unknown[]>();
  const unexpected: string[] = [];
  await context.route("**/*", async route => {
    const url = new URL(route.request().url());
    if (url.origin !== origin) return route.abort();
    if (!url.pathname.startsWith("/api/")) return route.continue();
    if (url.pathname === "/api/chat/sessions" && route.request().method() === "POST") {
      const input = route.request().postDataJSON();
      const created = { ...session, ...input, id: "44444444-4444-4444-8444-444444444449", title: "New conversation" };
      sessions.push(created);
      return route.fulfill({ status: 201, contentType: "application/json", body: JSON.stringify(created) });
    }
    if (url.pathname.endsWith("/messages") && route.request().method() === "POST") {
      const id = url.pathname.split("/")[4]!;
      messages.set(id, [{ id: "66666666-6666-4666-8666-666666666666", chat_session_id: id,
        role: "user", content: route.request().postDataJSON().content, task_id: taskId, created_at: timestamp }]);
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({
        message_id: "66666666-6666-4666-8666-666666666666", task_id: taskId,
        created_at: timestamp, supports_queue: true, queued: false,
      }) });
    }
    const match = url.pathname.match(/^\/api\/chat\/sessions\/([^/]+)(.*)$/);
    if (match) {
      const row = sessions.find(s => s.id === match[1]);
      let body: unknown = row;
      if (route.request().method() === "PATCH" && row) {
        const update = route.request().postDataJSON();
        if (match[2] === "/archive") row.status = update.archived ? "archived" : "active";
        else Object.assign(row, update);
      } else if (route.request().method() === "DELETE") {
        sessions = sessions.filter(s => s.id !== match[1]); body = {};
      } else if (match[2] === "/messages/page") body = { messages: messages.get(match[1]!) ?? [], has_more: false, limit: 50 };
      else if (match[2] === "/pending-task") body = {};
      else if (match[2] === "/draft-restores") body = [];
      else if (match[2] === "/read") body = {};
      return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body ?? {}) });
    }
    const body = responseFor(url.pathname);
    if (body === undefined) unexpected.push(url.pathname);
    return route.fulfill({ status: body === undefined ? 404 : 200, contentType: "application/json", body: JSON.stringify(body ?? {}) });
  });
  let sendEvent = (_type: string, _payload: unknown) => {};
  await context.routeWebSocket(url => url.pathname === "/ws", socket => {
    socket.onMessage(() => {});
    sendEvent = (type, payload) => socket.send(JSON.stringify({ type, payload, actor_id: userId, actor_type: "user" }));
  });
  await page.goto(`${origin}/perf/projects/${projectId}`);
  await expect(page.getByText("Project Alpha", { exact: true }).first()).toBeVisible();
  if (baseline) {
    await expect(page.getByRole("region", { name: "Chats", exact: true })).toHaveCount(0);
  } else {
    const chats = page.getByRole("region", { name: "Chats", exact: true });
    await expect(chats.getByRole("link")).toHaveCount(2);
    await expect(chats.getByRole("link").first()).toContainText("Alpha recent");
    await expect(chats.getByText("Latest Alpha summary")).toBeVisible();
  }
  await page.screenshot({ path: testInfo.outputPath(baseline ? "base-project.png" : "head-project.png"), fullPage: true });
  if (!baseline) {
    await page.getByRole("region", { name: "Chats", exact: true }).getByRole("link").first().click();
    await expect(page).toHaveURL(new RegExp(`session=${sessionId}`));
  } else await page.goto(`${origin}/perf/chat`);
  await expect(page.getByText("Beta conversation", { exact: true })).toBeVisible();
  if (baseline) {
    await expect(page.getByRole("combobox", { name: "Project", exact: true })).toHaveCount(0);
    await page.screenshot({ path: testInfo.outputPath("base-chat.png"), fullPage: true });
    return;
  }
  const filter = page.getByRole("combobox", { name: "Project", exact: true });
  await filter.click();
  await page.getByRole("option", { name: "Project Alpha", exact: true }).click();
  await expect(page.getByText("Beta conversation", { exact: true })).toHaveCount(0);
  await expect(page.getByText("Workspace conversation", { exact: true })).toHaveCount(0);
  await expect(page.getByText("Alpha recent", { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("option", { name: "Project Alpha", exact: true })).toHaveCount(0);
  await page.screenshot({ path: testInfo.outputPath("head-chat-filter.png"), fullPage: true });
  if (scenario === "new unscoped conversation") {
    await page.getByRole("button", { name: "New chat", exact: true }).click();
    const content = "Review new unscoped conversation";
    await page.locator('[contenteditable="true"]').last().fill(content);
    const created = page.waitForResponse(r => r.url().endsWith("/api/chat/sessions") && r.request().method() === "POST");
    const sent = page.waitForResponse(r => r.url().endsWith("/messages") && r.request().method() === "POST");
    await page.getByRole("button", { name: "Send", exact: true }).click();
    expect((await created).status()).toBe(201);
    expect((await sent).status()).toBe(200);
    expect(sessions.find(s => s.id.endsWith("449"))?.project_id).toBeNull();
    await expect(page).toHaveURL(/session=44444444-4444-4444-8444-444444444449/);
    await expect(filter).toContainText("All projects");
    await expect(page.getByText("New conversation", { exact: true }).first()).toBeVisible();
    await expect(page.getByText(content, { exact: true }).first()).toBeVisible();
    // Reopen through the list to verify the sent transcript remains reachable.
    const row = (title: string) => page.locator('div[tabindex="0"]').filter({ has: page.getByText(title, { exact: true }) });
    await row("Beta conversation").click();
    await expect(page).toHaveURL(/session=44444444-4444-4444-8444-444444444446/);
    await row("New conversation").click();
    await expect(page).toHaveURL(/session=44444444-4444-4444-8444-444444444449/);
    await expect(page.getByText(content, { exact: true }).first()).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath("new-chat-selected.png"), fullPage: true });
    expect(errors).toEqual([]);
    expect(unexpected).toEqual([]);
    return;
  }
  // Archiving advances within Alpha, never into another project's row.
  const selectedRow = page.locator('div[tabindex="0"]').filter({ has: page.getByText("Alpha recent", { exact: true }) });
  await selectedRow.hover();
  await selectedRow.getByRole("button", { name: "Archive", exact: true }).click();
  await expect(page).toHaveURL(/session=44444444-4444-4444-8444-444444444445/);
  await expect(page.getByText("Alpha recent", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: /Archived/ }).click();
  await expect(page.getByText("Alpha archived", { exact: true })).toBeVisible();
  await expect(page.getByText("Alpha recent", { exact: true })).toBeVisible();
  // Canonical realtime project_id patches remove a row from this project.
  const archived = sessions.find(s => s.title === "Alpha archived")!;
  archived.project_id = otherProject;
  sendEvent("chat:session_updated", { chat_session_id: archived.id, project_id: otherProject });
  await expect(page.getByText("Alpha archived", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Clear project filter" }).click();
  await expect(filter).toContainText("All projects");
  // Delete from the archive: no invisible selection remains.
  const row = page.locator('div[tabindex="0"]').filter({ has: page.getByText("Alpha recent", { exact: true }) });
  await row.click();
  await row.hover();
  await row.getByRole("button", { name: /Delete chat/ }).click();
  await row.getByRole("button", { name: "Delete", exact: true }).click();
  await expect(page.getByText("Alpha recent", { exact: true })).toHaveCount(0);
  // Deletion event invalidates projects AND surviving chat associations.
  await filter.click(); await page.getByRole("option", { name: "Project Alpha", exact: true }).click();
  projects = projects.filter(p => p.id !== projectId);
  sessions.forEach(s => { if (s.project_id === projectId) s.project_id = null; });
  sendEvent("project:deleted", { id: projectId });
  await expect(filter).toContainText("All projects");
  await expect(page.getByRole("button", { name: "Clear project filter" })).toHaveCount(0);
  await page.setViewportSize({ width: 390, height: 844 });
  const back = page.getByRole("button", { name: "Chat", exact: true });
  if (await back.isVisible()) await back.click();
  await expect(filter).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("head-chat-compact.png"), fullPage: true });
  expect(errors).toEqual([]);
  expect(unexpected).toEqual([]);
});
}
