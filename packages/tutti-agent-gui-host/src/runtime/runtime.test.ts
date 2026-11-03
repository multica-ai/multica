import { executionTargets } from "../protocol";
// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Agent, AgentTask, Attachment, ChatMessage, ChatSession, PendingChatTaskItem, TaskMessagePayload } from "@multica/core/types";
import type { AgentGuiHostEvent, AgentGuiInitSnapshot, AgentGuiRpcClient, AgentGuiRpcMethods, AgentGuiSessionDetail, AgentGuiTaskMessagePayload } from "../protocol";
import { LOCAL_TIMING_PREFIX } from "../local-timing";
import { createMulticaAgentGuiRuntime, MulticaAgentGuiError } from "./create-multica-agent-gui-runtime";
import { RuntimeLedger } from "./ledger";
import { projectDetail } from "./projection";
import { withLocalSnapshots } from "./local-snapshot-test-host";
import { createRichTextMentionMarkdown } from "@tutti-os/ui-rich-text/core";

const time = "2026-10-24T01:00:00Z";
const agent = { id: "agent-1", name: "Codex", model: "test-model", archived_at: null } as Agent;
const session = {
  id: "server-session", workspace_id: "workspace-1", agent_id: agent.id,
  creator_id: "member-1", title: "Example", status: "active",
  created_at: time, updated_at: time, has_unread: false,
} as ChatSession;
const init: AgentGuiInitSnapshot = {
  workspaceId: "workspace-1", workspaceSlug: "dev", userId: "member-1",
  locale: "en", theme: "dark", activeSessionId: null,
  agents: [{ agent, provider: "codex", iconUrl: "", canInvoke: true, runtimeBound: true, runtimeOnline: true }],
  sessions: [],
};
const handles: ReturnType<typeof createMulticaAgentGuiRuntime>[] = [];
afterEach(() => { for (const handle of handles.splice(0)) handle.dispose(); });

function backend(storage = new Map<string, string>()) {
  const sessions: ChatSession[] = [];
  const messages: ChatMessage[] = [];
  const tasks: AgentTask[] = [];
  const transcript: TaskMessagePayload[] = [];
  const listeners = new Set<(event: AgentGuiHostEvent) => void>();
  let pending: { task_id?: string; status?: string; created_at?: string } = {};
  let rejectSend: unknown;
  const request = vi.fn(async (method: keyof AgentGuiRpcMethods, params: any): Promise<any> => {
    switch (method) {
      case "storage.read": return storage.get(params.key) ?? null;
      case "storage.write": storage.set(params.key, params.value); return;
      case "workspace.load": return { agents: init.agents, sessions };
      case "session.create": sessions.push({ ...session }); return { ...session };
      case "session.load": return { session: { ...session }, messages: [...messages], pending: { ...pending }, tasks: [...tasks] };
      case "task.messages": return [...transcript];
      case "session.watch": return;
      case "session.send": {
        if (rejectSend) throw rejectSend;
        const taskId = `server-task-${messages.length + 1}`;
        const messageId = `server-message-${messages.length + 1}`;
        messages.push({ id: messageId, chat_session_id: session.id, role: "user", content: params.content, task_id: null, created_at: time });
        pending = { task_id: taskId, status: "running", created_at: time };
        return { message_id: messageId, task_id: taskId, created_at: time, supports_queue: true };
      }
      case "session.cancel":
        tasks.push({ id: params.taskId, chat_session_id: session.id, status: "cancelled", created_at: time, completed_at: time, started_at: time } as AgentTask);
        pending = {};
        messages.splice(0);
        return tasks[tasks.length - 1];
      case "session.markRead": return;
      case "attachment.upload": return {
        id: "attachment-1", filename: params.file.name, content_type: params.file.type,
        markdown_url: "/api/attachments/attachment-1/download", url: "/api/attachments/attachment-1/download",
        size_bytes: params.file.size,
      } as Attachment;
      default: throw new Error(`Unexpected RPC: ${method}`);
    }
  });
  const client: AgentGuiRpcClient = {
    request: withLocalSnapshots(request as never) as AgentGuiRpcClient["request"],
    subscribe: (listener) => { listeners.add(listener); return () => { listeners.delete(listener); }; },
  };
  return { client, request, storage, sessions, messages, transcript, tasks,
    complete: (taskId: string) => {
      tasks.push({ id: taskId, chat_session_id: session.id, status: "completed", created_at: time, started_at: time, completed_at: time } as AgentTask);
      if (pending.task_id === taskId) pending = {};
    },
    rejectSend: (error: unknown) => { rejectSend = error; },
    emit: (event: AgentGuiHostEvent) => { for (const listener of listeners) listener(event); },
  };
}

async function setup(data = backend()) {
  const handle = createMulticaAgentGuiRuntime(data.client, init);
  handles.push(handle);
  await handle.initialize();
  return { handle, ...data };
}

