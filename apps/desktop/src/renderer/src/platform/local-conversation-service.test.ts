// @vitest-environment node
import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CREATE_TASK_MENTION_ID } from "@multica/core/local-conversation/protocol";
import type { StorageAdapter } from "@multica/core/types";
import type { LocalV2Transport } from "@multica/core/local-conversation/v2";
import { createLocalConversationParentService } from "./local-conversation-service";

const mock = vi.hoisted(() => ({ api: {
  getBaseUrl: vi.fn(), listAgents: vi.fn(), listMembers: vi.fn(), listRuntimes: vi.fn(), listChatSessions: vi.fn(), listPendingChatTasks: vi.fn(), getIssue: vi.fn(),
} }));
vi.mock("@multica/core/api", async (original) => ({ ...await original<typeof import("@multica/core/api")>(), api: mock.api }));

const scope = { userId: "user", workspaceId: "ws" };
const at = "2026-10-29T00:00:00Z";
const summary = (id: string, fields: Record<string, unknown> = {}) => ({ conversation_id: id, summary_seq: 1, provider: "codex", title: id, status: "idle", cwd: "/repo", project_id: null, pinned: false, archived: false, updated_at: at, ...fields });
const snapshot = (id: string, fields: Record<string, unknown> = {}) => ({ version: 2, store_generation: "g", snapshot_seq: 4, conversation: summary(id), turns: [], settings: { model: "gpt", reasoning_effort: "high", permission_mode: "default" }, queue: { revision: 2, paused: false, items: [] }, created_at: at, ...fields });

type Route = (method: string, path: string, body?: Record<string, unknown>) => unknown;
let routes: Route[];
let requests: { method: string; path: string; body?: Record<string, unknown> }[];
let sent: unknown[];
let messages: ((scopeValue: typeof scope, message: unknown) => void)[];
let connections: Set<(scopeValue: typeof scope, connected: boolean) => void>;
const disposers: (() => void)[] = [];

function transport(): LocalV2Transport {
  return {
    request: async (_scope, method, path, body) => {
      requests.push({ method, path, body: body as Record<string, unknown> | undefined });
      for (const route of routes) { const value = route(method, path, body as Record<string, unknown> | undefined); if (value !== undefined) return value instanceof Error ? Promise.reject(value) : value; }
      throw new Error(`http_404: ${method} ${path}`);
    },
    send: async (_scope, message) => { sent.push(message); },
    subscribe: (listener) => { messages.push(listener); return () => {}; },
    onConnection: (listener) => { connections.add(listener); return () => { connections.delete(listener); }; },
    fileUrl: (_scope, path) => `v2:${path}`,
    upload: async (_scope, file) => ({ id: "att-1", name: file.name, mime_type: file.type, size: file.size }),
    choose: vi.fn(),
  };
}

function setup(options: { getSelectionIntent?: () => string; getSelectedSessionId?: () => string | null; storagePrefix?: string; userId?: string; workspaceId?: string; storage?: StorageAdapter } = {}, deviceId?: string) {
  const callbacks = { onLocalSelect: vi.fn(), onOpenSide: vi.fn(), onShowMessage: vi.fn() };
  const qc = new QueryClient();
  const service = createLocalConversationParentService({ queryClient: qc, workspaceId: "ws", workspaceSlug: "workspace", userId: "user", isCurrent: () => true, resolveAvatar: (value) => value,
    onSelectSession: vi.fn(), onOpenTask: vi.fn(), ...callbacks, ...options }, Object.assign(transport(), deviceId ? { deviceId } : {}));
  disposers.push(() => { service.dispose(); qc.clear(); });
  return { service, ...callbacks };
}

beforeEach(() => {
  vi.resetAllMocks();
  routes = []; requests = []; sent = []; messages = []; connections = new Set();
  mock.api.getBaseUrl.mockReturnValue("https://example.test");
  mock.api.listAgents.mockResolvedValue([{ id: "design", workspace_id: "ws", owner_id: "user", name: "Design reviewer", description: "", runtime_id: "cloud-runtime", runtime_bound: true, runtime_config: {}, visibility: "workspace", archived_at: null, permission_mode: "private", invocation_targets: [] }]);
  mock.api.listMembers.mockResolvedValue([{ user_id: "user", role: "owner" }]);
  mock.api.listRuntimes.mockResolvedValue([{ id: "cloud-runtime", provider: "codex" }]);
  mock.api.listChatSessions.mockResolvedValue([]);
  mock.api.listPendingChatTasks.mockResolvedValue({ tasks: [] });
  routes.push((method, path) => {
    if (path === "/providers") return { providers: [{ id: "codex", name: "Codex", available: true }, { id: "claude", name: "Claude Code", available: false }] };
    if (path === "/folder-projects" && method === "GET") return { projects: [{ id: "folder-1", title: "Repo", path: "/repo", created_at: at }] };
    if (path.startsWith("/sessions?")) return { scope_seq: 3, summaries: [summary("idle"), summary("busy", { status: "running", active_turn_id: "turn-9" })], has_more: false };
    return undefined;
  });
});
afterEach(() => { disposers.splice(0).forEach((dispose) => dispose()); });

