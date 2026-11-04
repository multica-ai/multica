import { executionTargets } from "../protocol";
// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Agent, AgentTask, Attachment, ChatMessage, ChatSession, PendingChatTaskItem } from "@multica/core/types";
import type { AgentGuiHostEvent, AgentGuiInitSnapshot, AgentGuiRpcClient, AgentGuiRpcMethods } from "../protocol";
import { LOCAL_TIMING_PREFIX } from "../local-timing";
import { createMulticaAgentGuiRuntime as createRuntime, MulticaAgentGuiError } from "./create-multica-agent-gui-runtime";
import { RuntimeLedger } from "./ledger";
import { createRichTextMentionMarkdown } from "@tutti-os/ui-rich-text/core";
import { localSourceFromClient } from "./local-snapshot-test-host";

const createMulticaAgentGuiRuntime = (client: AgentGuiRpcClient, init: AgentGuiInitSnapshot) => createRuntime(client, init, localSourceFromClient(client as never));

const time = "2026-10-24T01:00:00Z";
const agent = { id: "agent-1", name: "Codex", model: "test-model", archived_at: null } as Agent;
const session = {
  id: "server-session", workspace_id: "workspace-1", agent_id: agent.id,
  creator_id: "member-1", title: "Example", status: "active",
  created_at: time, updated_at: time, has_unread: false,
} as ChatSession;
const init: AgentGuiInitSnapshot = {
  workspaceId: "workspace-1", workspaceSlug: "dev", userId: "member-1",
  locale: "en", theme: "dark", activeSessionId: null, local: true, queueOwner: "desktop",
  agents: [{ agent, provider: "codex", iconUrl: "", canInvoke: true, runtimeBound: true, runtimeOnline: true }],
  sessions: [],
};
const handles: ReturnType<typeof createMulticaAgentGuiRuntime>[] = [];
afterEach(() => { for (const handle of handles.splice(0)) handle.dispose(); });

function backend(storage = new Map<string, string>()) {
  const sessions: ChatSession[] = [];
  const messages: ChatMessage[] = [];
  const tasks: AgentTask[] = [];
  const listeners = new Set<(event: AgentGuiHostEvent) => void>();
  let pending: { task_id?: string; status?: string; created_at?: string } = {};
  let rejectSend: unknown;
  const request = vi.fn(async (method: keyof AgentGuiRpcMethods, params: any): Promise<any> => {
    switch (method) {
      case "storage.read": return storage.get(params.key) ?? null;
      case "storage.write": storage.set(params.key, params.value); return;
      case "workspace.load": return { agents: init.agents, sessions, local: true, queueOwner: "desktop" };
      case "session.create": sessions.push({ ...session }); return { ...session };
      case "session.load": return { session: { ...session }, messages: [...messages], pending: { ...pending }, tasks: [...tasks] };
      case "queue.enqueue": {
        if (rejectSend) throw rejectSend;
        const taskId = `server-task-${messages.length + 1}`;
        messages.push({ id: `server-message-${messages.length + 1}`, chat_session_id: session.id, role: "user", content: params.item.content, task_id: null, created_at: time });
        pending = { task_id: taskId, status: "running", created_at: time };
        return { revision: messages.length, paused: false, items: [] };
      }
      case "models.list": return { id: "catalog", runtime_id: params.agentId, status: "completed", supported: true, models: [] };
      case "queue.update": return { revision: messages.length, paused: false, items: [] };
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
      case "attachment.upload": return {
        id: "attachment-1", filename: params.file.name, content_type: params.file.type,
        markdown_url: "/api/attachments/attachment-1/download", url: "/api/attachments/attachment-1/download",
        size_bytes: params.file.size,
      } as Attachment;
      default: throw new Error(`Unexpected RPC: ${method}`);
    }
  });
  const client: AgentGuiRpcClient = {
    request: request as unknown as AgentGuiRpcClient["request"],
    subscribe: (listener) => { listeners.add(listener); return () => { listeners.delete(listener); }; },
  };
  return { client, request, storage, sessions, messages, tasks,
    complete: (taskId: string) => {
      tasks.push({ id: taskId, chat_session_id: session.id, status: "completed", created_at: time, started_at: time, completed_at: time } as AgentTask);
      if (pending.task_id === taskId) pending = {};
    },
    rejectSend: (error: unknown) => { rejectSend = error; },
    // Besides host events, tests drive local conversations through localSourceFromClient.
    emit: (event: AgentGuiHostEvent | { type: "local.events" | "local.reload"; sessionId: string; message?: unknown }) => { for (const listener of listeners) listener(event as AgentGuiHostEvent); },
  };
}