describe("Multica Agent GUI runtime with the original session engine", () => {
  it.each([false, true])("queues ordinary desktop messages and sends them now without guidance (agent mention: %s)", async (mentioned) => {
    const data = backend(); data.sessions.push(session);
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => {
      if (method === "workspace.load") return { ...await original(method, params), local: true, queueOwner: "desktop" };
      if (method === "queue.enqueue") return { revision: 1, paused: true, items: [params.item] };
      if (method === "queue.update") return { revision: 2, paused: false, items: [] };
      return original(method, params);
    });
    const handle = createMulticaAgentGuiRuntime(data.client, { ...init, local: true, queueOwner: "desktop", sessions: [session] });
    handles.push(handle); await handle.initialize();
    await handle.runtime.getSession(init.workspaceId, session.id);
    const mention = mentioned ? createRichTextMentionMarkdown({ providerId: "agent-target", entityId: agent.id, label: "Agent", scope: { workspaceId: init.workspaceId } }) : "";
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    expect(engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "ordinary", content: [{ type: "text", text: `${mention}Help` }] }).accepted).toBe(true);
    await vi.waitFor(() => expect(data.request.mock.calls.filter(([method]) => method === "queue.enqueue")).toHaveLength(1));
    const admission = data.request.mock.calls.find(([method]) => method === "queue.enqueue")![1];
    expect(admission.item.references ?? []).toEqual(mentioned ? [{ kind: "agent", id: agent.id }] : []);
    expect(admission.item.targetTurnId).toBeUndefined();
    expect(admission.item.capabilityRefs).toBeUndefined();
    engine.dispatch({ type: "queue/sendNowRequested", agentSessionId: session.id, promptId: "ordinary", awaitingTurnExpiresAtUnixMs: Date.now() + 30000, cancelCommandId: "cancel", timeoutMs: 30000 });
    await vi.waitFor(() => expect(data.request).toHaveBeenCalledWith("queue.update", { sessionId: session.id, action: "sendNow", requestId: "ordinary" }));
    expect(data.request.mock.calls.filter(([method]) => method === "queue.enqueue")).toHaveLength(1);
    expect(data.request.mock.calls.filter(([method]) => method === "session.send" || method === "host.showMessage")).toEqual([]);
  });
  it("marks the send and its request under the mapped client stages, once for each attempt", async () => {
    const info = vi.spyOn(console, "info").mockImplementation(() => {});
    try {
      const data = backend(); data.sessions.push(session);
      const original = data.request.getMockImplementation()!;
      data.request.mockImplementation(async (method, params) => {
        if (method === "workspace.load") return { ...await original(method, params), local: true, queueOwner: "desktop" };
        if (method === "queue.enqueue") return { revision: 1, paused: true, items: [params.item] };
        return original(method, params);
      });
      const handle = createMulticaAgentGuiRuntime(data.client, { ...init, local: true, queueOwner: "desktop", sessions: [session] });
      handles.push(handle); await handle.initialize();
      await handle.runtime.getSession(init.workspaceId, session.id);
      handle.runtime.getSessionEngine(init.workspaceId).submitPrompt({ agentSessionId: session.id, clientSubmitId: "timed", content: [{ type: "text", text: "Help" }] });
      const records = () => info.mock.calls.map(([line]) => String(line)).filter((line) => line.startsWith(LOCAL_TIMING_PREFIX)).map((line) => JSON.parse(line.slice(LOCAL_TIMING_PREFIX.length)) as { stage: string; event: string; requestId?: string; conversationId?: string; attempt?: number }).filter((record) => record.requestId === "timed");
      await vi.waitFor(() => expect(records().map((record) => [record.stage, record.event])).toEqual([["client.send", "mark"], ["client.request", "start"], ["client.request", "end"]]));
      expect(records()[0]).toMatchObject({ conversationId: session.id, attempt: 1 });
    } finally { info.mockRestore(); }
  });
  it("keeps first-send content recoverable for an explicit queue retry after admission failure", async () => {
    const data = backend();
    const original = data.request.getMockImplementation()!;
    let admissions = 0;
    data.request.mockImplementation(async (method, params) => {
      if (method === "session.create") {
        const created = { ...await original(method, params), created_at: new Date().toISOString(), updated_at: new Date().toISOString() };
        return created;
      }
      if (method === "workspace.load") return { ...await original(method, params), local: true, queueOwner: "desktop" };
      if (method === "models.list") return { supported: true, models: [] };
      if (method === "host.showMessage") return;
      if (method === "queue.update") return { revision: 2, paused: false, items: [] };
      if (method === "queue.enqueue") {
        if (++admissions === 1) throw new Error("Initial queue admission failed");
        return { revision: 1, paused: true, items: [params.item] };
      }
      return original(method, params);
    });
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      const handle = createMulticaAgentGuiRuntime(data.client, { ...init, local: true, queueOwner: "desktop" });
      handles.push(handle); await handle.initialize();
      expect(data.request.mock.calls.filter(([method]) => method === "session.create")).toHaveLength(0);
      const engine = handle.runtime.getSessionEngine(init.workspaceId);
      const input = { mode: "new" as const, agentSessionId: "first-local-draft", agentTargetId: agent.id, cwd: "/project",
        requestId: "first-activation", clientSubmitId: "first-submit", initialContent: [{ type: "text" as const, text: "First local message" }] };
      engine.activateSession(input);
      await vi.waitFor(() => expect(engine.getSnapshot().promptQueue.recordsBySessionId[input.agentSessionId]).toMatchObject({
        failureMessage: "Initial queue admission failed", prompts: [{ clientSubmitId: "first-submit", content: input.initialContent, visibleInQueue: true }],
      }));
      await vi.waitFor(() => expect(engine.getSnapshot().pendingIntents.activationsByRequestId["first-activation"]?.status).toBe("confirmed"));
      expect(admissions).toBe(1);
      expect(handle.toServerSessionId(input.agentSessionId)).toBe(session.id);
      expect(engine.getSnapshot().pendingIntents.activationsByRequestId["first-activation"]).toMatchObject({ initialTurnExpected: false, initialPromptRetracted: false });
      engine.dispatch({ type: "queue/sendNowRequested", agentSessionId: input.agentSessionId, promptId: "first-submit",
        awaitingTurnExpiresAtUnixMs: Date.now() + 30000, cancelCommandId: "cancel", timeoutMs: 30000 });
      await vi.waitFor(() => expect(admissions).toBe(2));
      expect(data.request.mock.calls.filter(([method]) => method === "session.create")).toHaveLength(1);
      expect(data.request.mock.calls.find(([method]) => method === "session.create")?.[1]).toMatchObject({ cwd: "/project" });
      for (const [, params] of data.request.mock.calls.filter(([method]) => method === "queue.enqueue")) {
        expect(params).toMatchObject({ sessionId: session.id, item: { requestId: "first-submit", content: "First local message" } });
      }
    } finally { error.mockRestore(); }
  });
  it("reuses the original prepared image and settings on explicit admission retry and shows the failure", async () => {
    const data = backend(); data.sessions.push(session);
    const original = data.request.getMockImplementation()!;
    let admissions = 0;
    data.request.mockImplementation(async (method, params) => {
      if (method === "workspace.load") return { ...await original(method, params), local: true, queueOwner: "desktop" };
      if (method === "models.list") return { supported: true, models: [{ id: "test-model", label: "Test", thinking: { supported_levels: [{ value: "high", label: "High" }] } }] };
      if (method === "host.showMessage") return;
      if (method === "queue.enqueue") {
        if (++admissions === 1) throw new Error("The host request timed out");
        return { revision: 1, paused: true, items: [params.item] };
      }
      if (method === "queue.update") return { revision: 2, paused: false, items: [] };
      return original(method, params);
    });
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      const handle = createMulticaAgentGuiRuntime(data.client, { ...init, local: true, queueOwner: "desktop", sessions: [session] });
      handles.push(handle); await handle.initialize();
      await handle.runtime.getSession(init.workspaceId, session.id);
      await handle.setModelSelection(agent.id, session.id, "test-model");
      await handle.setReasoningSelection(agent.id, session.id, "high");
      await handle.setPermissionSelection(agent.id, session.id, "auto");
      const prepared = await handle.runtime.uploadPromptContent!({ workspaceId: init.workspaceId,
        content: [{ type: "text", text: "Use this image" }, { type: "image", name: "image.png", mimeType: "image/png", data: btoa("image bytes") }] });
      const engine = handle.runtime.getSessionEngine(init.workspaceId);
      engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "image-retry", content: prepared.content });
      await vi.waitFor(() => expect(data.request).toHaveBeenCalledWith("host.showMessage", { message: "The host request timed out", tone: "error" }));
      const initial = structuredClone(data.request.mock.calls.find(([method]) => method === "queue.enqueue")![1]);
      expect(initial.item).toMatchObject({ requestId: "image-retry", attachmentIds: ["attachment-1"], model: "test-model", reasoningEffort: "high", permissionMode: "auto" });
      await handle.setModelSelection(agent.id, session.id, "another-model");
      await handle.setPermissionSelection(agent.id, session.id, "full-access");
      engine.dispatch({ type: "queue/sendNowRequested", agentSessionId: session.id, promptId: "image-retry", awaitingTurnExpiresAtUnixMs: Date.now() + 30000, cancelCommandId: "cancel", timeoutMs: 30000 });
      await vi.waitFor(() => expect(data.request.mock.calls.filter(([method]) => method === "queue.update")).toHaveLength(1));
      const attempts = data.request.mock.calls.filter(([method]) => method === "queue.enqueue");
      expect(attempts).toHaveLength(2);
      expect(attempts[1]![1]).toEqual(initial);
      expect(data.request.mock.calls.filter(([method]) => method === "attachment.upload")).toHaveLength(1);
    } finally { consoleError.mockRestore(); }
  });
  it("keeps a local project during model changes and forwards its directory when activating a new session", async () => {
    const data = backend();
    const directory = { cwd: "/example/project", projectPath: "/example/project" };
    const localSession = { ...session, localDirectory: directory };
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => {
      if (method === "workspace.load") return { agents: init.agents, sessions: [localSession], local: true };
      if (method === "session.load") return { session: localSession, messages: [], pending: {}, tasks: [] };
      if (method === "session.create") return localSession;
      return original(method, params);
    });
    const handle = createMulticaAgentGuiRuntime(data.client, { ...init, local: true, sessions: [localSession] });
    handles.push(handle); await handle.initialize();
    const loaded = await handle.runtime.getSession(init.workspaceId, session.id);
    expect(loaded).toMatchObject({ cwd: directory.cwd, noProject: false });
    await handle.setModelSelection(agent.id, session.id, "another-model");
    expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]).toMatchObject({ cwd: directory.cwd, noProject: false, settings: { model: "another-model" } });
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    engine.activateSession({ mode: "new", agentSessionId: "project-draft", agentTargetId: agent.id,
      cwd: directory.cwd, requestId: "project-create", clientSubmitId: "project-submit" });
    await vi.waitFor(() => expect(data.request).toHaveBeenCalledWith("session.create", expect.objectContaining({ cwd: directory.cwd }), expect.anything()));
    await vi.waitFor(() => expect(handle.toServerSessionId("project-draft")).toBe(session.id));
    expect(await handle.runtime.getSession(init.workspaceId, "project-draft")).toMatchObject({ cwd: directory.cwd, noProject: false });
    const restored = createMulticaAgentGuiRuntime(data.client, { ...init, local: true, sessions: [localSession] });
    handles.push(restored); await restored.initialize();
    expect(await restored.runtime.getSession(init.workspaceId, session.id)).toMatchObject({ cwd: directory.cwd, noProject: false });
  });
  it.each(["claude-code", "codex"].flatMap((provider) => [false, true].map((desktopQueue) => ({ provider, desktopQueue }))))("loads and submits to an externally created $provider session before workspace refresh completes (desktop queue: $desktopQueue)", async ({ provider, desktopQueue }) => {
    const data = backend();
    const localInit = { ...init, local: true, queueOwner: desktopQueue ? "desktop" as const : undefined, agents: [{ ...init.agents[0]!, provider }] };
    const original = data.request.getMockImplementation()!;
    const staleWorkspace = { agents: localInit.agents, sessions: [] };
    let delayRefresh = false;
    let finishRefresh!: () => void;
    const gate = new Promise<void>((resolve) => { finishRefresh = resolve; });
    data.request.mockImplementation(async (method, params) => {
      if (method === "workspace.load") { if (delayRefresh) await gate; return staleWorkspace; }
      if (method === "queue.enqueue") return { revision: 1, paused: false, items: [params.item] };
      return original(method, params);
    });
    const handle = createMulticaAgentGuiRuntime(data.client, localInit);
    handles.push(handle);
    await handle.initialize();
    delayRefresh = true;
    const refresh = handle.runtime.load(init.workspaceId);
    // Match the host's create-success -> init -> selection/subscription order.
    await data.client.request("session.create", { agentId: agent.id });
    const errors = vi.fn();
    let release = () => {};
    const unsubscribe = data.client.subscribe((event) => {
      if (event.type === "init") release = handle.runtime.ensureSessionSynchronized!({ workspaceId: init.workspaceId, agentSessionId: session.id, onError: errors });
    });
    try {
      data.emit({ type: "init", snapshot: { ...localInit, sessions: [...data.sessions], activeSessionId: session.id } });
      await expect(handle.runtime.getSession(init.workspaceId, session.id)).resolves.toMatchObject({ agentSessionId: session.id });
      expect(handle.toServerSessionId(session.id)).toBe(session.id);
      expect(data.request.mock.calls.some(([method, params]) => method === "session.watch" && params.sessionIds.includes(session.id))).toBe(true);
      finishRefresh();
      await refresh;
      expect(handle.toServerSessionId(session.id)).toBe(session.id);
      expect(handle.runtime.getSnapshot(init.workspaceId).sessions.map((entry) => entry.agentSessionId)).toContain(session.id);
      const engine = handle.runtime.getSessionEngine(init.workspaceId);
      engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "external-first-send", content: [{ type: "text", text: "First message" }] });
      await vi.waitFor(() => expect(data.request.mock.calls.some(([method, params]) =>
        method === (desktopQueue ? "queue.enqueue" : "session.send") && params.sessionId === session.id && (desktopQueue ? params.item.content : params.content) === "First message")).toBe(true));
      expect(errors).not.toHaveBeenCalled();
      expect(data.request.mock.calls.filter(([method]) => method === "session.create")).toHaveLength(1);
    } finally {
      unsubscribe(); release(); finishRefresh(); await refresh;
    }
  });

  it("keeps init received during initialization and rapid session switches ahead of an older workspace response", async () => {
    const data = backend();
    const original = data.request.getMockImplementation()!;
    let finishRefresh!: () => void;
    const gate = new Promise<void>((resolve) => { finishRefresh = resolve; });
    data.request.mockImplementation(async (method, params) => {
      if (method === "workspace.load") { await gate; return { agents: init.agents, sessions: [] }; }
      return original(method, params);
    });
    const handle = createMulticaAgentGuiRuntime(data.client, init);
    handles.push(handle);
    const initializing = handle.initialize();
    try {
      await vi.waitFor(() => expect(data.request.mock.calls.some(([method]) => method === "workspace.load")).toBe(true));
      const second = { ...session, id: "second-session" };
      data.emit({ type: "init", snapshot: { ...init, sessions: [session], activeSessionId: session.id } });
      data.emit({ type: "init", snapshot: { ...init, sessions: [session, second], activeSessionId: second.id } });
      data.emit({ type: "init", snapshot: { ...init, sessions: [session, second], activeSessionId: session.id } });
      expect(handle.toServerSessionId(session.id)).toBe(session.id);
      expect(handle.toServerSessionId(second.id)).toBe(second.id);
      finishRefresh();
      await initializing;
      expect(handle.getWorkspaceState().sessions.map((entry) => entry.id)).toEqual([session.id, second.id]);
      expect(handle.runtime.getSnapshot(init.workspaceId).sessions.map((entry) => entry.agentSessionId)).toEqual([session.id, second.id]);
    } finally { finishRefresh(); await initializing; }
  });

  it("rejects unknown identities and ignores foreign or stale init snapshots", async () => {
    const { handle, emit, request } = await setup();
    const snapshot = { ...init, sessions: [session], activeSessionId: session.id, generation: 2 };
    emit({ type: "init", snapshot: { ...snapshot, workspaceId: "foreign-workspace" } });
    emit({ type: "init", snapshot: { ...snapshot, userId: "foreign-user" } });
    expect(handle.toServerSessionId(session.id)).toBeNull();
    await expect(handle.runtime.getSession(init.workspaceId, session.id)).rejects.toMatchObject({ code: "session_not_ready" });
    expect(request.mock.calls.filter(([method]) => method === "session.load")).toHaveLength(0);
    emit({ type: "init", snapshot });
    emit({ type: "init", snapshot: { ...snapshot, sessions: [], generation: 1 } });
    expect(handle.toServerSessionId(session.id)).toBe(session.id);
    expect(handle.toServerSessionId("unknown-session")).toBeNull();
    await expect(handle.runtime.getSession(init.workspaceId, "unknown-session")).rejects.toMatchObject({ code: "session_not_ready" });
  });

  it("migrates a browser-saved local queue once and remounts against Electron without suspending it", async () => {
    const saved = { agentSessionId: session.id, prompts: [{ id: "old-prompt", clientSubmitId: "old-submit", createdAtUnixMs: 1, content: [{ type: "text", text: "saved continuation" }] }] };
    const data = backend(new Map([["runtime-ledger-v1", JSON.stringify({ promptQueues: [saved] })]])); data.sessions.push(session);
    let queue: import("@multica/core/local-conversation/protocol").LocalPromptQueue = { revision: 0, paused: false, items: [] };
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => {
      if (method === "queue.enqueue") { queue = { revision: queue.revision + 1, paused: params.paused === true, items: [params.item] }; return structuredClone(queue); }
      const result = await original(method, params);
      if (method === "workspace.load") return { ...result, local: true, queueOwner: "desktop" };
      if (method === "session.load") return { ...result, promptQueue: structuredClone(queue) };
      return result;
    });
    const localInit = { ...init, local: true, queueOwner: "desktop" as const, activeSessionId: session.id };
    const first = createMulticaAgentGuiRuntime(data.client, localInit); handles.push(first); await first.initialize();
    expect(data.request.mock.calls.filter(([method]) => method === "queue.enqueue")).toHaveLength(1);
    expect(JSON.parse(data.storage.get("runtime-ledger-v1")!).promptQueues).toEqual([]);
    queue = { ...queue, revision: queue.revision + 1, paused: false }; first.dispose();
    const next = createMulticaAgentGuiRuntime(data.client, localInit); handles.push(next); await next.initialize();
    await next.runtime.getSession(init.workspaceId, session.id);
    const projected = next.runtime.getSessionEngine(init.workspaceId)!.getSnapshot().promptQueue.recordsBySessionId[session.id]!;
    expect(projected.suspendReason).toBeNull(); expect(projected.prompts[0]?.clientSubmitId).toBe("old-submit");
    expect(data.request.mock.calls.filter(([method]) => method === "queue.enqueue")).toHaveLength(1);
    expect(data.request.mock.calls.filter(([method]) => method === "queue.update")).toHaveLength(0);
  });
  it("remembers an already selected permission on upgrade without replacing a saved provider default", async () => {
    const data = backend(new Map([["permission-preferences-v1", JSON.stringify([[`session:${session.id}`, "auto"]])]]));
    data.sessions.push(session);
    const localInit = { ...init, local: true, activeSessionId: session.id };
    const handle = createMulticaAgentGuiRuntime(data.client, localInit); handles.push(handle); await handle.initialize();
    expect(handle.getPermissionSelection(agent.id)).toBe("auto");
    const restarted = createMulticaAgentGuiRuntime(data.client, { ...localInit, activeSessionId: null }); handles.push(restarted); await restarted.initialize();
    expect(restarted.getPermissionSelection(agent.id)).toBe("auto");
    await restarted.setPermissionSelection(agent.id, null, "default");
    const reopened = createMulticaAgentGuiRuntime(data.client, localInit); handles.push(reopened); await reopened.initialize();
    expect(reopened.getPermissionSelection(agent.id)).toBe("default");
    expect(reopened.getPermissionSelection(agent.id, session.id)).toBe("auto");
  });
  it("remembers the last local permission choice for new conversations without replacing existing permissions", async () => {
    const data = backend(); data.sessions.push(session);
    const handle = createMulticaAgentGuiRuntime(data.client, { ...init, local: true }); handles.push(handle); await handle.initialize();
    await handle.runtime.getSession(init.workspaceId, session.id);
    await handle.setPermissionSelection(agent.id, session.id, "auto");
    expect(handle.getPermissionSelection(agent.id)).toBe("auto");
    expect(handle.getPermissionSelection("other-provider")).toBe("default");
    const restored = createMulticaAgentGuiRuntime(data.client, { ...init, local: true }); handles.push(restored); await restored.initialize();
    expect(restored.getPermissionSelection(agent.id)).toBe("auto");
    expect(restored.getPermissionSelection(agent.id, session.id)).toBe("auto");
    await restored.setPermissionSelection(agent.id, null, "default");
    expect(restored.getPermissionSelection(agent.id)).toBe("default");
    expect(restored.getPermissionSelection(agent.id, session.id)).toBe("auto");
  });
  it("applies remembered permissions to empty conversations while retaining legacy history and explicit defaults", async () => {
    const data = backend(new Map([["permission-preferences-v1", JSON.stringify([["draft:agent-1", "auto"]])]]));
    const fresh = { ...session, id: "new-empty-conversation" };
    const historical = { ...session, id: "existing-history" };
    data.sessions.push(session, fresh, historical);
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => {
      const result = await original(method, params);
      if (method !== "session.load") return result;
      return { ...result, session: data.sessions.find((item) => item.id === params.sessionId),
        ...(params.sessionId === session.id ? { permissionMode: "default" } : {}),
        messages: params.sessionId === historical.id ? [{ id: "history-message", chat_session_id: historical.id, role: "user", content: "Saved history", created_at: time }] : [] };
    });
    const handle = createMulticaAgentGuiRuntime(data.client, { ...init, local: true }); handles.push(handle); await handle.initialize();
    expect(handle.getPermissionSelection(agent.id, fresh.id)).toBe("default");
    await handle.runtime.getSession(init.workspaceId, fresh.id);
    expect(handle.getPermissionSelection(agent.id, fresh.id)).toBe("auto");
    await handle.runtime.getSession(init.workspaceId, historical.id);
    expect(handle.getPermissionSelection(agent.id, historical.id)).toBe("default");
    await handle.runtime.getSession(init.workspaceId, session.id);
    expect(handle.getPermissionSelection(agent.id, session.id)).toBe("default");
  });
  it("hydrates local draft permissions before sending, binds them to the new session and prevents changes during execution", async () => {
    const data = backend(new Map([["permission-preferences-v1", JSON.stringify([["draft:agent-1", "auto"]])]]));
    const localInit = { ...init, local: true };
    const handle = createMulticaAgentGuiRuntime(data.client, localInit); handles.push(handle); await handle.initialize();
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    engine.activateSession({ mode: "new", agentSessionId: "permission-draft", agentTargetId: agent.id, requestId: "permission-activation", clientSubmitId: "permission-submit", initialContent: [{ type: "text", text: "Use Auto" }] });
    await vi.waitFor(() => expect(data.request.mock.calls.find(([method]) => method === "session.send")?.[1]).toMatchObject({ permissionMode: "auto" }));
    await expect(handle.setPermissionSelection(agent.id, "permission-draft", "full-access")).rejects.toMatchObject({ code: "run_in_progress" });
    const restored = createMulticaAgentGuiRuntime(data.client, localInit); handles.push(restored); await restored.initialize();
    expect(restored.getPermissionSelection(agent.id, session.id)).toBe("auto");
    expect(restored.getPermissionSelection("other-provider")).toBe("default");
  });
  it("preserves inherited permissions until explicitly changed, retains them after save failure and rejects cloud overrides", async () => {
    const data = backend(); data.sessions.push(session);
    const original = data.request.getMockImplementation()!;
    let failSave = false;
    data.request.mockImplementation(async (method, params) => {
      if (failSave && method === "storage.write" && params.key === "permission-preferences-v1") throw new Error("storage full");
      const result = await original(method, params);
      return method === "session.load" ? { ...result, permissionMode: "auto" } : result;
    });
    const handle = createMulticaAgentGuiRuntime(data.client, { ...init, local: true }); handles.push(handle); await handle.initialize();
    await handle.runtime.getSession(init.workspaceId, session.id);
    expect(handle.getPermissionSelection(agent.id, session.id)).toBe("auto");
    failSave = true;
    await expect(handle.setPermissionSelection(agent.id, session.id, "full-access")).rejects.toThrow("storage full");
    expect(handle.getPermissionSelection(agent.id, session.id)).toBe("auto");
    expect(handle.getPermissionSelection(agent.id)).toBe("default");
    failSave = false;
    await handle.setPermissionSelection(agent.id, session.id, "default");
    expect(handle.getPermissionSelection(agent.id, session.id)).toBe("default");
    await expect(handle.setPermissionSelection("other-provider", session.id, "auto")).rejects.toThrow();
    const cloud = (await setup(data)).handle;
    await expect(cloud.setPermissionSelection(agent.id, null, "full-access")).rejects.toMatchObject({ code: "invalid_permission_mode" });
  });
  it("moves a prepared queued message to Side only after pausing and persisting its original queue", async () => {
    const data = backend(); data.sessions.push(session);
    const original = data.request.getMockImplementation()!;
    let opened: any;
    data.request.mockImplementation(async (method, params) => {
      if (method === "host.openSide") {
        opened = params;
        const saved = JSON.parse(data.storage.get("runtime-ledger-v1")!);
        expect(saved.promptQueues[0].prompts.some((prompt: { id: string }) => prompt.id === "move-me")).toBe(true);
        return { ...session, id: "side-session" };
      }
      if (method === "host.startSide") {
        const saved = JSON.parse(data.storage.get("runtime-ledger-v1")!);
        expect(saved.promptQueues[0].prompts.map((prompt: { id: string }) => prompt.id)).toEqual(["keep-here"]);
        return undefined;
      }
      return original(method, params);
    });
    const handle = createMulticaAgentGuiRuntime(data.client, { ...init, local: true }); handles.push(handle); await handle.initialize();
    await handle.runtime.getSession(init.workspaceId, session.id);
    await handle.setPermissionSelection(agent.id, session.id, "auto");
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "active", content: [{ type: "text", text: "Original running request" }] });
    await vi.waitFor(() => expect(data.messages).toHaveLength(1));
    engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "move-me", content: [{ type: "text", text: "Side request" }, { type: "file", attachmentId: "prepared-file", name: "spec.txt" }] });
    engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "keep-here", content: [{ type: "text", text: "Stay in main queue" }] });
    await handle.moveQueuedToSide(session.id, "move-me");
    expect(opened).toMatchObject({ sessionId: session.id, requestId: `side:${session.id}:move-me`, pending: { content: "Side request", attachmentIds: ["prepared-file"], permissionMode: "auto", requestId: `side:${session.id}:move-me` } });
    expect(engine.getSnapshot().promptQueue.recordsBySessionId[session.id]?.prompts.map((prompt) => prompt.id)).toEqual(["keep-here"]);
    expect(data.request.mock.calls.filter(([method]) => method === "session.send")).toHaveLength(1);
    expect(data.request.mock.calls.some(([method]) => method === "session.cancel")).toBe(false);
  });
  it("retains a paused queue on failed Side creation and excludes desktop-owned transfers when restoring", async () => {
    const data = backend(); data.sessions.push(session);
    const original = data.request.getMockImplementation()!;
    let transferred = false;
    data.request.mockImplementation(async (method, params) => {
      if (method === "host.openSide") throw new Error("Side save failed");
      const result = await original(method, params);
      if (method === "workspace.load" && transferred) return { ...result, movedQueueRequests: [`side:${session.id}:moved`] };
      return result;
    });
    const { handle } = await setup(data); await handle.runtime.getSession(init.workspaceId, session.id);
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "active", content: [{ type: "text", text: "Running" }] });
    await vi.waitFor(() => expect(data.messages).toHaveLength(1));
    engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "moved", content: [{ type: "text", text: "Transferred elsewhere" }] });
    await expect(handle.moveQueuedToSide(session.id, "moved")).rejects.toThrow("Side save failed");
    expect(engine.getSnapshot().promptQueue.recordsBySessionId[session.id]).toMatchObject({ suspendReason: "user_stop", prompts: [{ id: "moved" }] });
    handle.dispose(); transferred = true;
    const restored = (await setup(data)).handle;
    expect(restored.runtime.getSessionEngine(init.workspaceId).getSnapshot().promptQueue.recordsBySessionId[session.id]).toBeUndefined();
    expect(data.request.mock.calls.filter(([method]) => method === "session.send")).toHaveLength(1);
  });
  it("reports a failed queue save and retains the messages for a later successful write", async () => {
    const data = backend(); data.sessions.push(session);
    const original = data.request.getMockImplementation()!;
    let storageFull = false;
    data.request.mockImplementation(async (method, params) => {
      if (method === "storage.write" && storageFull) throw new Error("Storage full");
      if (method === "host.showMessage") return;
      return original(method, params);
    });
    const errors = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      const { handle } = await setup(data);
      await handle.runtime.getSession(init.workspaceId, session.id);
      const engine = handle.runtime.getSessionEngine(init.workspaceId);
      engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "sent", content: [{ type: "text", text: "2" }] });
      await vi.waitFor(() => expect(JSON.parse(data.storage.get("runtime-ledger-v1")!).submissions.sent?.state).toBe("accepted"));
      storageFull = true;
      for (const id of ["3", "4"]) engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: id, content: [{ type: "text", text: id }] });
      await vi.waitFor(() => expect(data.request.mock.calls.filter(([method]) => method === "host.showMessage")).toHaveLength(1));
      expect(engine.getSnapshot().promptQueue.recordsBySessionId[session.id]?.prompts).toHaveLength(2);
      storageFull = false;
      engine.dispatch({ type: "submit/canceled", agentSessionId: session.id, clientSubmitId: "3" });
      await vi.waitFor(() => expect(JSON.parse(data.storage.get("runtime-ledger-v1")!).promptQueues[0].prompts.map((prompt: { id: string }) => prompt.id)).toEqual(["4"]));
    } finally { errors.mockRestore(); }
  });

  it("excludes accepted submissions from restored queues and keeps uncertain sends from reposting", async () => {
    const data = backend(); data.sessions.push(session);
    const queue = (id: string) => ({ id, clientSubmitId: id, content: [{ type: "text", text: id }], createdAtUnixMs: 1 });
    data.storage.set("runtime-ledger-v1", JSON.stringify({
      submissions: {
        delivered: { sessionId: session.id, state: "accepted", response: { message_id: "already-sent", task_id: "already-run", created_at: time } },
        uncertain: { sessionId: session.id, state: "pending" },
      },
      promptQueues: [{ agentSessionId: session.id, prompts: [queue("delivered"), queue("uncertain"), queue("waiting")] }],
    }));
    const { handle } = await setup(data);
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    expect(engine.getSnapshot().promptQueue.recordsBySessionId[session.id]).toMatchObject({ suspendReason: "user_stop", prompts: [{ id: "uncertain" }, { id: "waiting" }] });
    expect(data.request.mock.calls.filter(([method]) => method === "session.send")).toHaveLength(0);
    engine.dispatch({ type: "queue/resumed", agentSessionId: session.id });
    await vi.waitFor(() => expect(engine.getSnapshot().promptQueue.recordsBySessionId[session.id]?.failedPromptId).toBe("uncertain"));
    expect(data.request.mock.calls.filter(([method]) => method === "session.send")).toHaveLength(0);
  });

  it("persists queued content immediately and restores it paused across repeated restarts", async () => {
    const data = backend(); data.sessions.push(session);
    const { handle } = await setup(data);
    await handle.runtime.getSession(init.workspaceId, session.id);
    let engine = handle.runtime.getSessionEngine(init.workspaceId);
    engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "sent", content: [{ type: "text", text: "2" }] });
    await vi.waitFor(() => expect(data.messages).toHaveLength(1));
    const content = [{ type: "text" as const, text: "Read [@task](mention://workspace-issue/task-id)" }, { type: "file" as const, name: "report.txt", assetId: "saved-file", url: "blob:expired-preview" }];
    engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "saved-3", content });
    handle.registerAttachments([{ id: "markdown-file", filename: "report.txt", url: "/api/attachments/markdown-file/download", markdown_url: "/api/attachments/markdown-file/download" } as Attachment]);
    const lastPrompt = "4 [@report.txt](/api/attachments/markdown-file/download)";
    engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "saved-4", content: [{ type: "text", text: lastPrompt }] });
    await vi.waitFor(() => expect(JSON.parse(data.storage.get("runtime-ledger-v1")!).promptQueues?.[0]?.prompts.map((prompt: { id: string }) => prompt.id)).toEqual(["saved-3", "saved-4"]));
    handle.dispose();
    data.complete("server-task-1");
    const restored = (await setup(data)).handle;
    engine = restored.runtime.getSessionEngine(init.workspaceId);
    expect(engine.getSnapshot().promptQueue.recordsBySessionId[session.id]).toMatchObject({ suspendReason: "user_stop", prompts: [{ id: "saved-3", content: [{ text: content[0]!.text }, { name: "report.txt", assetId: "saved-file" }] }, { id: "saved-4" }] });
    expect(data.request.mock.calls.filter(([method]) => method === "session.send")).toHaveLength(1);
    engine.dispatch({ type: "submit/canceled", agentSessionId: session.id, clientSubmitId: "saved-3" });
    await vi.waitFor(() => expect(JSON.parse(data.storage.get("runtime-ledger-v1")!).promptQueues[0].prompts.map((prompt: { id: string }) => prompt.id)).toEqual(["saved-4"]));
    restored.dispose();
    const next = (await setup(data)).handle;
    engine = next.runtime.getSessionEngine(init.workspaceId);
    expect(engine.getSnapshot().promptQueue.recordsBySessionId[session.id]).toMatchObject({ suspendReason: "user_stop", prompts: [{ id: "saved-4" }] });
    engine.dispatch({ type: "queue/resumed", agentSessionId: session.id });
    await vi.waitFor(() => expect(data.messages.map((message) => message.content)).toEqual(["2", lastPrompt]));
    expect(data.request.mock.calls.filter(([method]) => method === "session.send").at(-1)?.[1].attachmentIds).toEqual(["markdown-file"]);
    await vi.waitFor(() => expect(JSON.parse(data.storage.get("runtime-ledger-v1")!).promptQueues).toEqual([]));
  });

  it("inherits a fork's native model and still allows an explicit default-model preference", async () => {
    const data = backend(); data.sessions.push(session);
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => {
      const result = await original(method, params);
      return method === "session.load" ? { ...result, model: "fork-inherited-model" } : result;
    });
    const { handle } = await setup(data);
    await handle.runtime.getSession(init.workspaceId, session.id);
    expect(handle.getModelSelection(agent.id, session.id)).toBe("fork-inherited-model");
    expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]?.settings.model).toBe("fork-inherited-model");
    await handle.setModelSelection(agent.id, session.id, null);
    expect(handle.getModelSelection(agent.id, session.id)).toBeNull();
  });
  it("executes source session settings updates and preserves them through workspace reconciliation", async () => {
    const data = backend(); data.sessions.push(session);
    const { handle } = await setup(data);
    await handle.runtime.getSession(init.workspaceId, session.id);
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    engine.updateSessionSettings({ agentSessionId: session.id, settings: { model: "runtime-catalog-model" } });
    await vi.waitFor(() => expect(handle.getModelSelection(agent.id, session.id)).toBe("runtime-catalog-model"));
    data.emit({ type: "invalidate", scope: "workspace" });
    await vi.waitFor(() => expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]?.settings.model).toBe("runtime-catalog-model"));
    expect(data.storage.get("model-preferences-v1")).toContain("runtime-catalog-model");
    expect(agent.model).toBe("test-model");
  });

  it("hydrates model settings before activation and keeps a per-turn override through real identity binding", async () => {
    const data = backend(new Map([["model-preferences-v1", JSON.stringify([["draft:agent-1", "catalog-model"]])]]));
    let release!: () => void;
    const gate = new Promise<void>((resolve) => { release = resolve; });
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => {
      if (method === "storage.read" && params.key === "model-preferences-v1") await gate;
      return original(method, params);
    });
    const handle = createMulticaAgentGuiRuntime(data.client, init);
    handles.push(handle);
    const initializing = handle.initialize();
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    engine.activateSession({ mode: "new", agentSessionId: "model-draft", agentTargetId: agent.id,
      requestId: "model-activation", clientSubmitId: "model-submit", initialContent: [{ type: "text", text: "Use selected model" }] });
    await new Promise((resolve) => setTimeout(resolve, 10));
    expect(data.request.mock.calls.some(([method]) => method === "session.send")).toBe(false);
    release(); await initializing;
    await vi.waitFor(() => expect(data.request.mock.calls.find(([method]) => method === "session.send")?.[1]).toMatchObject({ model: "catalog-model" }));
    expect(handle.getModelSelection(agent.id, "model-draft")).toBe("catalog-model");
    expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]?.settings.model).toBe("catalog-model");
    expect(agent.model).toBe("test-model");
    const restarted = createMulticaAgentGuiRuntime(data.client, init); handles.push(restarted); await restarted.initialize();
    expect(restarted.getModelSelection(agent.id, session.id)).toBe("catalog-model");
    await expect(handle.setModelSelection(agent.id, "model-draft", null)).rejects.toMatchObject({ code: "run_in_progress" });
  });

  it("keeps old model after a failed save, separates agent drafts and omits an inherited default", async () => {
    const data = backend();
    const otherAgent = { ...init.agents[0]!, agent: { ...agent, id: "other-agent" } };
    const original = data.request.getMockImplementation()!;
    let rejectSave = false;
    data.request.mockImplementation(async (method, params) => {
      if (method === "workspace.load") return { agents: [...init.agents, otherAgent], sessions: data.sessions };
      if (method === "storage.write" && params.key === "model-preferences-v1" && rejectSave) throw new Error("storage full");
      return original(method, params);
    });
    const { handle } = await setup(data);
    await handle.setModelSelection(agent.id, null, "first-model");
    rejectSave = true;
    await expect(handle.setModelSelection(agent.id, null, "rejected-model")).rejects.toThrow("storage full");
    expect(handle.getModelSelection(agent.id)).toBe("first-model");
    expect(handle.getModelSelection(otherAgent.agent.id)).toBeNull();
    rejectSave = false;
    await handle.setModelSelection(otherAgent.agent.id, null, "other-model");
    expect(handle.getModelSelection(agent.id)).toBe("first-model");
    await handle.setModelSelection(agent.id, null, null);
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    engine.activateSession({ mode: "new", agentSessionId: "default-draft", agentTargetId: agent.id,
      requestId: "default-activation", clientSubmitId: "default-submit", initialContent: [{ type: "text", text: "Use inherited default" }] });
    await vi.waitFor(() => expect(data.request.mock.calls.some(([method]) => method === "session.send")).toBe(true));
    expect(data.request.mock.calls.find(([method]) => method === "session.send")?.[1]).not.toHaveProperty("model");
  });

  it("sends selected reference identities to the parent and rejects a foreign workspace before submission", async () => {
    const { handle, request } = await setup();
    const content = ["workspace-issue", "agent-session", "agent-target"].map((providerId) => createRichTextMentionMarkdown({
      providerId, entityId: providerId === "agent-target" ? agent.id : `${providerId}-id`, label: "Reference", scope: { workspaceId: init.workspaceId },
    })).join(" ");
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    engine.activateSession({ mode: "new", agentSessionId: "reference-session", agentTargetId: agent.id,
      requestId: "reference-activation", clientSubmitId: "reference-submit", initialContent: [{ type: "text", text: content }] });
    await vi.waitFor(() => expect(request.mock.calls.some(([method]) => method === "session.send")).toBe(true));
    expect(request.mock.calls.find(([method]) => method === "session.send")?.[1]).toMatchObject({ references: [
      { kind: "issue", id: "workspace-issue-id" }, { kind: "session", id: "agent-session-id" }, { kind: "agent", id: agent.id },
    ] });
    const other = await setup();
    other.handle.runtime.getSessionEngine(init.workspaceId).activateSession({ mode: "new", agentSessionId: "foreign-session", agentTargetId: agent.id,
      requestId: "foreign-activation", clientSubmitId: "foreign-submit", initialContent: [{ type: "text", text: createRichTextMentionMarkdown({
        providerId: "workspace-issue", entityId: "foreign-issue", label: "Foreign", scope: { workspaceId: "another-workspace" },
      }) }] });
    await vi.waitFor(() => expect(other.request.mock.calls.some(([method]) => method === "session.create")).toBe(true));
    await new Promise((resolve) => setTimeout(resolve, 30));
    expect(other.request.mock.calls.filter(([method]) => method === "session.send")).toHaveLength(0);
  });
  it("does not forward the default agent's model to a mentioned agent", async () => {
    const data = backend();
    data.storage.set("model-preferences-v1", JSON.stringify([["draft:agent-1", "selected-parent-model"]]));
    const {handle}=await setup(data);
    handle.runtime.getSessionEngine(init.workspaceId).activateSession({mode:"new",agentSessionId:"invocation-session",agentTargetId:agent.id,requestId:"invoke",clientSubmitId:"invoke-submit",initialContent:[{type:"text",text:createRichTextMentionMarkdown({providerId:"agent-target",entityId:agent.id,label:"Defined reviewer",scope:{workspaceId:init.workspaceId}})}]});
    await vi.waitFor(()=>expect(data.request.mock.calls.some(([method])=>method==="session.send")).toBe(true));
    expect(data.request.mock.calls.find(([method])=>method==="session.send")?.[1]).not.toHaveProperty("model");
  });

  it.each(["codex", "claude"])("drains %s prompts in order after the window loses focus", async (provider) => {
    const data = backend();
    data.sessions.push(session);
    let focused = true;
    const original = data.request.getMockImplementation()!;
    const agents = [{ ...init.agents[0]!, provider }];
    data.request.mockImplementation(async (method, params) => {
      if (method === "session.send" && !focused && params.queued !== true) {
        throw new MulticaAgentGuiError("This Agent GUI tab is inactive", "inactive_host", 409);
      }
      const value = await original(method, params);
      const running = data.tasks.find((task) => task.status === "running");
      if (method === "workspace.load") return { ...value, agents, pendingTasks: running ? [{ task_id: running.id, chat_session_id: session.id, status: "running" }] : [] };
      if (method === "session.load") return { ...value, pending: running ? { task_id: running.id, status: "running", created_at: time } : {} };
      if (method === "session.send") data.tasks.push({ id: value.task_id, chat_session_id: session.id, status: "running", created_at: time, started_at: time } as AgentTask);
      return value;
    });
    const handle = createMulticaAgentGuiRuntime(data.client, { ...init, agents });
    handles.push(handle);
    await handle.initialize();
    await handle.runtime.getSession(init.workspaceId, session.id);
    const release = handle.runtime.ensureSessionSynchronized!({ workspaceId: init.workspaceId, agentSessionId: session.id });
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "queue-2", content: [{ type: "text", text: "2" }] });
    await vi.waitFor(() => expect(data.tasks).toHaveLength(1));
    for (const content of ["3", "4", "5", "6"]) engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: `queue-${content}`, content: [{ type: "text", text: content }] });
    expect(data.tasks).toHaveLength(1);
    focused = false;
    for (let index = 0; index < 5; index++) {
      const task = data.tasks[index]!;
      task.status = "completed";
      task.completed_at = new Date(Date.parse(time) + (index + 1) * 1000).toISOString();
      data.emit({ type: "invalidate", scope: "session", sessionId: session.id });
      data.emit({ type: "invalidate", scope: "workspace" });
      if (index < 4) await vi.waitFor(() => expect(data.tasks).toHaveLength(index + 2));
    }
    await vi.waitFor(() => expect(engine.getSnapshot().promptQueue.recordsBySessionId[session.id]).toBeUndefined());
    const sends = data.request.mock.calls.filter(([method]) => method === "session.send");
    expect(sends.map(([, params]) => params.content)).toEqual(["2", "3", "4", "5", "6"]);
    expect(sends[0]![1]).not.toHaveProperty("queued");
    for (const [, params] of sends.slice(1)) expect(params.queued).toBe(true);
    release();
  });

  it("keeps a real queued submission stable through subscription and canonical refreshes", async () => {
    const data = backend();
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => {
      const value = await original(method, params);
      if (method === "session.load" && value.pending.task_id) return {
        ...value, pending: { ...value.pending, status: "queued" },
        tasks: [{ id: value.pending.task_id, chat_session_id: session.id, status: "queued", created_at: time }],
      };
      return value;
    });
    const { handle } = await setup(data);
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    let updates = 0;
    const unsubscribe = engine.subscribe(() => { updates++; });
    engine.activateSession({ mode: "new", agentSessionId: "gui-session", agentTargetId: agent.id,
      requestId: "activation-1", clientSubmitId: "submit-1", initialContent: [{ type: "text", text: "Queued task" }] });
    const release = handle.runtime.ensureSessionSynchronized!({ workspaceId: init.workspaceId, agentSessionId: "gui-session" });
    await vi.waitFor(() => expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]).toMatchObject({ activeTurnId: "server-task-1", activeTurn: { phase: "submitted" } }));
    for (let index = 0; index < 3; index++) data.emit({ type: "invalidate", scope: "session", sessionId: session.id });
    await new Promise((resolve) => setTimeout(resolve, 100));
    const reads = data.request.mock.calls.filter(([method]) => method === "session.load").length;
    const settledUpdates = updates;
    await new Promise((resolve) => setTimeout(resolve, 100));
    expect(data.request.mock.calls.filter(([method]) => method === "session.load")).toHaveLength(reads);
    expect(updates).toBe(settledUpdates);
    expect(data.request.mock.calls.filter(([method]) => method === "session.send")).toHaveLength(1);
    expect(updates).toBeLessThan(50);
    release(); unsubscribe();
  });

  it("drains simultaneous queued-session and task invalidations without reinserting into a live Map iterator", async () => {
    const data = backend();
    data.sessions.push(session);
    const original = data.request.getMockImplementation()!;
    let unblock = () => {};
    let entered = false;
    const gate = new Promise<void>((resolve) => { unblock = resolve; });
    data.request.mockImplementation(async (method, params) => {
      const value = await original(method, params);
      if (method !== "session.load") return value;
      if (!entered) { entered = true; await gate; }
      return { ...value, pending: { task_id: "queued-task", status: "queued", created_at: time },
        tasks: [{ id: "queued-task", chat_session_id: session.id, status: "queued", created_at: time }] };
    });
    const { handle } = await setup(data);
    const release = handle.runtime.ensureSessionSynchronized!({ workspaceId: init.workspaceId, agentSessionId: session.id });
    await vi.waitFor(() => expect(entered).toBe(true));
    data.emit({ type: "invalidate", scope: "session", sessionId: session.id });
    data.emit({ type: "invalidate", scope: "task", sessionId: session.id, taskId: "queued-task" });
    data.transcript.push({ task_id: "queued-task", issue_id: "", seq: 1, type: "text", content: "Final queued diagnostic frame" });
    unblock();
    await vi.waitFor(() => expect(handle.runtime.getSnapshot(init.workspaceId).sessionMessagesById[session.id]?.some((message) => message.payload.content === "Final queued diagnostic frame")).toBe(true));
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(data.request.mock.calls.filter(([method]) => method === "session.load")).toHaveLength(2);
    expect(data.request.mock.calls.filter(([method]) => method === "task.messages").length).toBeLessThanOrEqual(3);
    expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]?.activeTurn?.phase).toBe("submitted");
    release();
  });

  it("shows a cold conversation's real pending run without inventing a start time or completion", async () => {
    const data = backend();
    data.sessions.push(session);
    let pendingTasks: PendingChatTaskItem[] = [{ task_id: "real-pending-task", chat_session_id: session.id, status: "running" }];
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => method === "workspace.load"
      ? { agents: init.agents, sessions: data.sessions, pendingTasks }
      : original(method, params));
    const { handle } = await setup(data);
    expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]).toMatchObject({
      activeTurnId: "real-pending-task", activeTurn: { turnId: "real-pending-task", phase: "running", startedAtUnixMs: 0, updatedAtUnixMs: 0 },
    });
    expect(data.request.mock.calls.filter(([method]) => method === "session.load")).toHaveLength(0);
    data.tasks.push({ id: "real-pending-task", chat_session_id: session.id, status: "completed", created_at: time, started_at: time, completed_at: time } as AgentTask);
    pendingTasks = [];
    data.emit({ type: "invalidate", scope: "workspace" });
    await vi.waitFor(() => expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]).toMatchObject({ activeTurnId: null, latestTurn: { turnId: "real-pending-task", phase: "settled", outcome: "completed", startedAtUnixMs: Date.parse(time) } }));
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(data.request.mock.calls.filter(([method]) => method === "session.load").length).toBeLessThanOrEqual(2);
  });

  it("marks a run that settles out of view as an unread completion", async () => {
    const data = backend();
    data.sessions.push(session);
    let pendingTasks: PendingChatTaskItem[] = [{ task_id: "real-pending-task", chat_session_id: session.id, status: "running" }];
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => method === "workspace.load"
      ? { agents: init.agents, sessions: data.sessions, pendingTasks }
      : original(method, params));
    const { handle } = await setup(data);
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    expect(engine.getSnapshot().attentionReadState.partitionsByUserId[init.userId]?.recordsBySessionId[session.id]).toBeUndefined();
    data.complete("real-pending-task");
    data.messages.push({ id: "reply", chat_session_id: session.id, role: "assistant", content: "Done", task_id: "real-pending-task", created_at: time } as ChatMessage);
    pendingTasks = [];
    data.emit({ type: "invalidate", scope: "workspace" });
    await vi.waitFor(() => expect(engine.getSnapshot().attentionReadState.partitionsByUserId[init.userId]?.recordsBySessionId[session.id]).toMatchObject({
      isUnread: true, kind: "completed", completionKey: `turn:${session.id}:real-pending-task:completed`,
    }));
  });

  it("does not mark already settled history unread when it is opened", async () => {
    const data = backend();
    data.sessions.push(session);
    data.complete("completed-task");
    data.messages.push({ id: "completed-message", chat_session_id: session.id, role: "assistant", content: "Completed result", task_id: "completed-task", created_at: time } as ChatMessage);
    const { handle } = await setup(data);
    await handle.runtime.getSession(init.workspaceId, session.id);
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    expect(engine.getSnapshot().attentionReadState.partitionsByUserId[init.userId]?.recordsBySessionId[session.id]).toBeUndefined();
  });

  it("refreshes previously loaded history when the aggregate reports a newly started real run", async () => {
    const data = backend();
    data.sessions.push(session);
    let pendingTasks: PendingChatTaskItem[] = [];
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => {
      if (method === "workspace.load") return { agents: init.agents, sessions: data.sessions, pendingTasks };
      const value = await original(method, params);
      if (method === "session.load") return { ...value, pending: pendingTasks[0] ? { task_id: pendingTasks[0].task_id, status: pendingTasks[0].status, created_at: time } : {} };
      return value;
    });
    const { handle } = await setup(data);
    await handle.runtime.getSession(init.workspaceId, session.id);
    pendingTasks = [{ task_id: "another-surface-run", chat_session_id: session.id, status: "running" }];
    data.emit({ type: "invalidate", scope: "workspace" });
    await vi.waitFor(() => expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]).toMatchObject({ activeTurnId: "another-surface-run", activeTurn: { phase: "running", startedAtUnixMs: Date.parse(time) } }));
  });

  it("settles completed conversation history without generating a new reconcile demand", async () => {
    const data = backend();
    data.sessions.push(session);
    data.tasks.push({ id: "completed-task", chat_session_id: session.id, status: "completed", created_at: time, started_at: time, completed_at: time } as AgentTask);
    data.messages.push({ id: "completed-message", chat_session_id: session.id, role: "assistant", content: "Completed result", task_id: "completed-task", created_at: time } as ChatMessage);
    const { handle } = await setup(data);
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => {
      if (method === "session.load" && data.request.mock.calls.filter(([name]) => name === "session.load").length > 8) return new Promise(() => {});
      return original(method, params);
    });
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    engine.dispatch({ type: "session/reconcileRequested", workspaceId: init.workspaceId, agentSessionId: session.id, needsState: true, needsMessages: true });
    await new Promise((resolve) => setTimeout(resolve, 30));
    expect(engine.getSnapshot().sessionReconcile.recordsBySessionId[session.id]).toMatchObject({ inFlightCommandId: null, pendingState: false, pendingMessages: false });
    expect(data.request.mock.calls.filter(([method]) => method === "session.load")).toHaveLength(1);
  });
  it("creates a real server session while retaining the source GUI identity and confirms the real turn", async () => {
    const { handle, request, storage } = await setup();
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    expect(engine.activateSession({
      mode: "new", agentSessionId: "gui-session", agentTargetId: agent.id,
      requestId: "activation-1", clientSubmitId: "submit-1", initialContent: [{ type: "text", text: "Inspect build" }],
    })).toBe(true);
    expect(handle.toServerSessionId("gui-session")).toBeNull();
    await vi.waitFor(() => expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]?.activeTurnId).toBe("server-task-1"));
    const snapshot = handle.runtime.getSnapshot(init.workspaceId);
    expect(snapshot.sessions[0]?.agentSessionId).toBe("gui-session");
    expect(snapshot.sessions[0]?.railSectionKey).toBe("conversations");
    expect(snapshot.sessionMessagesById["gui-session"]?.[0]).toMatchObject({
      messageId: "server-message-1", turnId: "server-task-1", payload: { clientSubmitId: "submit-1" },
    });
    expect(handle.toServerSessionId("gui-session")).toBe("server-session");
    const send = request.mock.calls.find(([method]) => method === "session.send");
    expect(send?.[1]).toMatchObject({ sessionId: "server-session", content: "Inspect build" });
    expect(storage.get("runtime-ledger-v1")).toContain("server-session");
    expect(handle.runtime.getSnapshot(init.workspaceId)).toBe(handle.runtime.getSnapshot(init.workspaceId));
  });

  it("cancels the exact Multica run and reflects authoritative message deletion", async () => {
    const { handle, request } = await setup();
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    engine.activateSession({ mode: "new", agentSessionId: "gui-session", agentTargetId: agent.id,
      requestId: "activation-1", clientSubmitId: "submit-1", initialContent: [{ type: "text", text: "Inspect build" }] });
    await vi.waitFor(() => expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]?.activeTurnId).toBe("server-task-1"));
    engine.stopSession({ agentSessionId: "gui-session" });
    await vi.waitFor(() => expect(request.mock.calls.some(([method]) => method === "session.cancel")).toBe(true));
    expect(request.mock.calls.find(([method]) => method === "session.cancel")?.[1]).toEqual({ sessionId: "server-session", taskId: "server-task-1" });
    await vi.waitFor(() => expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]?.activeTurnId).toBeNull());
    expect(handle.runtime.getSnapshot(init.workspaceId).sessionMessagesById["gui-session"]).toEqual([]);
  });

  it("restores aliases across runtime restart instead of inventing a second conversation", async () => {
    const data = backend();
    const { handle } = await setup(data);
    handle.runtime.getSessionEngine(init.workspaceId).activateSession({ mode: "new", agentSessionId: "gui-session", agentTargetId: agent.id,
      requestId: "activation-1", clientSubmitId: "submit-1" });
    await vi.waitFor(() => expect(handle.toServerSessionId("gui-session")).toBe("server-session"));
    handle.dispose();
    const next = createMulticaAgentGuiRuntime(data.client, { ...init, sessions: data.sessions });
    handles.push(next);
    await next.initialize();
    expect(next.toGuiSessionId("server-session")).toBe("gui-session");
    expect(next.runtime.getSnapshot(init.workspaceId).sessions.map((entry) => entry.agentSessionId)).toEqual(["gui-session"]);
  });

  it("preserves ambiguous sends as unknown across reload and never automatically posts them again", async () => {
    const data = backend();
    data.rejectSend(new Error("Connection lost after submission"));
    const { handle } = await setup(data);
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    engine.activateSession({ mode: "new", agentSessionId: "gui-session", agentTargetId: agent.id,
      requestId: "activation-1", clientSubmitId: "submit-1", initialContent: [{ type: "text", text: "Inspect build" }] });
    await vi.waitFor(() => expect(data.storage.get("runtime-ledger-v1")).toContain('"state":"unknown"'));
    const restored = new RuntimeLedger();
    restored.hydrate(data.storage.get("runtime-ledger-v1")!);
    expect(restored.submission("submit-1")?.state).toBe("unknown");
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(data.request.mock.calls.filter(([method]) => method === "session.send")).toHaveLength(1);
    engine.activateSession({ mode: "new", agentSessionId: "gui-session", agentTargetId: agent.id,
      requestId: "retry-activation", clientSubmitId: "submit-1", initialContent: [{ type: "text", text: "Inspect build" }] });
    await vi.waitFor(() => expect(JSON.stringify(engine.getSnapshot().pendingIntents)).toContain("submission_unconfirmed"));
    expect(data.request.mock.calls.filter(([method]) => method === "session.send")).toHaveLength(1);
    expect(data.request.mock.calls.filter(([method]) => method === "session.create")).toHaveLength(1);
  });

  it("rejects recoverable deletion and unsupported capabilities without calling a destructive API", async () => {
    const { handle, request } = await setup();
    await expect(handle.runtime.deleteSession({ workspaceId: init.workspaceId, agentSessionId: "anything" })).rejects.toMatchObject({ code: "unsupported" });
    expect(request.mock.calls.some(([method]) => method === "session.delete")).toBe(false);
    const options = await handle.runtime.getComposerOptions({ workspaceId: init.workspaceId, agentTargetId: agent.id });
    expect(options.capabilities).toMatchObject({ goalPause: false, planMode: false, modelSwitch: true, activeTurnGuidance: false });
    expect(handle.runtime.getSnapshot(init.workspaceId).composerOptionsByTargetKey?.[agent.id]).toEqual(options);
    expect(() => handle.runtime.getSessionEngine("another-workspace")).toThrow("workspace changed");
  });

  it("refreshes streaming task output without reloading the full session history", async () => {
    const data = backend();
    const { handle } = await setup(data);
    handle.runtime.getSessionEngine(init.workspaceId).activateSession({ mode: "new", agentSessionId: "gui-session", agentTargetId: agent.id,
      requestId: "activation-1", clientSubmitId: "submit-1", initialContent: [{ type: "text", text: "Inspect build" }] });
    await vi.waitFor(() => expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]?.activeTurnId).toBe("server-task-1"));
    const release = handle.runtime.ensureSessionSynchronized!({ workspaceId: init.workspaceId, agentSessionId: "gui-session" });
    await vi.waitFor(() => expect(data.request.mock.calls.some(([method]) => method === "session.watch")).toBe(true));
    await new Promise((resolve) => setTimeout(resolve, 10));
    const sessionReads = data.request.mock.calls.filter(([method]) => method === "session.load").length;
    data.transcript.push({ task_id: "server-task-1", issue_id: "", seq: 1, type: "text", content: "Live output" });
    data.emit({ type: "invalidate", scope: "task", sessionId: "server-session", taskId: "server-task-1" });
    await vi.waitFor(() => expect(handle.runtime.getSnapshot(init.workspaceId).sessionMessagesById["gui-session"]?.some((message) => message.payload.content === "Live output")).toBe(true));
    expect(data.request.mock.calls.filter(([method]) => method === "session.load")).toHaveLength(sessionReads);
    release();
  });

  it("does not lose the last stream frame when another task invalidation arrives during a read", async () => {
    const data = backend();
    const { handle } = await setup(data);
    handle.runtime.getSessionEngine(init.workspaceId).activateSession({ mode: "new", agentSessionId: "gui-session", agentTargetId: agent.id,
      requestId: "activation-1", clientSubmitId: "submit-1", initialContent: [{ type: "text", text: "Inspect build" }] });
    await vi.waitFor(() => expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]?.activeTurnId).toBe("server-task-1"));
    const release = handle.runtime.ensureSessionSynchronized!({ workspaceId: init.workspaceId, agentSessionId: "gui-session" });
    await new Promise((resolve) => setTimeout(resolve, 10));
    const original = data.request.getMockImplementation()!;
    let unblock = () => {};
    let entered = false;
    const gate = new Promise<void>((resolve) => { unblock = resolve; });
    data.request.mockImplementation(async (method, params) => {
      const result = await original(method, params);
      if (method === "task.messages" && !entered) {
        entered = true;
        await gate;
      }
      return result;
    });
    data.transcript.push({ task_id: "server-task-1", issue_id: "", seq: 1, type: "text", content: "Hello " });
    data.emit({ type: "invalidate", scope: "task", sessionId: "server-session", taskId: "server-task-1" });
    await vi.waitFor(() => expect(entered).toBe(true));
    data.transcript.push({ task_id: "server-task-1", issue_id: "", seq: 2, type: "text", content: "world" });
    data.emit({ type: "invalidate", scope: "task", sessionId: "server-session", taskId: "server-task-1" });
    unblock();
    await vi.waitFor(() => expect(handle.runtime.getSnapshot(init.workspaceId).sessionMessagesById["gui-session"]?.some((message) => message.payload.content === "Hello world")).toBe(true));
    release();
  });

  it("maps the source engine's unread-removal write to one real server mark-read", async () => {
    const data = backend();
    data.sessions.push(session);
    const key = "turn:server-session:server-task-1:completed";
    data.storage.set("attention-read-state-v1", JSON.stringify({ completed: { readIds: [], unreadIds: [key] }, failed: { readIds: [], unreadIds: [] } }));
    const { handle } = await setup(data);
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    engine.dispatch({ type: "attention/hydrateRequested", commandId: "hydrate-1", userId: init.userId, workspaceId: init.workspaceId });
    await vi.waitFor(() => expect(engine.getSnapshot().attentionReadState.partitionsByUserId[init.userId]?.hydrated).toBeTruthy());
    engine.dispatch({ type: "attention/read", userId: init.userId, agentSessionId: "server-session" });
    await vi.waitFor(() => expect(data.request.mock.calls.filter(([method]) => method === "session.markRead")).toHaveLength(1));
    expect(data.request.mock.calls.find(([method]) => method === "session.markRead")?.[1]).toEqual({ sessionId: "server-session" });
    engine.dispatch({ type: "attention/read", userId: init.userId, agentSessionId: "server-session" });
    expect(data.request.mock.calls.filter(([method]) => method === "session.markRead")).toHaveLength(1);
  });

  it("uploads real image bytes and reuses the resulting attachment identity on send", async () => {
    const { handle, request } = await setup();
    const prepared = await handle.runtime.uploadPromptContent!({ workspaceId: init.workspaceId,
      content: [{ type: "image", name: "screen.png", mimeType: "image/png", data: btoa("image bytes") }] });
    expect(prepared.content[0]).toMatchObject({ attachmentId: "attachment-1", assetId: "attachment-1", data: undefined });
    const upload = request.mock.calls.find(([method]) => method === "attachment.upload");
    expect(await upload?.[1].file.text()).toBe("image bytes");
    handle.runtime.getSessionEngine(init.workspaceId).activateSession({ mode: "new", agentSessionId: "gui-session", agentTargetId: agent.id,
      requestId: "activation-1", clientSubmitId: "submit-1", initialContent: prepared.content });
    await vi.waitFor(() => expect(request.mock.calls.some(([method]) => method === "session.send")).toBe(true));
    expect(request.mock.calls.find(([method]) => method === "session.send")?.[1]).toMatchObject({ attachmentIds: ["attachment-1"] });
    expect(request.mock.calls.filter(([method]) => method === "attachment.upload")).toHaveLength(1);
  });

  it.each([["file", "data"], ["image", "data"], ["file", "blob"], ["image", "blob"]] as const)("keeps local %s uploads from %s compact with lazy image previews", async (type, transport) => {
    const data = backend(); const original = data.request.getMockImplementation()!;
    const url = "file:///private/multica/files/stable-attachment.png";
    data.request.mockImplementation(async (method, params) => {
      if (method === "workspace.load") return { ...await original(method, params), local: true, queueOwner: "desktop" };
      if (method === "attachment.upload") return { ...await original(method, params), url, markdown_url: url };
      if (method === "queue.enqueue") return { revision: 1, paused: true, items: [params.item] };
      return original(method, params);
    });
    const handle = createMulticaAgentGuiRuntime(data.client, { ...init, local: true }); handles.push(handle); await handle.initialize();
    const blobUrl = transport === "blob" ? URL.createObjectURL(new Blob(["fixture pixels"])) : undefined;
    let prepared: Awaited<ReturnType<NonNullable<typeof handle.runtime.uploadPromptContent>>>;
    try {
      prepared = await handle.runtime.uploadPromptContent!({ workspaceId: init.workspaceId,
        content: [{ type, name: "attachment.png", mimeType: type === "image" ? "image/png" : "application/octet-stream", ...(blobUrl ? { url: blobUrl } : { data: btoa("fixture pixels") }) }] });
    } finally { if (blobUrl) URL.revokeObjectURL(blobUrl); }
    expect(prepared.content[0]).toMatchObject({ attachmentId: "attachment-1", data: undefined });
    if (type === "image") expect(prepared.content[0]).toHaveProperty("url", undefined);
    handle.runtime.getSessionEngine(init.workspaceId).activateSession({ mode: "new", agentSessionId: "gui-session", agentTargetId: agent.id,
      requestId: "activation-path", clientSubmitId: "submit-path", initialContent: prepared.content });
    await vi.waitFor(() => expect(data.request.mock.calls.some(([method]) => method === "queue.enqueue")).toBe(true));
    const item = data.request.mock.calls.find(([method]) => method === "queue.enqueue")?.[1].item;
    expect(item.attachmentIds).toEqual(["attachment-1"]);
    expect(JSON.stringify(item).includes("base64")).toBe(false);
    expect(JSON.stringify(item).length).toBeLessThan(3000);
    expect(item.content).toContain(type === "image" ? "[Image attachment:" : url);
  });

  it("waits for server identity when a new GUI conversation subscribes before creation completes", async () => {
    const data = backend();
    const original = data.request.getMockImplementation()!;
    let completeCreation = () => {};
    const gate = new Promise<void>((resolve) => { completeCreation = resolve; });
    data.request.mockImplementation(async (method, params) => {
      if (method === "session.create") await gate;
      return original(method, params);
    });
    const { handle } = await setup(data);
    handle.runtime.getSessionEngine(init.workspaceId).activateSession({ mode: "new", agentSessionId: "gui-session", agentTargetId: agent.id,
      requestId: "activation-1", clientSubmitId: "submit-1", initialContent: [{ type: "text", text: "Inspect build" }] });
    const release = handle.runtime.ensureSessionSynchronized!({ workspaceId: init.workspaceId, agentSessionId: "gui-session" });
    await vi.waitFor(() => expect(data.request.mock.calls.some(([method]) => method === "session.create")).toBe(true));
    expect(data.request.mock.calls.filter(([method]) => method === "session.load")).toHaveLength(0);
    expect(data.request.mock.calls.filter(([method]) => method === "session.watch").every(([, params]) => !params.sessionIds.includes("gui-session"))).toBe(true);
    completeCreation();
    await vi.waitFor(() => expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]?.activeTurnId).toBe("server-task-1"));
    expect(data.request.mock.calls.filter(([method]) => method === "session.load").every(([, params]) => params.sessionId === "server-session")).toBe(true);
    expect(data.request.mock.calls.some(([method, params]) => method === "session.watch" && params.sessionIds.includes("server-session"))).toBe(true);
    release();
  });

  it("binds source file mention locators only to exactly registered attachment URLs", async () => {
    const { handle, request } = await setup();
    handle.registerAttachments([{ id: "real-file", filename: "report.txt", markdown_url: "/api/attachments/real-file/download", url: "/api/attachments/real-file/download" } as Attachment]);
    const realHref = `mention://file/${encodeURIComponent("/api/attachments/real-file/download")}`;
    const lookalikeHref = `mention://file/${encodeURIComponent("/other/report.txt")}`;
    handle.runtime.getSessionEngine(init.workspaceId).activateSession({ mode: "new", agentSessionId: "gui-session", agentTargetId: agent.id,
      requestId: "activation-1", clientSubmitId: "submit-1", initialContent: [{ type: "text", text: `Read [@report.txt](${realHref}) and [@report.txt](${lookalikeHref})` }] });
    await vi.waitFor(() => expect(request.mock.calls.some(([method]) => method === "session.send")).toBe(true));
    expect(request.mock.calls.find(([method]) => method === "session.send")?.[1].attachmentIds).toEqual(["real-file"]);
  });

  it("binds ordinary source Markdown files to their registered real attachment IDs", async () => {
    const { handle, request } = await setup();
    handle.registerAttachments([
      { id: "real-file", filename: "report.txt", markdown_url: "http://localhost:6099/api/attachments/real-file/download", url: "http://localhost:6099/api/attachments/real-file/download" } as Attachment,
      { id: "other-file", filename: "report.txt", markdown_url: "http://localhost:6099/api/attachments/other-file/download", url: "http://localhost:6099/api/attachments/other-file/download" } as Attachment,
    ]);
    handle.runtime.getSessionEngine(init.workspaceId).activateSession({ mode: "new", agentSessionId: "gui-session", agentTargetId: agent.id,
      requestId: "activation-1", clientSubmitId: "submit-1", initialContent: [{ type: "text", text: "Read [@report.txt](http://localhost:6099/api/attachments/real-file/download) and [@report.txt](https://example.test/report.txt)" }] });
    await vi.waitFor(() => expect(request.mock.calls.some(([method]) => method === "session.send")).toBe(true));
    expect(request.mock.calls.find(([method]) => method === "session.send")?.[1].attachmentIds).toEqual(["real-file"]);
  });

  it("keeps archived history visible and blocks execution when the agent is no longer callable", async () => {
    const data = backend();
    data.sessions.push({ ...session, status: "archived" });
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => {
      const result = await original(method, params);
      if (method === "workspace.load") return { ...result, agents: [{ ...init.agents[0], canInvoke: false }] };
      if (method === "session.load") return { ...result, session: { ...session, status: "archived" } };
      return result;
    });
    const { handle } = await setup(data);
    await handle.runtime.getSession(init.workspaceId, session.id);
    expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]).toMatchObject({ visible: true, resumable: false, agentTargetId: agent.id });
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    expect(engine.getSnapshot().sessionLifecycle.operationBySessionId[session.id]?.runtimeAvailability.state).toBe("blocked");
    engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "read-only-submit", content: [{ type: "text", text: "must not run" }] });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(data.request.mock.calls.filter(([method]) => method === "session.send")).toHaveLength(0);
  });
});

