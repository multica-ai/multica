import type { Page } from "@playwright/test";

export const workspaceId = "11111111-1111-4111-8111-111111111111";
export const userId = "22222222-2222-4222-8222-222222222222";
export const agentId = "33333333-3333-4333-8333-333333333333";
const at = "2026-01-01T00:00:00Z";
const runtimeId = "44444444-4444-4444-8444-444444444444";
const demoAgent = {
  id: agentId, workspace_id: workspaceId, runtime_id: runtimeId, runtime_bound: true,
  name: "Demo Assistant", description: "Generated demo agent", instructions: "",
  avatar_url: null, runtime_mode: "local", runtime_config: {}, custom_args: [],
  visibility: "private", permission_mode: "private", invocation_targets: [],
  status: "active", max_concurrent_tasks: 1, model: "", owner_id: userId,
  skills: [], created_at: at, updated_at: at, archived_at: null, archived_by: null,
};
const workspace = { id: workspaceId, name: "Demo workspace", slug: "perf", description: null, context: null, settings: {}, repos: [], issue_prefix: "DEMO", avatar_url: null, created_at: at, updated_at: at };

export function buildSessions(active = 500, archived = 1500, previewLength = 8192) {
  return Array.from({ length: active + archived }, (_, index) => {
    const isArchived = index >= active;
    const n = isArchived ? index - active : index;
    const timestamp = new Date(Date.parse(at) - n * 1000).toISOString();
    return {
      id: `${isArchived ? "bbbb" : "aaaa"}${String(n).padStart(4, "0")}-4444-4444-8444-444444444444`,
      workspace_id: workspaceId, agent_id: agentId, creator_id: userId,
      title: `${isArchived ? "Archived" : "Session"} ${String(n).padStart(4, "0")}`,
      status: isArchived ? "archived" : "active", pinned: false,
      has_unread: !isArchived && n === active - 1, unread_count: !isArchived && n === active - 1 ? 3 : 0,
      last_message: { content: (`Synthetic demo response ${index}. ` + "This is generated test content. ".repeat(300)).slice(0, previewLength), role: "assistant", created_at: timestamp },
      created_at: timestamp, updated_at: timestamp,
    };
  });
}