type Requests = { mock: { calls: unknown[][] } };
const enqueuedItem = (request: Requests) => (request.mock.calls.find(([method]) => method === "queue.enqueue")?.[1] as { item: Record<string, any> } | undefined)?.item;
const wasEnqueued = (request: Requests) => request.mock.calls.some(([method]) => method === "queue.enqueue");

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
  describe("attachments still uploading", () => {
    const uploading = async () => {
      const data = backend(); data.sessions.push(session);
      const original = data.request.getMockImplementation()!;
      data.request.mockImplementation(async (method, params) => {
        if (method === "workspace.load") return { ...await original(method, params), local: true, queueOwner: "desktop" };
        if (method === "queue.enqueue") return { revision: 1, paused: true, items: [params.item] };
        if (method === "attachment.upload") return { ...await original(method, params), uploading: params.background === true };
        if (method === "attachment.retry" || method === "attachment.forget" || method === "attachment.cancel") return undefined;
        return original(method, params);
      });
      const handle = createMulticaAgentGuiRuntime(data.client, { ...init, local: true, queueOwner: "desktop", sessions: [session] });
      handles.push(handle); await handle.initialize();
      await handle.runtime.getSession(init.workspaceId, session.id);
      const { content } = await handle.runtime.uploadPromptContent!({ workspaceId: init.workspaceId, content: [{ type: "image", mimeType: "image/png", data: "AAAA", name: "a.png" }] });
      const engine = handle.runtime.getSessionEngine(init.workspaceId);
      const enqueued = () => data.request.mock.calls.filter(([method]) => method === "queue.enqueue");
      return { data, handle, engine, content, enqueued };
    };

    it("sends the message once the transfer finishes", async () => {
      const { data, handle, engine, content, enqueued } = await uploading();
      expect(handle.activeUploadCount()).toBe(1);
      expect(engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "with-image", content: content as never }).accepted).toBe(true);
      await new Promise((resolve) => setTimeout(resolve, 20));
      expect(enqueued()).toHaveLength(0);
      data.emit({ type: "attachment.upload", attachmentId: "attachment-1", state: "uploaded" });
      await vi.waitFor(() => expect(enqueued()).toHaveLength(1));
      expect(enqueued()[0]![1].item.attachmentIds).toEqual(["attachment-1"]);
      expect(handle.activeUploadCount()).toBe(0);
      await vi.waitFor(() => expect(data.request).toHaveBeenCalledWith("attachment.forget", { attachmentId: "attachment-1" }));
    });

    it("keeps a failed message for an explicit retry, which transfers again", async () => {
      const { data, engine, content, enqueued } = await uploading();
      engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "with-image", content: content as never });
      await new Promise((resolve) => setTimeout(resolve, 20));
      data.emit({ type: "attachment.upload", attachmentId: "attachment-1", state: "failed", message: "offline" });
      await vi.waitFor(() => {
        const queue = engine.getSnapshot().promptQueue.recordsBySessionId[session.id];
        expect(queue?.failureMessage).toContain("Could not upload a.png: offline");
      });
      expect(enqueued()).toHaveLength(0);
      expect(engine.getSnapshot().promptQueue.recordsBySessionId[session.id]?.prompts.map((prompt) => prompt.clientSubmitId)).toEqual(["with-image"]);
    });

    it("stops the transfer when the draft drops the attachment", async () => {
      const { data, handle } = await uploading();
      const with_ = [{ type: "text", text: "" }, { type: "image", attachmentId: "attachment-1", mimeType: "image/png", name: "a.png" }];
      (handle.runtime as unknown as { promptAttachmentsRemoved(before: unknown, after: unknown): void }).promptAttachmentsRemoved(with_, [{ type: "text", text: "" }]);
      expect(data.request).toHaveBeenCalledWith("attachment.cancel", { attachmentId: "attachment-1" });
      expect(handle.activeUploadCount()).toBe(0);
    });
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
  it.each(["claude-code", "codex"])("loads and submits to an externally created %s session before workspace refresh completes", async (provider) => {
    const data = backend();
    const localInit = { ...init, local: true, agents: [{ ...init.agents[0]!, provider }] };
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
      expect((handle.localConversations as unknown as { opened: string[] }).opened).toContain(session.id);
      finishRefresh();
      await refresh;
      expect(handle.toServerSessionId(session.id)).toBe(session.id);
      expect(handle.runtime.getSnapshot(init.workspaceId).sessions.map((entry) => entry.agentSessionId)).toContain(session.id);
      const engine = handle.runtime.getSessionEngine(init.workspaceId);
      engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "external-first-send", content: [{ type: "text", text: "First message" }] });
      await vi.waitFor(() => expect(data.request.mock.calls.some(([method, params]) =>
        method === "queue.enqueue" && params.sessionId === session.id && params.item.content === "First message")).toBe(true));
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
    await vi.waitFor(() => expect(enqueuedItem(data.request)).toMatchObject({ permissionMode: "auto" }));
    await handle.runtime.getSession(init.workspaceId, "permission-draft");
    await expect(handle.setPermissionSelection(agent.id, "permission-draft", "full-access")).rejects.toMatchObject({ code: "run_in_progress" });
    const restored = createMulticaAgentGuiRuntime(data.client, localInit); handles.push(restored); await restored.initialize();
    expect(restored.getPermissionSelection(agent.id, session.id)).toBe("auto");
    expect(restored.getPermissionSelection("other-provider")).toBe("default");
  });
  it("preserves inherited permissions until explicitly changed, and retains them after save failure", async () => {
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
  });



  it("moves a queued message to Side through its own queue, and keeps it if that fails", async () => {
    const data = backend(); data.sessions.push(session);
    const original = data.request.getMockImplementation()!;
    let failSide = true;
    let items: unknown[] = [];
    data.request.mockImplementation(async (method, params) => {
      if (method === "queue.enqueue") { items = [...items, params.item]; return { revision: items.length, paused: false, items }; }
      if (method === "host.openSide") {
        if (failSide) throw new Error("Side save failed");
        return { ...session, id: "side-session" };
      }
      return original(method, params);
    });
    const { handle } = await setup(data); await handle.runtime.getSession(init.workspaceId, session.id);
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    engine.submitPrompt({ agentSessionId: session.id, clientSubmitId: "move-me", content: [{ type: "text", text: "Side request" }] });
    await vi.waitFor(() => expect(wasEnqueued(data.request)).toBe(true));
    await vi.waitFor(() => expect(engine.getSnapshot().promptQueue.recordsBySessionId[session.id]?.prompts.map((prompt) => prompt.id)).toEqual(["move-me"]));
    await expect(handle.moveQueuedToSide(session.id, "move-me")).rejects.toThrow("Side save failed");
    expect(engine.getSnapshot().promptQueue.recordsBySessionId[session.id]?.prompts.map((prompt) => prompt.id)).toEqual(["move-me"]);
    failSide = false;
    await expect(handle.moveQueuedToSide(session.id, "move-me")).resolves.toMatchObject({ id: "side-session" });
    expect(data.request).toHaveBeenCalledWith("host.openSide", { sessionId: session.id, requestId: `side:${session.id}:move-me`, queued: true, queuedRequestId: "move-me" });
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
    handle.runtime.composer.updateSettings({ agentSessionId: session.id, settings: { model: "runtime-catalog-model" } });
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
    expect(wasEnqueued(data.request)).toBe(false);
    release(); await initializing;
    await vi.waitFor(() => expect(enqueuedItem(data.request)).toMatchObject({ model: "catalog-model" }));
    await handle.runtime.getSession(init.workspaceId, "model-draft");
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
    await vi.waitFor(() => expect(wasEnqueued(data.request)).toBe(true));
    expect(enqueuedItem(data.request)).not.toHaveProperty("model");
  });

  it("sends selected reference identities to the parent and rejects a foreign workspace before submission", async () => {
    const { handle, request } = await setup();
    const content = ["workspace-issue", "agent-session", "agent-target"].map((providerId) => createRichTextMentionMarkdown({
      providerId, entityId: providerId === "agent-target" ? agent.id : `${providerId}-id`, label: "Reference", scope: { workspaceId: init.workspaceId },
    })).join(" ");
    const engine = handle.runtime.getSessionEngine(init.workspaceId);
    engine.activateSession({ mode: "new", agentSessionId: "reference-session", agentTargetId: agent.id,
      requestId: "reference-activation", clientSubmitId: "reference-submit", initialContent: [{ type: "text", text: content }] });
    await vi.waitFor(() => expect(wasEnqueued(request)).toBe(true));
    expect(enqueuedItem(request)).toMatchObject({ references: [
      { kind: "issue", id: "workspace-issue-id" }, { kind: "session", id: "agent-session-id" }, { kind: "agent", id: agent.id },
    ] });
    const other = await setup();
    other.handle.runtime.getSessionEngine(init.workspaceId).activateSession({ mode: "new", agentSessionId: "foreign-session", agentTargetId: agent.id,
      requestId: "foreign-activation", clientSubmitId: "foreign-submit", initialContent: [{ type: "text", text: createRichTextMentionMarkdown({
        providerId: "workspace-issue", entityId: "foreign-issue", label: "Foreign", scope: { workspaceId: "another-workspace" },
      }) }] });
    await vi.waitFor(() => expect(other.request.mock.calls.some(([method]) => method === "session.create")).toBe(true));
    await new Promise((resolve) => setTimeout(resolve, 30));
    expect(wasEnqueued(other.request)).toBe(false);
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
    await vi.waitFor(() => expect(wasEnqueued(data.request)).toBe(true));
    data.emit({ type: "invalidate", scope: "session", sessionId: session.id });
    await vi.waitFor(() => expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]).toMatchObject({ activeTurnId: "server-task-1" }));
    for (let index = 0; index < 3; index++) data.emit({ type: "invalidate", scope: "session", sessionId: session.id });
    await new Promise((resolve) => setTimeout(resolve, 100));
    const reads = data.request.mock.calls.filter(([method]) => method === "session.load").length;
    const settledUpdates = updates;
    await new Promise((resolve) => setTimeout(resolve, 100));
    expect(data.request.mock.calls.filter(([method]) => method === "session.load")).toHaveLength(reads);
    expect(updates).toBe(settledUpdates);
    expect(data.request.mock.calls.filter(([method]) => method === "queue.enqueue")).toHaveLength(1);
    expect(updates).toBeLessThan(50);
    release(); unsubscribe();
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

  it("reads the workspace again when the connection comes back, and shows a read that failed", async () => {
    const data = backend();
    const { handle } = await setup(data);
    await vi.waitFor(() => expect(handle.runtime.workspaceLoad.status).toBe("ready"));
    const loads = () => data.request.mock.calls.filter(([method]) => method === "workspace.load").length;
    const before = loads();
    data.emit({ type: "connection", connected: false });
    expect(loads()).toBe(before);
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => {
      if (method === "workspace.load") throw new Error("daemon unavailable");
      return original(method, params);
    });
    data.emit({ type: "connection", connected: true });
    await vi.waitFor(() => expect(handle.runtime.workspaceLoad).toMatchObject({ status: "failed", errorMessage: "daemon unavailable" }));
    expect(loads()).toBe(before + 1);
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
    await vi.waitFor(() => expect(wasEnqueued(request)).toBe(true));
    await handle.runtime.getSession(init.workspaceId, "gui-session");
    await vi.waitFor(() => expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]?.activeTurnId).toBe("server-task-1"));
    const snapshot = handle.runtime.getSnapshot(init.workspaceId);
    expect(snapshot.sessions[0]?.agentSessionId).toBe("gui-session");
    expect(snapshot.sessions[0]?.railSectionKey).toBe("conversations");
    expect(handle.runtime.transcript.messages("gui-session")[0]).toMatchObject({
      messageId: "server-message-1", turnId: "server-task-1",
    });
    expect(handle.toServerSessionId("gui-session")).toBe("server-session");
    expect(request.mock.calls.find(([method]) => method === "queue.enqueue")?.[1]).toMatchObject({ sessionId: "server-session", item: { content: "Inspect build" } });
    expect(storage.get("runtime-ledger-v1")).toContain("server-session");
    expect(handle.runtime.getSnapshot(init.workspaceId)).toBe(handle.runtime.getSnapshot(init.workspaceId));
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


  it("renames and pins through the server, then shows what it committed", async () => {
    const data = backend(); data.sessions.push(session);
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => {
      if (method === "session.rename") return { ...session, title: params.title };
      if (method === "session.pin") return { ...session, pinned: params.pinned };
      return original(method, params);
    });
    const { handle } = await setup(data);
    await handle.runtime.getSession(init.workspaceId, session.id);
    await expect(handle.runtime.mutations.rename({ agentSessionId: session.id, title: " Renamed " })).resolves.toMatchObject({ title: "Renamed" });
    expect(data.request).toHaveBeenCalledWith("session.rename", { sessionId: session.id, title: "Renamed" }, expect.anything());
    expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]?.title).toBe("Renamed");
    await handle.runtime.mutations.setPinned({ agentSessionId: session.id, pinned: true });
    expect(data.request).toHaveBeenCalledWith("session.pin", { sessionId: session.id, pinned: true }, expect.anything());
    expect(handle.runtime.getSnapshot(init.workspaceId).sessions[0]?.pinnedAtUnixMs).not.toBeNull();
  });

  it("forks through a turn and binds the child to the id it was asked to appear under", async () => {
    const forkable = { ...init, agents: [{ ...init.agents[0]!, canFork: true }] };
    const child = { ...session, id: "server-child" };
    const data = backend(); data.sessions.push(session);
    data.messages.push({ id: "message-1", chat_session_id: session.id, role: "user", content: "hi", task_id: "turn-1", created_at: time } as ChatMessage);
    data.tasks.push({ id: "turn-1", chat_session_id: session.id, status: "completed", created_at: time, started_at: time, completed_at: time } as AgentTask);
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => {
      if (method === "workspace.load") return { ...await original(method, params), agents: forkable.agents };
      if (method === "session.fork") return child;
      if (method === "session.load" && params.sessionId === child.id) return { ...await original(method, params), session: child, messages: [], tasks: [],
        forkedFrom: { sessionId: session.id, turnId: "turn-1", targetTurnId: "child-turn", operationId: child.id, createdAt: time } };
      return original(method, params);
    });
    const handle = createMulticaAgentGuiRuntime(data.client, forkable);
    handles.push(handle);
    await handle.initialize();
    await handle.runtime.getSession(init.workspaceId, session.id);
    const fork = await handle.runtime.mutations.forkThroughTurn({ sourceAgentSessionId: session.id, turnId: "turn-1" });
    expect(handle.toServerSessionId(fork.targetAgentSessionId)).toBe(child.id);
    expect(data.request).toHaveBeenCalledWith("session.fork", expect.objectContaining({ sessionId: session.id, turnId: "turn-1" }), expect.anything());
    expect(handle.runtime.mutations.pendingForkTurnIds(session.id)).toEqual([]);
  });

  it("rejects recoverable deletion and unsupported capabilities without calling a destructive API", async () => {
    const { handle, request } = await setup();
    await expect(handle.runtime.deleteSession({ workspaceId: init.workspaceId, agentSessionId: "anything" })).rejects.toMatchObject({ code: "unsupported" });
    expect(request.mock.calls.some(([method]) => method === "session.delete")).toBe(false);
    const options = await handle.runtime.getComposerOptions({ workspaceId: init.workspaceId, agentTargetId: agent.id });
    expect(options.capabilities).toMatchObject({ goalPause: false, planMode: false, modelSwitch: true, activeTurnGuidance: false });
    expect(handle.runtime.composer.options(agent.id)).toEqual(options);
    expect(() => handle.runtime.getSessionEngine("another-workspace")).toThrow("workspace changed");
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
    await vi.waitFor(() => expect(wasEnqueued(request)).toBe(true));
    expect(enqueuedItem(request)).toMatchObject({ attachmentIds: ["attachment-1"] });
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
    const opened = (handle.localConversations as unknown as { opened: string[] }).opened;
    expect(opened).not.toContain("gui-session");
    completeCreation();
    await vi.waitFor(() => expect(opened).toContain("server-session"));
    await vi.waitFor(() => expect(wasEnqueued(data.request)).toBe(true));
    expect(data.request.mock.calls.filter(([method]) => method === "session.load").every(([, params]) => params.sessionId === "server-session")).toBe(true);
    expect(opened).not.toContain("gui-session");
    release();
  });

  it("binds source file mention locators only to exactly registered attachment URLs", async () => {
    const { handle, request } = await setup();
    handle.registerAttachments([{ id: "real-file", filename: "report.txt", markdown_url: "/api/attachments/real-file/download", url: "/api/attachments/real-file/download" } as Attachment]);
    const realHref = `mention://file/${encodeURIComponent("/api/attachments/real-file/download")}`;
    const lookalikeHref = `mention://file/${encodeURIComponent("/other/report.txt")}`;
    handle.runtime.getSessionEngine(init.workspaceId).activateSession({ mode: "new", agentSessionId: "gui-session", agentTargetId: agent.id,
      requestId: "activation-1", clientSubmitId: "submit-1", initialContent: [{ type: "text", text: `Read [@report.txt](${realHref}) and [@report.txt](${lookalikeHref})` }] });
    await vi.waitFor(() => expect(wasEnqueued(request)).toBe(true));
    expect(enqueuedItem(request)?.attachmentIds).toEqual(["real-file"]);
  });

  it("binds ordinary source Markdown files to their registered real attachment IDs", async () => {
    const { handle, request } = await setup();
    handle.registerAttachments([
      { id: "real-file", filename: "report.txt", markdown_url: "http://localhost:6099/api/attachments/real-file/download", url: "http://localhost:6099/api/attachments/real-file/download" } as Attachment,
      { id: "other-file", filename: "report.txt", markdown_url: "http://localhost:6099/api/attachments/other-file/download", url: "http://localhost:6099/api/attachments/other-file/download" } as Attachment,
    ]);
    handle.runtime.getSessionEngine(init.workspaceId).activateSession({ mode: "new", agentSessionId: "gui-session", agentTargetId: agent.id,
      requestId: "activation-1", clientSubmitId: "submit-1", initialContent: [{ type: "text", text: "Read [@report.txt](http://localhost:6099/api/attachments/real-file/download) and [@report.txt](https://example.test/report.txt)" }] });
    await vi.waitFor(() => expect(wasEnqueued(request)).toBe(true));
    expect(enqueuedItem(request)?.attachmentIds).toEqual(["real-file"]);
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
    await vi.waitFor(() => expect(enqueuedItem(data.request)).toMatchObject({ reasoningEffort: null }));
    await handle.runtime.getSession(init.workspaceId, "default-draft");
    await expect(handle.setReasoningSelection(agent.id, "default-draft", "low")).rejects.toMatchObject({ code: "run_in_progress" });
  });
  it("keeps draft selection through activation, delivery, and restart", async () => {
    const data = backend();
    const original = data.request.getMockImplementation()!;
    data.request.mockImplementation(async (method, params) => {
      if (method === "models.list") return { supported: true, models: [{ id: "test-model", label: "Test", thinking: { supported_levels: [{ value: "low", label: "Low" }, { value: "high", label: "High" }] } }] };
      return original(method, params);
    });
    const handle = createMulticaAgentGuiRuntime(data.client, { ...init, local: true }); handles.push(handle);
    await handle.initialize();
    await handle.setModelSelection(agent.id, null, "test-model");
    await handle.setReasoningSelection(agent.id, null, "high");
    const options = await handle.runtime.getComposerOptions({ workspaceId: init.workspaceId, agentTargetId: agent.id });
    expect(options).toMatchObject({ reasoningConfigurable: true, reasoningEfforts: [{ value: "low", label: "Low" }, { value: "high", label: "High" }], effectiveSettings: { reasoningEffort: "high" } });
    handle.runtime.getSessionEngine(init.workspaceId).activateSession({ mode: "new", agentSessionId: "thinking-draft", agentTargetId: agent.id,
      requestId: "thinking-activation", clientSubmitId: "thinking-submit", initialContent: [{ type: "text", text: "Think" }] });
    await vi.waitFor(() => expect(enqueuedItem(data.request)).toMatchObject({ reasoningEffort: "high", model: "test-model" }));
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
    const messages = () => handle.runtime.transcript.messages(session.id);
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