describe("session projection cursors and execution messages", () => {
  it("omits internal local image markers from message text while retaining image blocks and raw history", () => {
    const content = "Can you see it?\n\n[Image attachment: screen.png]";
    const detail: AgentGuiSessionDetail = { session: { ...session, agent_id: "local:codex" }, pending: {}, messages: [
      { id: "image-message", role: "user", content, chat_session_id: session.id, task_id: null, created_at: time,
        attachments: [{ id: "image", filename: "screen.png", content_type: "image/png", url: "data:image/png;base64,cGl4ZWxz", size_bytes: 6 }] as Attachment[] },
    ] };
    const projected = projectDetail(detail, undefined, new Map(), new RuntimeLedger());
    expect(projected.messages[0]?.payload).toMatchObject({ text: "Can you see it?", content: [{ type: "text", text: "Can you see it?" }, { type: "image", attachmentId: "image" }] });
    expect((projected.messages[0]?.payload.content as { url?: string }[])[1]?.url).toBeUndefined();
    expect(detail.messages[0]?.content).toBe(content);
    const cloud = projectDetail({ ...detail, session }, undefined, new Map(), new RuntimeLedger());
    expect(cloud.messages[0]?.payload.text).toBe(content);
  });
  it.each([false, true])("keeps connection retries separate from assistant replies when completed=%s", (completed) => {
    const taskId = "connection-run";
    const warning = (attempt: number) => `Reconnecting... ${attempt}/5 (stream disconnected before completion: websocket closed by server before response.completed)`;
    const reply = { id: "reply", chat_session_id: session.id, task_id: taskId, role: "assistant", content: "Actual answer", created_at: time } as ChatMessage;
    const detail: AgentGuiSessionDetail = { session, pending: completed ? {} : { task_id: taskId, status: "running" }, messages: completed ? [reply] : [],
      tasks: [{ id: taskId, status: completed ? "completed" : "running", created_at: time, ...(completed ? { completed_at: time } : {}) }] as AgentTask[] };
    const events = [
      { task_id: taskId, issue_id: "", seq: 1, type: "status", content: warning(2) },
      { task_id: taskId, issue_id: "", seq: 2, type: "status", content: warning(3) },
      { task_id: taskId, issue_id: "", seq: 3, type: "text", content: "Actual answer" },
    ] satisfies AgentGuiTaskMessagePayload[];
    const result = projectDetail(detail, executionTargets(init)[0], new Map([[taskId, events]]), new RuntimeLedger(), "zh-CN");
    const notices = result.messages.filter((message) => message.payload.kind === "agent_system_notice");
    expect(notices).toHaveLength(completed ? 0 : 2);
    if (!completed) expect(notices[1]).toMatchObject({ payload: { noticeKind: "transport_retry", title: "连接中断，正在重连（3/5）", detail: warning(3), retryable: true }, semantics: { userVisibleAssistantResponse: false, turnSettling: false } });
    const answers = result.messages.filter((message) => message.kind === "text" && message.payload.kind !== "agent_system_notice");
    expect(answers).toHaveLength(1);
    expect(answers[0]?.payload.content).toBe("Actual answer");
    expect(result.session.activeTurnId).toBe(completed ? null : taskId);
  });
  it.each(["completed", "failed", "cancelled"] as const)("updates a background record in place to %s without settling the foreground or fabricating an assistant reply", (status) => {
    const ledger = new RuntimeLedger();
    const createdAt = "2026-10-24T01:00:01Z";
    const detail: AgentGuiSessionDetail = { session, pending: { task_id: "foreground", status: "running" }, messages: [],
      tasks: [{ id: "origin", status: "completed", created_at: time, completed_at: time }, { id: "foreground", status: "running", created_at: time }] as AgentTask[],
      backgroundTasks: [{ turnId: "origin", task: { id: "watcher", description: "Watch PR", status: "running", createdAt } }],
    };
    const running = projectDetail(detail, executionTargets(init)[0], new Map(), ledger, "zh-CN");
    expect(running.messages).toHaveLength(1);
    expect(running.messages[0]).toMatchObject({ messageId: "background:origin:watcher", turnId: "origin", status: "running",
      payload: { kind: "agent_system_notice", noticeKind: "background_task", title: "后台任务运行中 · Watch PR" }, semantics: { userVisibleAssistantResponse: false, turnSettling: false }, occurredAtUnixMs: Date.parse(createdAt) });
    Object.assign(detail.backgroundTasks![0]!.task, { status, summary: "Native task result" });
    const done = projectDetail(detail, executionTargets(init)[0], new Map(), ledger);
    expect(done.messages).toHaveLength(1);
    expect(done.messages[0]).toMatchObject({ messageId: running.messages[0]!.messageId, status, occurredAtUnixMs: Date.parse(createdAt), payload: { content: "Native task result" } });
    expect(done.messages[0]!.version).toBeGreaterThan(running.messages[0]!.version);
    expect(done.session.activeTurnId).toBe("foreground");
    expect(done.turns.find((turn) => turn.turnId === "origin")?.phase).toBe("settled");
    detail.backgroundTasks![0]!.task.ambient = true;
    expect(projectDetail(detail, executionTargets(init)[0], new Map(), ledger).messages).toHaveLength(0);
  });
  it("shows the authored prompt while retaining execution context in server history", () => {
    const context = "\n\n<!-- multica-context-snapshot:start -->\nReference data\n<!-- multica-context-snapshot:end -->";
    const detail = { session, pending: {}, messages: [{ id: "reference-message", chat_session_id: session.id, role: "user", content: `Review${context}`, created_at: time }] as ChatMessage[] };
    const projected = projectDetail(detail, executionTargets(init)[0], new Map(), new RuntimeLedger());
    expect(projected.messages[0]?.payload.content).toBe("Review");
    expect(detail.messages[0]?.content).toContain("Reference data");
  });
  it("pairs calls within each task, merges stream fragments, replaces final text and preserves attachment identities", () => {
    const ledger = new RuntimeLedger();
    const detail = {
      session, pending: {}, messages: [
        { id: "user-1", role: "user", content: "Inspect screenshot", task_id: "task-1", chat_session_id: session.id, created_at: time,
          attachments: [{ id: "attachment-1", filename: "screen.png", content_type: "image/png", markdown_url: "/attachment", url: "/attachment", size_bytes: 4 }] },
      ] as ChatMessage[],
    };
    const transcript = new Map<string, TaskMessagePayload[]>([
      ["task-1", [
        { task_id: "task-1", issue_id: "", seq: 1, type: "text", content: "Hello " },
        { task_id: "task-1", issue_id: "", seq: 2, type: "text", content: "world" },
        { task_id: "task-1", issue_id: "", seq: 3, type: "tool_use", call_id: "same-id", tool: "Read", input: { path: "file" } },
        { task_id: "task-1", issue_id: "", seq: 4, type: "tool_result", call_id: "same-id", tool: "Read", output: "contents", output_truncated: true },
      ]],
      ["task-2", [{ task_id: "task-2", issue_id: "", seq: 1, type: "tool_use", call_id: "same-id", tool: "Write" }]],
    ]);
    const first = projectDetail(detail, executionTargets(init)[0], transcript, ledger);
    const text = first.messages.find((message) => message.payload.content === "Hello world");
    expect(text).toBeDefined();
    const calls = first.messages.filter((message) => message.kind === "tool_call");
    expect(calls).toHaveLength(2);
    expect(calls.find((message) => message.payload.callId === "task-1:same-id")).toMatchObject({ status: "completed", payload: { callId: "task-1:same-id", input: { path: "file" }, output: "contents", outputTruncated: true } });
    const image = (first.messages[0]!.payload.content as any[]).find((block) => block.type === "image");
    expect(image.attachmentId).toBe("attachment-1");
    const second = projectDetail(detail, executionTargets(init)[0], transcript, ledger);
    expect(second.messages.map((message) => message.version)).toEqual(first.messages.map((message) => message.version));
    detail.messages.push({ id: "final-1", role: "assistant", content: "Hello world", task_id: "task-1", chat_session_id: session.id, created_at: time });
    const final = projectDetail(detail, executionTargets(init)[0], transcript, ledger);
    expect(final.messages.filter((message) => message.payload.content === "Hello world")).toHaveLength(1);
    expect(final.messages.find((message) => message.messageId === "final-1")!.version).toBeGreaterThan(ledger.latestVersion(session.id) - 1);
  });
});

