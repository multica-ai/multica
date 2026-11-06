import { expect, test } from "@playwright/test";

// 使用真实网页和编辑器，HTTP 与 WebSocket 仅返回合成数据，不创建账号或启动智能体。
const wsId = "11111111-1111-4111-8111-111111111111";
const userId = "22222222-2222-4222-8222-222222222222";
const agentId = "33333333-3333-4333-8333-333333333333";
const sessionId = "44444444-4444-4444-8444-444444444444";
const taskId = "77777777-7777-4777-8777-777777777777";
const timestamp = "2026-10-20T00:00:00Z";
const session = {
  id: sessionId, workspace_id: wsId, agent_id: agentId, creator_id: userId,
  title: "Chat annotations", status: "active", has_unread: false, unread_count: 0,
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


test.use({ viewport: { width: 1280, height: 800 }, locale: "en-US", serviceWorkers: "block" });

for (const surface of ["page", "floating", "narrow"] as const) {
  test(`chat annotations preserve draft and retry on the ${surface} surface`, async ({ page, context, baseURL }, testInfo) => {
    const origin = new URL(baseURL ?? "http://localhost:3000").origin;
    const unexpected: string[] = [];
    const errors: string[] = [];
    const sends: string[] = [];
    let failNextSend = true;
    if (surface === "narrow") {
      await page.setViewportSize({ width: 390, height: 844 });
      await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
    }
    page.on("pageerror", error => errors.push(error.message));
    page.on("console", message => {
      if (message.text().includes("[api-schema]")) errors.push(message.text());
    });
    await context.addCookies([
      { name: "multica_logged_in", value: "1", url: origin },
      { name: "last_workspace_slug", value: "perf", url: origin },
    ]);
    await context.addInitScript(({ sessionId, agentId }) => {
      localStorage.setItem("multica:chat:isOpen", "false");
      localStorage.setItem("multica:chat:floatingChatEnabled", "true");
      localStorage.setItem("multica:chat:activeSessionId:perf", sessionId);
      localStorage.setItem("multica:chat:selectedAgentId:perf", agentId);
    }, { sessionId, agentId });
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
        case "/api/chat/sessions": return [session];
        case `/api/chat/sessions/${sessionId}`: return session;
        case `/api/chat/sessions/${sessionId}/messages/page`: return {
          messages: Array.from({ length: 4 }, (_, i) => ({
            id: `message-${i}`, chat_session_id: sessionId, role: "assistant", task_id: null,
            content: i === 3 ? "Please review this proposal before implementation." : `Fixture message ${i}. `.repeat(1 + i % 5),
            created_at: timestamp,
          })), limit: 50, has_more: false,
        };
        case `/api/chat/sessions/${sessionId}/pending-task`: return null;
        case `/api/tasks/${taskId}/messages`: return [];
        case `/api/chat/sessions/${sessionId}/read`: return {};
        case `/api/chat/sessions/${sessionId}/draft-restores`: return { restores: [] };
        case "/api/chat/pending-tasks/has-any": return { has_pending: false };
        case "/api/projects": return { projects: [], total: 0 };
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

    await context.route("**/*", async route => {
      const url = new URL(route.request().url());
      if (url.origin !== origin && !url.pathname.startsWith("/api/")) return route.abort();
      if (!url.pathname.startsWith("/api/")) return route.continue();
      if (url.pathname === `/api/chat/sessions/${sessionId}/messages` && route.request().method() === "POST") {
        sends.push(route.request().postDataJSON().content);
        const failed = failNextSend;
        failNextSend = false;
        return route.fulfill({ status: failed ? 500 : 201, contentType: "application/json",
          body: JSON.stringify(failed ? { error: "Fixture send failed" } : {
            message_id: "66666666-6666-4666-8666-666666666666", task_id: taskId, created_at: timestamp,
          }) });
      }
      const body = responseFor(url.pathname);
      if (body === undefined) unexpected.push(url.pathname);
      return route.fulfill({ status: body === undefined ? 404 : 200,
        contentType: "application/json", body: JSON.stringify(body ?? {}) });
    });
    await context.routeWebSocket(url => url.pathname === "/ws", socket => socket.onMessage(() => {}));
    const path = surface === "floating" ? "/perf/agents" : `/perf/chat?session=${sessionId}`;
    await page.goto(origin + path, { waitUntil: "domcontentloaded" });
    if (surface === "floating") await page.getByRole("button", { name: "Ask Multica", exact: true }).click();
    const source = page.locator('[data-chat-reply-content="message-3"]');
    await expect(source).toBeVisible({ timeout: 15000 });
    const editor = page.locator('.tiptap[contenteditable="true"]');
    await editor.fill("My existing draft");
    await source.focus();
    await source.evaluate(element => {
      const text = element.querySelector("p")?.firstChild ?? element.firstChild!;
      const range = document.createRange();
      range.setStart(text, 0);
      range.setEnd(text, "Please review this proposal".length);
      const selection = window.getSelection()!;
      selection.removeAllRanges();
      selection.addRange(range);
      element.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
    });
    await page.getByRole("button", { name: "Add to reply", exact: true }).click();
    const note = page.getByRole("textbox", { name: "Comment", exact: true });
    await note.fill("Please make this more specific.");
    await note.press("Enter");
    await expect(page.getByRole("button", { name: "1 annotation", exact: true })).toBeVisible();
    await expect(editor).toHaveText("My existing draft");
    await page.getByRole("button", { name: "Edit annotation 1", exact: true }).click({ timeout: 5000 });
    await expect(note).toHaveValue("Please make this more specific.");
    await note.press("Escape");
    await expect.poll(() => page.evaluate(() => localStorage.getItem("multica:chat:drafts:perf")))
      .toContain("My existing draft");
    await page.reload();
    if (surface === "floating") await page.getByRole("button", { name: "Ask Multica", exact: true }).click();
    await expect(source).toBeVisible();
    await expect(page.getByRole("button", { name: "1 annotation", exact: true })).toBeVisible();
    await expect(editor).toHaveText("My existing draft");
    await page.getByRole("button", { name: "1 annotation", exact: true }).click();
    await page.getByRole("button", { name: "Edit annotation 1", exact: true }).last().click();
    await expect(note).toBeVisible();
    await note.fill("Please add a concrete example.");
    await page.screenshot({ path: testInfo.outputPath("chat-annotation.png") });
    await note.press("Enter");
    await page.getByRole("button", { name: "Send", exact: true }).click();
    await expect.poll(() => sends.length).toBe(1);
    await expect(page.getByRole("button", { name: "1 annotation", exact: true })).toBeVisible();
    await expect(editor).toHaveText("My existing draft");
    await editor.press("ControlOrMeta+Enter");
    await expect.poll(() => sends.length).toBe(2);
    expect(sends[0]).toBe(sends[1]);
    expect(sends[1]).toContain("> Please review this proposal");
    expect(sends[1]).toContain("Please add a concrete example.");
    expect(sends[1]).toContain("My existing draft");
    await expect(page.getByRole("button", { name: "1 annotation", exact: true })).toHaveCount(0);
    expect(errors).toEqual([]);
    expect(unexpected).toEqual([]);
  });
}