describe("local conversation parent service over v2", () => {
  it("reloads the selected conversation on reconnect even when its first snapshot failed before subscribing", async () => {
    routes.unshift((_method, path) => path === "/sessions/c1/snapshot" ? new Error("device_unreachable: the network changed") : undefined);
    const { service } = setup({ getSelectedSessionId: () => "c1" }, "peer-1");
    await expect(service.request("local.snapshot", { sessionId: "c1" })).rejects.toThrow("network changed");
    const events: unknown[] = []; service.subscribe((event) => events.push(event));
    for (const listener of connections) listener({ ...scope, workspaceId: "other" }, true);
    expect(events).toEqual([]);
    for (const listener of connections) listener(scope, false);
    expect(events).toEqual([{ type: "connection", connected: false }]);
    for (const listener of connections) listener(scope, true);
    expect(events).toContainEqual({ type: "connection", connected: true });
    expect(events).toContainEqual({ type: "local.reload", sessionId: "c1" });
    service.dispose(); events.length = 0;
    for (const listener of connections) listener(scope, true);
    expect(events).toEqual([]);
  });

  it("loads providers, folder projects and every conversation with its running turn", async () => {
    const { service } = setup();
    const state = await service.load();
    expect(state.executionTargets?.map((target) => [target.id, target.canInvoke])).toEqual([["local:codex", true], ["local:claude", false]]);
    expect(state.sessions.map((session) => session.id)).toEqual(["idle", "busy"]);
    expect(state.folderProjects).toEqual([{ id: "folder-1", title: "Repo", path: "/repo", createdAt: at }]);
    expect(state.pendingTasks).toEqual([{ task_id: "turn-9", status: "running", chat_session_id: "busy" }]);
    expect(sent).toContainEqual({ version: 2, type: "subscribe_scope", after_scope_seq: 3 });
  });

  it("creates a conversation in a folder project and sends with mentions and quoted references", async () => {
    routes.unshift((method, path, body) => {
      if (method === "POST" && path === "/sessions") return summary(String(body!.id), { cwd: "/repo" });
      if (path === "/sessions/c1/snapshot") return snapshot("c1", { turns: [{ id: "t", turn_index: 0, request_id: "r", status: "completed", started_at: at, completed_at: at, messages: [{ id: "m", role: "user", content: "earlier question", created_at: at }], events: [] }] });
      if (method === "POST" && path === "/sessions/new/commands") return body!.type === "send" ? { turn_ids: ["turn-1", "turn-2"] } : summary("new");
      return undefined;
    });
    const { service } = setup();
    await service.request("session.create", { agentId: "local:codex", cwd: "/repo/" });
    expect(requests.find((entry) => entry.path === "/sessions" && entry.method === "POST")?.body).toMatchObject({ provider: "codex", folder_project_id: "folder-1" });
    const result = await service.request("session.send", { sessionId: "new", content: "Review this", requestId: "req-1", model: "gpt-5", references: [{ kind: "agent", id: "design" }, { kind: "session", id: "c1" }] } as never);
    expect(result).toMatchObject({ message_id: "msg-req-1", task_id: "turn-1" });
    const send = requests.find((entry) => entry.body?.type === "send")!.body!;
    expect(send).toMatchObject({ request_id: "req-1", text: "Review this", agents: [{ id: "design", name: "Design reviewer" }] });
    expect(send.prompt).toContain("Referenced context (quoted data)");
    expect(send.prompt).toContain("user: earlier question");
    // Model choices belong to the local provider; agent sends leave them alone.
    expect(requests.some((entry) => entry.body?.type === "update_settings")).toBe(false);
  });

  it("applies composer settings before a local send", async () => {
    routes.unshift((method, path, body) => method === "POST" && path === "/sessions/c1/commands" ? (body!.type === "send" ? { turn_id: "turn-1" } : { status: "ok" }) : undefined);
    const { service } = setup();
    await service.request("session.send", { sessionId: "c1", content: "hi", model: "gpt-5", reasoningEffort: "low" } as never);
    expect(requests.filter((entry) => entry.method === "POST").map((entry) => entry.body?.type)).toEqual(["update_settings", "send"]);
    expect(requests.find((entry) => entry.body?.type === "update_settings")!.body).toEqual({ type: "update_settings", model: "gpt-5", reasoning_effort: "low" });
  });

  it("rejects task creation mixed with agent mentions before sending", async () => {
    const { service } = setup();
    await expect(service.request("session.send", { sessionId: "c1", content: "x", references: [{ kind: "agent", id: "design" }, { kind: "issue", id: CREATE_TASK_MENTION_ID }] } as never))
      .rejects.toThrow("New task requires the current local provider");
    expect(requests.some((entry) => entry.body?.type === "send")).toBe(false);
  });

  it("projects the snapshot's settings, queue and fork origin into the session detail", async () => {
    // The copied turn keeps its start time; a turn sent after the fork must not move the branch point.
    const turn = (id: string, startedAt: string) => ({ id, turn_index: 0, request_id: id, status: "completed", started_at: startedAt, completed_at: startedAt, messages: [], events: [] });
    const turns = [turn("copy-3", "2026-10-28T23:00:00Z"), turn("after", "2026-10-29T01:00:00Z")];
    routes.unshift((_method, path) => path === "/sessions/c1/snapshot" ? snapshot("c1", { forked_from_id: "src", forked_from_turn_id: "t-3", turns, queue: { revision: 5, paused: true, error: "stopped", items: [] } }) : undefined);
    const { service } = setup();
    const { detail } = await service.request("local.snapshot", { sessionId: "c1" }) as unknown as { detail: Record<string, unknown> };
    expect(detail).toMatchObject({ model: "gpt", reasoningEffort: "high", permissionMode: "default", promptQueue: { revision: 5, paused: true, error: "stopped", items: [] }, forkedFrom: { sessionId: "src", turnId: "t-3", targetTurnId: "copy-3" } });
  });

  it("forwards a conversation's events while subscribed", async () => {
    const { service } = setup();
    const events: unknown[] = [];
    service.subscribe((event) => events.push(event));
    await service.request("local.subscribe", { sessionId: "c1", afterSeq: 4, generation: "g" } as never);
    expect(sent).toContainEqual({ version: 2, type: "subscribe", conversation_id: "c1", after_seq: 4, store_generation: "g" });
    const message = { version: 2, type: "conversation.events", conversation_id: "c1", from_seq: 5, to_seq: 5, events: [{ seq: 5, type: "turn.started", turn_id: "t", final: false, created_at: at, payload: {} }] };
    messages.forEach((listener) => listener(scope, message));
    expect(events).toContainEqual({ type: "local.events", sessionId: "c1", message });
    await service.request("local.unsubscribe", { sessionId: "c1" } as never);
    expect(sent.at(-1)).toEqual({ version: 2, type: "unsubscribe", conversation_id: "c1" });
  });

  it("maps queue, fork, answer and cancel to conversation commands", async () => {
    routes.unshift((method, path, body) => {
      if (method !== "POST" || path !== "/sessions/c1/commands") return undefined;
      if (body!.type === "fork") return summary("forked");
      if (String(body!.type).startsWith("queue")) return { revision: 3, paused: false, dispatching_request_id: "q1", items: [] };
      return { status: "requested" };
    });
    const { service } = setup();
    expect(await service.request("queue.update", { sessionId: "c1", action: "sendNow", requestId: "q1" } as never)).toMatchObject({ revision: 3, dispatchingRequestId: "q1" });
    expect(await service.request("session.fork", { sessionId: "c1", turnId: "t-1", requestId: "f-1" } as never)).toMatchObject({ id: "forked" });
    await service.request("interaction.respond", { sessionId: "c1", requestId: "question:t:q", answers: { a: "b" } } as never);
    await service.request("session.cancel", { sessionId: "c1", taskId: "t-1" } as never);
    expect(requests.map((entry) => entry.body)).toEqual([
      { type: "queue_update", action: "send_now", request_id: "q1" },
      { type: "fork", turn_id: "t-1", request_id: "f-1" },
      { type: "answer", interaction_id: "question:t:q", answer: { a: "b" } },
      { type: "interrupt" },
    ]);
  });

  it("maps the daemon's usage into the composer's shape", async () => {
    routes.unshift((_method, path) => path.startsWith("/usage?") ? { provider: "claude", context: { used_tokens: 120, limit_tokens: 1000, model: null, updated_at: null }, account: { status: "available", plan: "max", windows: [{ id: "5h", label: "5 hours", used_percent: 40, resets_at: at }], updated_at: at, stale: true } } : undefined);
    const { service } = setup();
    expect(await service.request("usage.read", { agentId: "local:claude", sessionId: "c1" } as never)).toEqual({ provider: "claude",
      context: { usedTokens: 120, limitTokens: 1000, model: null, updatedAt: null },
      account: { status: "available", plan: "max", updatedAt: at, stale: true, windows: [{ id: "5h", label: "5 hours", usedPercent: 40, resetsAt: at }] } });
    expect(requests[0]!.path).toBe("/usage?provider=claude&conversation_id=c1");
  });

  it("routes compaction to the selected local conversation and validates its acknowledgement", async () => {
    let reply: unknown = { turn_id: "turn-compact-1" };
    routes.unshift((method, path) => method === "POST" && path === "/sessions/c1/commands" ? reply : undefined);
    const { service } = setup();
    expect(await service.request("session.compact", { sessionId: "c1", requestId: "compact-1" })).toEqual({ turnId: "turn-compact-1" });
    expect(requests[0]).toMatchObject({ method: "POST", path: "/sessions/c1/commands", body: { type: "compact", request_id: "compact-1" } });
    reply = {};
    await expect(service.request("session.compact", { sessionId: "c1", requestId: "compact-2" })).rejects.toThrow();
  });

  it("keeps only the latest GUI selection when reads finish in reverse order", async () => {
    let intent = "a";
    let release: () => void = () => {};
    routes.unshift((_method, path) => path === "/sessions/slow/snapshot" ? new Promise((resolve) => { release = () => resolve(snapshot("slow")); }) : path === "/sessions/fast/snapshot" ? snapshot("fast") : undefined);
    const { service, onLocalSelect } = setup({ getSelectionIntent: () => intent });
    const slow = service.request("host.selectSession", { sessionId: "slow" } as never);
    intent = "b";
    await service.request("host.selectSession", { sessionId: "fast" } as never);
    release(); await slow;
    expect(onLocalSelect.mock.calls).toEqual([["fast", "codex"]]);
  });

  it("refuses to delete local history from the GUI", async () => {
    const { service } = setup();
    await expect(service.request("session.delete", { sessionId: "c1" } as never)).rejects.toThrow("Archive local conversations");
  });
});