it("projects a single authored input and both named agent results from durable invocation runs", () => {
  const tasks=[{id:"run-design",agent_id:"design",chat_invocation_group_id:"group",chat_invoked_agent_name:"设计评审师"},{id:"run-code",agent_id:"code",chat_invocation_group_id:"group",chat_invoked_agent_name:"代码审查师"}].map((item)=>({...item,chat_session_id:session.id,status:"completed",created_at:time,completed_at:time})) as AgentTask[];
  const detail={session,pending:{},tasks,messages:tasks.flatMap((task)=>[{id:`input-${task.id}`,chat_session_id:session.id,role:"user",content:"Review this change",task_id:task.id,created_at:time},{id:`reply-${task.id}`,chat_session_id:session.id,role:"assistant",content:`Result ${task.agent_id}`,task_id:task.id,created_at:time}]) as ChatMessage[]};
  const result=projectDetail(detail,executionTargets(init)[0],new Map(),new RuntimeLedger());
  expect(result.messages.filter((item)=>item.role==="user")).toHaveLength(1);
  expect(result.messages.filter((item)=>item.role==="assistant").map((item)=>item.payload)).toEqual([
    { text: "Result design", content: "Result design", invokedAgent: { id: "design", name: "设计评审师" } },
    { text: "Result code", content: "Result code", invokedAgent: { id: "code", name: "代码审查师" } },
  ]);
  const transcript = new Map<string, AgentGuiTaskMessagePayload[]>([["run-code", [
    { issue_id: "", task_id: "run-code", seq: 1, type: "text", content: "Result " },
    { issue_id: "", task_id: "run-code", seq: 2, type: "text", content: "code" },
  ]]]);
  const streaming = projectDetail({ ...detail, messages: detail.messages.filter((item) => item.task_id !== "run-code") }, executionTargets(init)[0], transcript, new RuntimeLedger());
  expect(streaming.messages.filter((item) => item.role === "assistant").map((item) => item.payload)).toEqual([
    result.messages.find((item) => item.messageId === "reply-run-design")!.payload,
    { content: "Result code", invokedAgent: { id: "code", name: "代码审查师" } },
  ]);
  const restored = projectDetail(detail, executionTargets(init)[0], transcript, new RuntimeLedger());
  expect(restored.messages.filter((item) => item.role === "assistant")).toHaveLength(2);
  expect(restored.messages.find((item) => item.messageId === "reply-run-code")!.payload).toEqual(result.messages.find((item) => item.messageId === "reply-run-code")!.payload);
  expect(result.turns).toHaveLength(2);
});