export async function setupSessionPagination(page: Page, baseURL: string, options: { active?: number; archived?: number; failFirstPage?: boolean } = {}) {
  const origin = new URL(baseURL).origin;
  const rows = buildSessions(options.active, options.archived);
  const requests: { path: string; bytes: number; rows?: number }[] = [];
  const unexpected: string[] = [];
  const errors: string[] = [];
  let failFirstPage = options.failFirstPage ?? false;
  page.on("pageerror", (error) => errors.push(error.message));
  await page.context().addCookies([{ name: "multica_logged_in", value: "1", url: origin }, { name: "last_workspace_slug", value: "perf", url: origin }]);
  await page.addInitScript(() => localStorage.setItem("multica:chat:floatingChatEnabled", "true"));
  await page.routeWebSocket(() => true, (socket) => socket.onMessage(() => {}));
  await page.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    if (url.origin !== origin) { unexpected.push(url.origin + url.pathname); return route.abort(); }
    if (!url.pathname.startsWith("/api/")) return route.continue();
    let body: unknown;
    switch (url.pathname) {
      case "/api/config": body = { feature_flags: {}, allow_signup: true }; break;
      case "/api/me": body = { id: userId, name: "Demo User", email: "demo@example.test", avatar_url: null, language: "en", onboarded_at: at, onboarding_questionnaire: { source: "other" }, created_at: at, updated_at: at }; break;
      case "/api/workspaces": body = [workspace]; break;
      case "/api/client-usage": body = {}; break;
      case "/api/chat/sessions": body = rows; break;
      case "/api/chat/sessions/page": {
        if (failFirstPage) { failFirstPage = false; return route.fulfill({ status: 400, json: { error: "Synthetic one-time failure" } }); }
        const status = url.searchParams.get("status") ?? "active";
        const offset = Number(url.searchParams.get("cursor") ?? 0);
        const filtered = rows.filter((row) => row.status === status);
        const sessions = filtered.slice(offset, offset + 50).map((row) => ({ ...row, last_message: { ...row.last_message, content: row.last_message.content.slice(0, 256) } }));
        const hasMore = offset + 50 < filtered.length;
        body = { sessions, has_more: hasMore, next_cursor: hasMore ? String(offset + 50) : null };
        break;
      }
      case "/api/chat/sessions/summary": body = {
        active_count: rows.filter((row) => row.status === "active").length,
        archived_count: rows.filter((row) => row.status === "archived").length,
        unread_by_session: Object.fromEntries(rows.filter((row) => row.unread_count > 0).map((row) => [row.id, row.unread_count])),
      }; break;
      case "/api/chat/pending-tasks": body = { tasks: [] }; break;
      case "/api/chat/pending-tasks/has-any": body = { has_pending: false }; break;
      case "/api/projects": body = { projects: [], total: 0 }; break;
      case "/api/issues": body = { issues: [], total: 0 }; break;
      case "/api/issue-statuses": body = { statuses: [] }; break;
      case "/api/properties": body = { properties: [], total: 0 }; break;
      case "/api/quick-actions": body = { quick_actions: [] }; break;
      case "/api/search-index/manifest": body = { cursor: "0", issue_count: 0, comment_count: 0, project_count: 0, text_bytes: 0 }; break;
      case "/api/search-index/snapshot": body = { issues: [], comments: [], projects: [], next_after_number: 0, done: true }; break;
      case "/api/search-index/changes": body = { issues: [], comments: [], projects: [], deleted: { issues: [], comments: [], projects: [] }, cursor: "0", has_more: false }; break;
      case "/api/agents": body = [demoAgent]; break;
      case "/api/runtimes": body = [{ id: runtimeId, workspace_id: workspaceId, name: "Demo runtime", provider: "claude", status: "online", owner_id: userId, visibility: "private", last_seen_at: new Date().toISOString(), created_at: at, updated_at: at }]; break;
      case `/api/workspaces/${workspaceId}/members`: body = [{ id: userId, user_id: userId, workspace_id: workspaceId, role: "owner", name: "Demo User", email: "demo@example.test", created_at: at }]; break;
      case "/api/squads": case "/api/agent-task-snapshot":
      case "/api/invitations": case "/api/inbox": case "/api/inbox/unread-summary": case "/api/pins": case "/api/assignee-frequency":
      case "/api/skills": case "/api/autopilots": case "/api/agent-run-counts": case "/api/agent-activity-30d": body = []; break;
      default: {
        const match = /^\/api\/chat\/sessions\/([^/]+)(.*)$/.exec(url.pathname);
        const row = rows.find((row) => row.id === match?.[1]);
        if (row && match) {
          switch (match[2]) {
            case "": body = row; break;
            case "/messages/page": body = { messages: [{ id: row.id.replace(/^[ab]{4}/, "cccc"), chat_session_id: row.id, role: "assistant", content: "This is a synthetic conversation for verifying session selection. No real user data is used.", task_id: null, created_at: at }], limit: 50, has_more: false, next_cursor: null }; break;
            case "/messages": body = []; break;
            case "/pending-task": case "/read": body = {}; break;
            case "/draft-restores": body = { restores: [] }; break;
          }
        }
      }
    }
    if (body === undefined) { unexpected.push(`${route.request().method()} ${url.pathname}${url.search}`); return route.fulfill({ status: 404, json: {} }); }
    const text = JSON.stringify(body);
    if (url.pathname.startsWith("/api/chat/sessions")) requests.push({ path: url.pathname + url.search, bytes: Buffer.byteLength(text), rows: Array.isArray(body) ? body.length : (body as { sessions?: unknown[] }).sessions?.length });
    return route.fulfill({ status: 200, contentType: "application/json", body: text });
  });
  return { rows, requests, unexpected, errors };
}