describe("file menu", () => {
  it("names the device whose conversation the file belongs to", async () => {
    const show = vi.fn(async () => undefined);
    vi.stubGlobal("window", { desktopAPI: { localFileContextMenu: { show } } });
    const params = { sessionId: "c1", url: "file:///repo/a.ts", hasSelection: false };
    await setup({}, "peer-1").service.request("host.showFileContextMenu", params as never);
    await setup().service.request("host.showFileContextMenu", params as never);
    expect(show.mock.calls.map((call) => (call as unknown[])[2])).toEqual(["peer-1", "local"]);
    vi.unstubAllGlobals();
  });
});


it("shares agent selection counts across main and Side while isolating API, user and workspace", async () => {
  const records = new Map<string, string>();
  const storage = { getItem: (key: string) => records.get(key) ?? null, setItem: (key: string, value: string) => { records.set(key, value); }, removeItem: (key: string) => { records.delete(key); }, keys: () => [...records.keys()] };
  const main = setup({ storage }).service;
  const side = setup({ storage, storagePrefix: "side:c1:" }).service;
  await Promise.all([main.request("storage.write", { key: "agent-mention-selection:v1", value: "design" }), side.request("storage.write", { key: "agent-mention-selection:v1", value: "design" })]);
  const usage = await main.request("storage.read", { key: "agent-mention-usage:v1" });
  expect(JSON.parse(usage!)).toMatchObject({ version: 1, agents: { design: { count: 2 } } });
  expect(await side.request("storage.read", { key: "agent-mention-usage:v1" })).toBe(usage);
  for (const options of [{ userId: "other-user" }, { workspaceId: "other-ws" }]) {
    expect(await setup({ storage, ...options }).service.request("storage.read", { key: "agent-mention-usage:v1" })).toBeNull();
  }
  mock.api.getBaseUrl.mockReturnValue("https://other.test");
  expect(await setup({ storage }).service.request("storage.read", { key: "agent-mention-usage:v1" })).toBeNull();
  mock.api.getBaseUrl.mockReturnValue("https://example.test");
  expect(await setup({ storage }).service.request("storage.read", { key: "agent-mention-usage:v1" })).toBe(usage);
  await main.request("storage.write", { key: "draft", value: "main" });
  await side.request("storage.write", { key: "draft", value: "side" });
  expect(await main.request("storage.read", { key: "draft" })).toBe("main");
  expect(await side.request("storage.read", { key: "draft" })).toBe("side");
});