describe("local thinking settings", () => {
  it("keeps the saved choice on write failure, respects refreshed capabilities and preserves an explicit default during activation", async () => {
    const data = backend();
    const original = data.request.getMockImplementation()!;
    let rejectSave = false;
    let levels = ["low", "high"];
    data.request.mockImplementation(async (method, params) => {
      if (method === "models.list") return { supported: true, models: [{ id: "test-model", label: "Test", thinking: { supported_levels: levels.map((value) => ({ value, label: value })) } }] };
      if (method === "storage.write" && params.key === "reasoning-preferences-v1" && rejectSave) throw new Error("storage full");
      return original(method, params);
    });
    const handle = createMulticaAgentGuiRuntime(data.client, { ...init, local: true }); handles.push(handle);
    await handle.initialize();
    await handle.setModelSelection(agent.id, null, "test-model");
    await handle.setReasoningSelection(agent.id, null, "high");
    rejectSave = true;
    await expect(handle.setReasoningSelection(agent.id, null, "low")).rejects.toThrow("storage full");
    expect(handle.getReasoningSelection(agent.id)).toBe("high");
    rejectSave = false; levels = ["low"];
    await handle.loadModelCatalog(agent.id, { force: true });
    expect(handle.getReasoningSelection(agent.id)).toBeNull();
    await handle.setReasoningSelection(agent.id, null, null);
    handle.runtime.getSessionEngine(init.workspaceId).activateSession({ mode: "new", agentSessionId: "default-draft", agentTargetId: agent.id,
      settings: { reasoningEffort: "high" }, requestId: "default-activation", clientSubmitId: "default-submit", initialContent: [{ type: "text", text: "Default" }] });
    await vi.waitFor(() => expect(data.request.mock.calls.find(([method]) => method === "session.send")?.[1]).toMatchObject({ reasoningEffort: null }));
    await expect(handle.setReasoningSelection(agent.id, "default-draft", "low")).rejects.toMatchObject({ code: "run_in_progress" });
  });
  it.each([undefined, "desktop"] as const)("keeps draft selection through activation, delivery, and restart (queue owner: %s)", async (queueOwner) => {
    const data = backend();
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => {
      if (method === "models.list") return { supported: true, models: [{ id: "test-model", label: "Test", thinking: { supported_levels: [{ value: "low", label: "Low" }, { value: "high", label: "High" }] } }] };
      if (method === "queue.enqueue") return { revision: 1, paused: false, items: [params.item] };
      const result = await original(method, params);
      return method === "workspace.load" ? { ...result, local: true, queueOwner } : result;
    });
    const handle = createMulticaAgentGuiRuntime(data.client, { ...init, local: true }); handles.push(handle);
    await handle.initialize();
    await handle.setModelSelection(agent.id, null, "test-model");
    await handle.setReasoningSelection(agent.id, null, "high");
    const options = await handle.runtime.getComposerOptions({ workspaceId: init.workspaceId, agentTargetId: agent.id });
    expect(options).toMatchObject({ reasoningConfigurable: true, reasoningEfforts: [{ value: "low", label: "Low" }, { value: "high", label: "High" }], effectiveSettings: { reasoningEffort: "high" } });
    handle.runtime.getSessionEngine(init.workspaceId).activateSession({ mode: "new", agentSessionId: "thinking-draft", agentTargetId: agent.id,
      requestId: "thinking-activation", clientSubmitId: "thinking-submit", initialContent: [{ type: "text", text: "Think" }] });
    const method = queueOwner ? "queue.enqueue" : "session.send";
    await vi.waitFor(() => expect(data.request.mock.calls.find(([name]) => name === method)?.[1]).toMatchObject(queueOwner ? { item: { reasoningEffort: "high", model: "test-model" } } : { reasoningEffort: "high", model: "test-model" }));
    expect(handle.getReasoningSelection(agent.id, "thinking-draft")).toBe("high");
    const restarted = createMulticaAgentGuiRuntime(data.client, { ...init, local: true }); handles.push(restarted);
    await restarted.initialize();
    await restarted.runtime.getSession(init.workspaceId, session.id);
    expect(restarted.getReasoningSelection(agent.id, session.id)).toBe("high");
    await restarted.setModelSelection(agent.id, session.id, "unsupported-model");
    expect(restarted.getReasoningSelection(agent.id, session.id)).toBeNull();
    await expect(restarted.setReasoningSelection(agent.id, session.id, "high")).rejects.toMatchObject({ code: "invalid_reasoning_effort" });
  });
  it("reads a saved Claude default as following the default, keeping thinking settings available", async () => {
    const storage = new Map([
      ["model-preferences-v1", JSON.stringify([[`draft:${agent.id}`, "default"]])],
      ["reasoning-preferences-v1", JSON.stringify([[`draft:${agent.id}`, { model: "default", effort: "low" }]])],
    ]);
    const data = backend(storage);
    const original = data.request.getMockImplementation()!;
    const claude = [{ ...init.agents[0]!, provider: "claude", agent: { ...agent, name: "Claude Code", model: "" } }];
    data.request.mockImplementation(async (method, params) => {
      if (method === "models.list") return { supported: true, models: [{ id: "sonnet", label: "Sonnet", isDefault: true, thinking: { supported_levels: [{ value: "low", label: "Low" }, { value: "high", label: "High" }] } }] };
      if (method === "workspace.load") return { ...(await original(method, params)), local: true, agents: claude };
      return original(method, params);
    });
    const handle = createMulticaAgentGuiRuntime(data.client, { ...init, local: true, agents: claude }); handles.push(handle);
    await handle.initialize();
    const options = await handle.runtime.getComposerOptions({ workspaceId: init.workspaceId, agentTargetId: agent.id });
    expect(options).toMatchObject({ reasoningConfigurable: true, effectiveSettings: { reasoningEffort: "low" } });
    await handle.setReasoningSelection(agent.id, null, "high");
    expect(handle.getReasoningSelection(agent.id)).toBe("high");
    expect(handle.getModelSelection(agent.id)).toBeNull();
  });
  it("leaves a model called default alone for other providers", async () => {
    const data = backend(new Map([["model-preferences-v1", JSON.stringify([[`draft:${agent.id}`, "default"]])]]));
    const handle = createMulticaAgentGuiRuntime(data.client, { ...init, local: true }); handles.push(handle);
    await handle.initialize();
    expect(handle.getModelSelection(agent.id)).toBe("default");
  });
  it("rejects thinking changes on managed agent connections", async () => {
    const data = backend();
    const { handle } = await setup(data);
    await expect(handle.setReasoningSelection(agent.id, null, "high")).rejects.toMatchObject({ code: "invalid_reasoning_effort" });
    expect(handle.getReasoningSelection(agent.id)).toBeNull();
  });
});

describe("local conversations over protocol v2", () => {
  async function localRuntime() {
    const data = backend(); data.sessions.push(session);
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => {
      if (method === "workspace.load") return { ...await original(method, params), local: true };
      if (method === "models.list") return { supported: true, models: [] };
      return original(method, params);
    });
    const handle = createMulticaAgentGuiRuntime(data.client, { ...init, local: true, sessions: [session] });
    handles.push(handle); await handle.initialize();
    const release = handle.runtime.ensureSessionSynchronized!({ workspaceId: init.workspaceId, agentSessionId: session.id });
    await vi.waitFor(() => expect(data.request.mock.calls.some(([method]) => method === "session.load")).toBe(true));
    await new Promise((resolve) => setTimeout(resolve, 10));
    const messages = () => (handle.runtime.getSnapshot(init.workspaceId).sessionMessagesById[session.id] ?? []) as { messageId: string; payload: Record<string, unknown> }[];
    return { data, handle, messages, release };
  }
  const event = (seq: number, type: string, payload: Record<string, unknown>, extra: Record<string, unknown> = {}) =>
    ({ seq, turn_id: "turn-1", turn_seq: seq, item_id: null, type, final: type === "item.completed" || type === "turn.completed", created_at: time, payload, ...extra });
  const batch = (events: ReturnType<typeof event>[]) => ({ version: 2, type: "conversation.events", conversation_id: session.id, from_seq: events[0]!.seq, to_seq: events.at(-1)!.seq, events });

  it("projects streamed events of a subscribed conversation", async () => {
    const { data, messages } = await localRuntime();
    expect(data.request.mock.calls.filter(([method]) => method === "session.load")).toHaveLength(1);
    data.emit({ type: "local.events", sessionId: session.id, message: batch([
      event(1, "user.message", { message_id: "m1", content: "hi" }),
      event(2, "turn.started", { request_id: "r1" }),
      event(3, "item.delta", { kind: "assistant_text", field: "text", text: "Hel" }, { item_id: "it-1" }),
    ]) });
    await vi.waitFor(() => expect(messages().some((message) => message.messageId === "it-1" && message.payload.content === "Hel")).toBe(true));
    data.emit({ type: "local.events", sessionId: session.id, message: batch([event(4, "item.completed", { kind: "assistant_text", status: "completed", text: "Hello!" }, { item_id: "it-1" })]) });
    await vi.waitFor(() => expect(messages().find((message) => message.messageId === "it-1")?.payload.content).toBe("Hello!"));
    // Live events never re-read the conversation.
    expect(data.request.mock.calls.filter(([method]) => method === "session.load")).toHaveLength(1);
  });

  it("marks the first model output received and applied, not the user's message", async () => {
    const info = vi.spyOn(console, "info").mockImplementation(() => {});
    try {
      const stages = () => info.mock.calls.map(([line]) => String(line)).filter((line) => line.startsWith(LOCAL_TIMING_PREFIX)).map((line) => JSON.parse(line.slice(LOCAL_TIMING_PREFIX.length)) as { stage: string; turnId?: string }).filter((record) => record.turnId === "turn-1").map((record) => record.stage);
      const { data, messages } = await localRuntime();
      data.emit({ type: "local.events", sessionId: session.id, message: batch([event(1, "user.message", { message_id: "m1", content: "hi" }), event(2, "turn.started", { request_id: "r1" })]) });
      await new Promise((resolve) => setTimeout(resolve, 10));
      expect(stages()).toEqual([]);
      data.emit({ type: "local.events", sessionId: session.id, message: batch([event(3, "item.delta", { kind: "assistant_text", field: "text", text: "Hel" }, { item_id: "it-1" })]) });
      data.emit({ type: "local.events", sessionId: session.id, message: batch([event(4, "item.delta", { kind: "assistant_text", field: "text", text: "lo" }, { item_id: "it-1" })]) });
      await vi.waitFor(() => expect(messages().some((message) => message.messageId === "it-1")).toBe(true));
      await vi.waitFor(() => expect(stages()).toEqual(["client.first_event_received", "client.first_event_applied"]));
    } finally { info.mockRestore(); }
  });

  it("continues a stopped turn from the empty composer", async () => {
    const { data, handle } = await localRuntime();
    expect(handle.continuable(session.id)).toBe(false);
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => method === "session.load"
      ? { ...await original(method, params), tasks: [{ id: "turn-1", status: "cancelled", created_at: time }] }
      : original(method, params));
    data.emit({ type: "local.reload", sessionId: session.id });
    await vi.waitFor(() => expect(handle.continuable(session.id)).toBe(true));
    handle.continueSession(session.id);
    await vi.waitFor(() => expect(data.request.mock.calls.some(([method, params]) => ["session.send", "queue.enqueue"].includes(method) && JSON.stringify(params).includes("Continue"))).toBe(true));
  });

  it("reloads the snapshot after a gap or an ended subscription", async () => {
    const { data } = await localRuntime();
    data.emit({ type: "local.events", sessionId: session.id, message: batch([event(5, "turn.started", { request_id: "r1" })]) });
    await vi.waitFor(() => expect(data.request.mock.calls.filter(([method]) => method === "session.load")).toHaveLength(2));
    data.emit({ type: "local.reload", sessionId: session.id });
    await vi.waitFor(() => expect(data.request.mock.calls.filter(([method]) => method === "session.load")).toHaveLength(3));
  });
});
