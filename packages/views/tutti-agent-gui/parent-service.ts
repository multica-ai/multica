import { QueryObserver, type QueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@multica/core/api";
import { deriveAgentPresenceDetail, isAgentRuntimeBound } from "@multica/core/agents";
import { canAssignAgentToIssue } from "@multica/core/permissions";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import {
  chatKeys,
  chatSessionsOptions,
  pendingChatTaskOptions,
  pendingChatTasksOptions,
  taskMessagesOptions,
} from "@multica/core/chat/queries";
import { defaultStorage } from "@multica/core/platform";
import { useChatStore } from "@multica/core/chat";
import { runtimeModelsOptions, refreshRuntimeModels } from "@multica/core/runtimes/models";
import { MAX_FILE_SIZE } from "@multica/core/constants/upload";
import type {
  Agent,
  AgentTask,
  ChatMessage,
  ChatPendingTask,
  ChatSession,
  StorageAdapter,
} from "@multica/core/types";
import type {
  AgentGuiAgentDescriptor,
  AgentGuiHostEvent,
  AgentGuiRpcMethod,
  AgentGuiRpcMethods,
  AgentGuiWorkspaceState,
  LocalConversationSource,
} from "@multica/tutti-agent-gui-host/protocol";
import { redactSecrets } from "../common/task-transcript/redact";
import { issueKeys } from "@multica/core/issues/queries";
import { buildIssueStatusCatalog, issueStatusListOptions } from "@multica/core/issue-statuses";
import { chatAuthoredPrompt, CHAT_CONTEXT_SNAPSHOT_START as CONTEXT_SNAPSHOT_START, CHAT_CONTEXT_SNAPSHOT_END as CONTEXT_SNAPSHOT_END } from "@multica/core/chat/context-snapshot";
import { agentGuiAvatarUrl } from "./agent-avatar";
import { isTaskCreationReference } from "@multica/core/local-conversation/protocol";
import { resolveResourceReference } from "@multica/tutti-agent-gui-host/resource-reference";

export class AgentGuiBridgeError extends Error {
  constructor(message: string, readonly code: string, readonly status?: number) {
    super(message);
  }
}

export interface ParentServiceOptions {
  queryClient: QueryClient;
  workspaceId: string;
  workspaceSlug: string;
  userId: string;
  isCurrent: () => boolean;
  resolveAvatar: (url: string) => string;
  onSelectSession: (session: ChatSession | null, agent?: Agent) => void;
  onOpenTask: (issueId: string) => void;
  getAppOrigin?: () => string | null;
  onOpenInternalResource?: (path: string, background: boolean) => void;
  onShowMessage: (message: string, tone?: "success" | "info" | "warning" | "error") => void;
  onPromptApplied?: (sequence: number, restoreRequestId?: string) => void;
  storage?: StorageAdapter;
}

function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new AgentGuiBridgeError("Invalid GUI request", "invalid_request", 400);
  }
  return value as Record<string, unknown>;
}

function stringField(value: Record<string, unknown>, key: string): string {
  const result = value[key];
  if (typeof result !== "string" || !result.trim()) {
    throw new AgentGuiBridgeError(`Missing ${key}`, "invalid_request", 400);
  }
  return result;
}

function boolField(value: Record<string, unknown>, key: string): boolean {
  if (typeof value[key] !== "boolean") {
    throw new AgentGuiBridgeError(`Invalid ${key}`, "invalid_request", 400);
  }
  return value[key];
}

/** Auth and server connections stay in the Multica document. */
export interface AgentGuiParentService {
  /** Present when the host keeps local conversations. */
  localConversations?: LocalConversationSource;
  load(): Promise<AgentGuiWorkspaceState>;
  request<M extends AgentGuiRpcMethod>(method: M, params: AgentGuiRpcMethods[M]["params"]): Promise<AgentGuiRpcMethods[M]["result"]>;
  publish(event: AgentGuiHostEvent): void;
  subscribe(listener: (event: AgentGuiHostEvent) => void): () => void;
  reconnect(): void;
  dispose(): void;
}

export function createAgentGuiParentService(options: ParentServiceOptions): AgentGuiParentService {
  const { queryClient: qc, workspaceId, workspaceSlug, userId } = options;
  const storage = options.storage ?? defaultStorage;
  const listeners = new Set<(event: AgentGuiHostEvent) => void>();
  const sessionObservers = new Map<string, () => void>();
  const taskObservers = new Map<string, () => void>();
  const tasksBySession = new Map<string, Set<string>>();
  const knownTasksBySession = new Map<string, Set<string>>();
  const attachmentNames = new Map<string, string>();
  const cleanup: Array<() => void> = [];
  let disposed = false;
  let sessionTaskListingSupported = true;
  let workspaceNotificationPending = false;
  const sessionNotificationPending = new Set<string>();
  const taskNotificationPending = new Set<string>();

  function assertCurrent() {
    if (disposed || !options.isCurrent()) {
      throw new AgentGuiBridgeError("This workspace is no longer active", "stale_workspace", 409);
    }
  }

  function publish(event: AgentGuiHostEvent) {
    if (disposed || !options.isCurrent()) return;
    listeners.forEach((listener) => listener(event));
  }

  function notify(scope: "workspace" | "session", sessionId?: string) {
    if (scope === "workspace") {
      if (workspaceNotificationPending) return;
      workspaceNotificationPending = true;
      queueMicrotask(() => {
        workspaceNotificationPending = false;
        publish({ type: "invalidate", scope: "workspace" });
      });
    } else if (sessionId && !sessionNotificationPending.has(sessionId)) {
      sessionNotificationPending.add(sessionId);
      queueMicrotask(() => {
        sessionNotificationPending.delete(sessionId);
        publish({ type: "invalidate", scope: "session", sessionId });
      });
    }
  }

  function notifyTask(sessionId: string, taskId: string) {
    const key = `${sessionId}:${taskId}`;
    if (taskNotificationPending.has(key)) return;
    taskNotificationPending.add(key);
    queueMicrotask(() => {
      taskNotificationPending.delete(key);
      publish({ type: "invalidate", scope: "task", sessionId, taskId });
    });
  }

  async function session(sessionId: string) {
    assertCurrent();
    const result = await api.getChatSession(sessionId);
    assertCurrent();
    if (result.workspace_id !== workspaceId || result.id !== sessionId) {
      throw new AgentGuiBridgeError("Conversation does not belong to this workspace", "invalid_session", 403);
    }
    return result;
  }

  async function workspaceLoad(): Promise<AgentGuiWorkspaceState> {
    assertCurrent();
    const [agents, sessions, members, runtimes, pending] = await Promise.all([
      qc.fetchQuery({ ...agentListOptions(workspaceId), staleTime: 0 }),
      qc.fetchQuery({ ...chatSessionsOptions(workspaceId), staleTime: 0 }),
      qc.fetchQuery(memberListOptions(workspaceId)),
      api.listRuntimes({ workspace_id: workspaceId }),
      qc.fetchQuery({ ...pendingChatTasksOptions(workspaceId), staleTime: 0 }),
    ]);
    assertCurrent();
    const role = members.find((member) => member.user_id === userId)?.role;
    const descriptors: AgentGuiAgentDescriptor[] = agents.map((agent) => {
      const runtime = runtimes.find((entry) => entry.id === agent.runtime_id);
      const provider = runtime?.provider || (
        typeof agent.runtime_config.provider === "string"
          ? agent.runtime_config.provider : "unknown"
      );
      const runtimeBound = isAgentRuntimeBound(agent);
      const canInvoke = !agent.archived_at && canAssignAgentToIssue(agent, {
        userId,
        role: role === "owner" || role === "admin" || role === "member" ? role : null,
      }).allowed;
      const { availability } = deriveAgentPresenceDetail({ agent, runtime: runtime ?? null, tasks: [], now: Date.now() });
      return {
        agent,
        provider,
        iconUrl: agentGuiAvatarUrl(agent.avatar_url, options.resolveAvatar),
        canInvoke,
        runtimeBound,
        runtimeOnline: availability === "online" || availability === "unstable",
      };
    });
    const sessionIds = new Set(sessions.map((chat) => chat.id));
    return { agents: descriptors, sessions, pendingTasks: pending.tasks.filter((task) => sessionIds.has(task.chat_session_id)) };
  }

  async function allMessages(sessionId: string): Promise<ChatMessage[]> {
    const messages: ChatMessage[] = [];
    const cursors = new Set<string>();
    let before: { created_at: string; id: string } | null = null;
    do {
      assertCurrent();
      const page = await api.listChatMessagesPage(sessionId, { before, limit: 100 });
      assertCurrent();
      messages.push(...page.messages);
      before = page.has_more ? page.next_cursor ?? null : null;
      if (before) {
        const key = `${before.created_at}:${before.id}`;
        if (cursors.has(key)) throw new AgentGuiBridgeError("Message pagination did not advance", "invalid_cursor");
        cursors.add(key);
      }
    } while (before);
    return [...new Map(messages.map((message) => [message.id, message])).values()]
      .sort((a, b) => a.created_at.localeCompare(b.created_at) || a.id.localeCompare(b.id));
  }

  async function sessionTasks(chat: ChatSession, messages: ChatMessage[], pending: ChatPendingTask) {
    const wanted = new Set(messages.flatMap((message) => message.task_id ? [message.task_id] : []));
    if (pending.task_id) wanted.add(pending.task_id);
    pending.queued_tasks?.forEach((task) => wanted.add(task.task_id));
    const tasks = new Map<string, AgentTask>();
    const cursors = new Set<string>();
    let before: string | undefined;
    do {
      assertCurrent();
      let page;
      if (sessionTaskListingSupported) {
        try {
          page = await api.listChatSessionTasksPage(chat.id, { limit: 200, before });
        } catch (error) {
          if (!(error instanceof ApiError) || error.status !== 404) throw error;
          sessionTaskListingSupported = false;
        }
      }
      // Official servers may not have the session task projection yet.
      // Reuse their agent-scoped projection, then enforce the session boundary.
      if (!page) {
        assertCurrent();
        if (!chat.agent_id) return [];
        page = await api.listAgentTasksPage(chat.agent_id, { limit: 200, before });
      }
      assertCurrent();
      page.tasks.forEach((task) => {
        if (task.chat_session_id === chat.id) {
          tasks.set(task.id, task);
          wanted.delete(task.id);
        }
      });
      before = page.nextCursor ?? undefined;
      if (before && cursors.has(before)) throw new AgentGuiBridgeError("Run pagination did not advance", "invalid_cursor");
      if (before) cursors.add(before);
    } while (before && wanted.size > 0);
    return [...tasks.values()];
  }

  function holdTasks(sessionId: string, pending: ChatPendingTask) {
    const ids = new Set([
      ...(pending.task_id ? [pending.task_id] : []),
      ...(pending.queued_tasks?.map((task) => task.task_id) ?? []),
    ]);
    tasksBySession.set(sessionId, ids);
    const known = knownTasksBySession.get(sessionId) ?? new Set<string>();
    ids.forEach((id) => known.add(id));
    knownTasksBySession.set(sessionId, known);
    const needed = new Set([...tasksBySession.values()].flatMap((values) => [...values]));
    for (const [taskId, release] of taskObservers) {
      if (!needed.has(taskId)) { release(); taskObservers.delete(taskId); }
    }
    for (const taskId of needed) {
      if (taskObservers.has(taskId)) continue;
      const observer = new QueryObserver(qc, taskMessagesOptions(taskId));
      const unsubscribe = observer.subscribe(() => {});
      taskObservers.set(taskId, () => { unsubscribe(); observer.destroy(); });
      // Reopening a live timeline must repair events missed while it was closed.
      void observer.refetch().catch(() => notify("session", sessionId));
    }
  }

  function watch(sessionIds: string[]) {
    for (const [id, release] of sessionObservers) {
      if (!sessionIds.includes(id)) {
        release(); sessionObservers.delete(id); tasksBySession.delete(id);
      }
    }
    for (const id of sessionIds) {
      if (sessionObservers.has(id)) continue;
      const observer = new QueryObserver(qc, pendingChatTaskOptions(id));
      const unsubscribe = observer.subscribe((value) => {
        if (!disposed && value.data) holdTasks(id, value.data);
      });
      sessionObservers.set(id, () => { unsubscribe(); observer.destroy(); });
      void observer.refetch().catch(() => notify("session", id));
    }
    holdTasks("", {});
  }

  function upsertSession(chat: ChatSession) {
    assertCurrent();
    qc.setQueryData<ChatSession[]>(chatKeys.sessions(workspaceId), (current) => {
      const rows = current ?? [];
      return rows.some((item) => item.id === chat.id)
        ? rows.map((item) => item.id === chat.id ? { ...item, ...chat } : item)
        : [chat, ...rows];
    });
  }

  async function request(method: AgentGuiRpcMethod, rawParams: unknown): Promise<unknown> {
    assertCurrent();
    const params = record(rawParams);
    let result: unknown;
    switch (method) {
      case "workspace.load": result = await workspaceLoad(); break;
      case "models.list": {
        const agentId = stringField(params, "agentId");
        if (params.force !== undefined && typeof params.force !== "boolean") throw new AgentGuiBridgeError("Invalid model refresh flag", "invalid_request", 400);
        const state = await workspaceLoad();
        const target = state.agents.find((entry) => entry.agent.id === agentId);
        if (!target?.canInvoke || !target.runtimeBound || !target.agent.runtime_id) throw new AgentGuiBridgeError("This agent has no available model catalog", "agent_unavailable", 403);
        result = params.force === true
          ? await refreshRuntimeModels(qc, target.agent.runtime_id)
          : await qc.fetchQuery(runtimeModelsOptions(target.agent.runtime_id));
        assertCurrent();
        break;
      }
      case "context.tasks": {
        if (typeof params.query !== "string" || params.query.length > 200 || typeof params.limit !== "number" || !Number.isInteger(params.limit) || params.limit < 1 || params.limit > 50) {
          throw new AgentGuiBridgeError("Invalid task search", "invalid_request", 400);
        }
        const query = params.query.trim();
        const limit = params.limit;
        const [page, statuses] = await Promise.all([qc.fetchQuery({
          queryKey: [...issueKeys.all(workspaceId), "gui-context", query, limit],
          queryFn: () => api.listIssues({ workspace_id: workspaceId, q: query, limit, sort_by: "updated_at", sort_direction: "desc" }),
          staleTime: 0,
        }), qc.fetchQuery({ ...issueStatusListOptions(workspaceId), retry: false }).catch(() => undefined)]);
        assertCurrent();
        const catalog = buildIssueStatusCatalog(statuses?.statuses);
        result = page.issues.filter((issue) => issue.workspace_id === workspaceId).map((issue) => ({
          id: issue.id, label: `${issue.identifier} ${issue.title}`, description: issue.description ? redactSecrets(issue.description).slice(0, 300) : undefined, status: issue.status,
          statusLabel: catalog.entryOf(issue.status)?.is_system === false ? catalog.labelOf(issue.status) : undefined,
          statusCategory: catalog.entryOf(issue.status)?.category ?? issue.status_category,
        }));
        break;
      }
      case "session.load": {
        const id = stringField(params, "sessionId");
        const chat = await session(id);
        const [messages, pending] = await Promise.all([
          allMessages(id),
          qc.fetchQuery({ ...pendingChatTaskOptions(id), staleTime: 0 }),
        ]);
        const tasks = await sessionTasks(chat, messages, pending);
        const known = new Set(tasks.map((task) => task.id));
        messages.forEach((message) => { if (message.task_id) known.add(message.task_id); });
        if (pending.task_id) known.add(pending.task_id);
        knownTasksBySession.set(id, known);
        messages.flatMap((message) => message.attachments ?? [])
          .forEach((attachment) => attachmentNames.set(attachment.id, attachment.filename));
        result = { session: chat, messages, pending, tasks };
        break;
      }
      case "session.create": {
        const agentId = stringField(params, "agentId");
        const state = await workspaceLoad();
        const target = state.agents.find((entry) => entry.agent.id === agentId);
        if (!target?.canInvoke || !target.runtimeBound) throw new AgentGuiBridgeError("This agent cannot start a conversation", "agent_unavailable", 403);
        const created = await api.createChatSession({
          agent_id: agentId,
          ...(typeof params.title === "string" ? { title: params.title } : {}),
          ...(typeof params.projectId === "string" ? { project_id: params.projectId } : {}),
        }, workspaceSlug);
        upsertSession(created);
        result = created;
        break;
      }
      case "session.send": {
        const id = stringField(params, "sessionId");
        const chat = await session(id);
        if (chat.status === "archived") throw new AgentGuiBridgeError("Archived conversations are read-only", "read_only", 409);
        if (typeof params.content !== "string") throw new AgentGuiBridgeError("Invalid message", "invalid_request", 400);
        const ids = params.attachmentIds;
        if (ids !== undefined && (!Array.isArray(ids) || ids.some((id) => typeof id !== "string"))) throw new AgentGuiBridgeError("Invalid attachments", "invalid_request", 400);
        const context = await referenceContext(params.references);
        assertCurrent();
        const content = context ? chatAuthoredPrompt(params.content) : params.content;
        const model = params.model;
        // Model identifiers must reject ASCII controls at the bridge boundary.
        // eslint-disable-next-line no-control-regex
        if (model !== undefined && (typeof model !== "string" || !model.trim() || model.trim() !== model || new TextEncoder().encode(model).length > 256 || /[\x00-\x1f\x7f]/.test(model))) throw new AgentGuiBridgeError("Invalid model ID", "invalid_request", 400);
        const targets = Array.isArray(params.references) ? [...new Set(params.references.filter((value) => record(value).kind === "agent").map((value) => stringField(record(value), "id")))] : [];
        if (targets.length) {
          const state = await workspaceLoad();
          for (const targetId of targets) {
            const target = state.agents.find((entry) => entry.agent.id === targetId);
            if (!target?.canInvoke || !target.runtimeBound || target.agent.archived_at) throw new AgentGuiBridgeError("One or more mentioned agents cannot be invoked. Check permissions and runtime bindings.", "agent_unavailable", 403);
          }
          try {
            result = await api.sendChatAgentMessage(id, content + context, targets, ids as string[] | undefined);
          } catch (error) {
            if (!(error instanceof ApiError) || error.status !== 404) throw error;
            throw new AgentGuiBridgeError("This server does not support invoking mentioned agents. Open a conversation with the agent directly, or connect to a server with multi-agent support.", "unsupported_agent_invocation", error.status);
          }
        } else result = model === undefined
          ? await api.sendChatMessage(id, content + context, ids as string[] | undefined)
          : await api.sendChatMessage(id, content + context, ids as string[] | undefined, { model: model as string });
        assertCurrent();
        void qc.invalidateQueries({ queryKey: chatKeys.pendingTask(id) });
        void qc.invalidateQueries({ queryKey: chatKeys.messages(id) });
        void qc.invalidateQueries({ queryKey: chatKeys.messagesPage(id) });
        void qc.invalidateQueries({ queryKey: chatKeys.sessions(workspaceId) });
        notify("session", id);
        break;
      }
      case "session.rename":
      case "session.pin": {
        const id = stringField(params, "sessionId");
        await session(id);
        const updated = method === "session.rename"
          ? await api.updateChatSession(id, { title: stringField(params, "title") })
          : await api.setChatSessionPinned(id, boolField(params, "pinned"));
        upsertSession(updated);
        result = updated;
        break;
      }
      case "session.delete":
        throw new AgentGuiBridgeError("Recoverable conversation deletion is not supported by this host", "unsupported");
      case "session.markRead": {
        const id = stringField(params, "sessionId");
        await session(id);
        const cached = qc.getQueryData<ChatSession[]>(chatKeys.sessions(workspaceId))?.find((row) => row.id === id);
        if (cached?.has_unread === false && !cached.unread_count) break;
        await api.markChatSessionRead(id);
        assertCurrent();
        qc.setQueryData<ChatSession[]>(chatKeys.sessions(workspaceId), (rows) => rows?.map((row) => row.id === id ? { ...row, has_unread: false, unread_count: 0 } : row));
        break;
      }
      case "session.cancel": {
        const id = stringField(params, "sessionId");
        const taskId = stringField(params, "taskId");
        await session(id);
        const detail = await qc.fetchQuery({ ...pendingChatTaskOptions(id), staleTime: 0 });
        assertCurrent();
        if (detail.task_id !== taskId && !detail.queued_tasks?.some((task) => task.task_id === taskId)) throw new AgentGuiBridgeError("Run does not belong to the selected conversation", "invalid_task", 409);
        const cancelled = await api.cancelTaskById(taskId);
        assertCurrent();
        if (cancelled.cancelled_chat_message?.restore_to_input) {
          const restored = cancelled.cancelled_chat_message;
          useChatStore.getState().enqueuePendingSendRestore({
            id: restored.message_id, sessionId: id, content: chatAuthoredPrompt(restored.content),
            attachments: restored.attachments,
          });
        }
        await qc.invalidateQueries({ queryKey: chatKeys.pendingTask(id) });
        await qc.invalidateQueries({ queryKey: chatKeys.messages(id) });
        await qc.invalidateQueries({ queryKey: chatKeys.messagesPage(id) });
        await qc.invalidateQueries({ queryKey: chatKeys.taskMessages(taskId) });
        notify("session", id);
        result = cancelled;
        break;
      }
      case "session.watch": {
        if (!Array.isArray(params.sessionIds) || params.sessionIds.some((id) => typeof id !== "string") || params.sessionIds.length > 20) throw new AgentGuiBridgeError("Invalid conversation subscriptions", "invalid_request", 400);
        const ids = params.sessionIds as string[];
        await Promise.all(ids.map(session));
        watch(ids);
        break;
      }
      case "task.messages": {
        const taskId = stringField(params, "taskId");
        if (![...knownTasksBySession.values()].some((ids) => ids.has(taskId))) throw new AgentGuiBridgeError("Run was not loaded from a conversation", "invalid_task", 403);
        const messages = await qc.fetchQuery(taskMessagesOptions(taskId));
        result = messages.map((message) => ({ ...message,
          content: message.content ? redactSecrets(message.content) : message.content,
          output: message.output ? redactSecrets(message.output) : message.output,
        }));
        break;
      }
      case "attachment.upload": {
        if (!(params.file instanceof File)) throw new AgentGuiBridgeError("Invalid upload file", "invalid_request", 400);
        if (params.file.size > MAX_FILE_SIZE) throw new AgentGuiBridgeError("This file exceeds the upload limit", "file_too_large", 413);
        const sessionId = typeof params.sessionId === "string" ? params.sessionId : undefined;
        if (sessionId) await session(sessionId);
        const attachment = await api.uploadFile(params.file, sessionId ? { chatSessionId: sessionId } : undefined);
        attachmentNames.set(attachment.id, attachment.filename);
        result = attachment;
        break;
      }
      case "attachment.read": {
        const id = stringField(params, "attachmentId");
        const blob = await api.getAttachmentBlob(id);
        result = { data: await blobBase64(blob), mimeType: blob.type, name: attachmentNames.get(id) ?? id };
        break;
      }
      case "storage.read":
      case "storage.write": {
        const key = `multica:tutti-gui:${userId}:${workspaceId}:${encodeURIComponent(stringField(params, "key"))}`;
        if (method === "storage.read") result = storage.getItem(key);
        else {
          if (typeof params.value !== "string") throw new AgentGuiBridgeError("Invalid stored value", "invalid_request", 400);
          storage.setItem(key, params.value);
        }
        break;
      }
      case "host.openTask": options.onOpenTask(stringField(params, "issueId")); break;
      case "host.openInternalResource": {
        const resource = resolveResourceReference(stringField(params, "href"), { workspaceId, workspaceSlug: options.workspaceSlug, appOrigin: options.getAppOrigin?.() });
        if (!resource || params.background !== undefined && typeof params.background !== "boolean") throw new AgentGuiBridgeError("Invalid internal resource link", "invalid_request", 400);
        if (!options.onOpenInternalResource) throw new AgentGuiBridgeError("Internal navigation is unavailable", "navigation_unavailable", 409);
        options.onOpenInternalResource(resource.path, params.background === true); break;
      }
      case "host.selectSession": {
        if (params.sessionId === null) {
          const state = await workspaceLoad();
          const target = state.agents.find((entry) => entry.agent.id === params.agentId);
          options.onSelectSession(null, target?.agent);
        } else options.onSelectSession(await session(stringField(params, "sessionId")));
        break;
      }
      case "host.showMessage":
        options.onShowMessage(stringField(params, "message"), params.tone === "success" || params.tone === "warning" || params.tone === "error" ? params.tone : "info");
        break;
      case "host.promptApplied":
        if (typeof params.sequence === "number") options.onPromptApplied?.(params.sequence, typeof params.restoreRequestId === "string" ? params.restoreRequestId : undefined);
        break;
      default: throw new AgentGuiBridgeError("Unsupported GUI command", "unsupported");
    }
    assertCurrent();
    return result;
  }

  async function referenceContext(raw: unknown): Promise<string> {
    if (raw === undefined) return "";
    if (!Array.isArray(raw) || raw.length > 5) throw new AgentGuiBridgeError("Reference up to five items per message", "invalid_reference", 400);
    const references = raw.map((value) => {
      const item = record(value);
      if (isTaskCreationReference(item)) throw new AgentGuiBridgeError("Use New task in a local Codex or Claude Code conversation.", "local_provider_required", 409);
      if (item.kind !== "issue" && item.kind !== "session" && item.kind !== "agent") throw new AgentGuiBridgeError("Invalid reference", "invalid_reference", 400);
      return { kind: item.kind, id: stringField(item, "id") };
    });
    const sections: string[] = [];
    const seen = new Set<string>();
    for (const reference of references) {
      const key = `${reference.kind}:${reference.id}`;
      if (seen.has(key)) continue;
      seen.add(key);
      let body: string;
      if (reference.kind === "issue") {
        const issue = await api.getIssue(reference.id);
        if (issue.workspace_id !== workspaceId) throw new AgentGuiBridgeError("Task does not belong to this workspace", "invalid_reference", 403);
        body = `[${issue.identifier}](mention://issue/${issue.id}) ${issue.title}\n${issue.description ?? ""}`;
      } else if (reference.kind === "session") {
        const chat = await session(reference.id);
        const page = await api.listChatMessagesPage(chat.id, { limit: 12 });
        const messages = page.messages.filter((message) => message.message_kind !== "onboarding_kickoff");
        body = `Conversation: ${chat.title}\n${messages.slice(-12).map((message) => `${message.role}: ${message.content}`).join("\n\n")}`;
        if (page.has_more) body += "\n[Earlier messages omitted]";
      } else {
        const state = await workspaceLoad();
        const target = state.agents.find((entry) => entry.agent.id === reference.id);
        if (!target) throw new AgentGuiBridgeError("Agent does not belong to this workspace", "invalid_reference", 403);
        if (!target.canInvoke || !target.runtimeBound || target.agent.archived_at) throw new AgentGuiBridgeError("Agent cannot be invoked", "agent_unavailable", 403);
        continue;
      }
      assertCurrent();
      const redacted = redactSecrets(body.replaceAll(CONTEXT_SNAPSHOT_START, "\n\n").replaceAll(CONTEXT_SNAPSHOT_END, ""));
      sections.push(redacted.length > 6000 ? `${redacted.slice(0, 6000)}\n[Reference excerpt truncated]` : redacted);
    }
    return sections.length ? `${CONTEXT_SNAPSHOT_START}Referenced context (snapshot):\n\n${sections.join("\n\n---\n\n")}${CONTEXT_SNAPSHOT_END}` : "";
  }

  cleanup.push(qc.getQueryCache().subscribe((event) => {
    if (event.type !== "updated") return;
    const key = event.query.queryKey;
    // A reopened task's HTTP backfill must reach the child too. Its next read
    // uses the fresh cache, so this does not create another network fetch.
    if (event.action.type !== "invalidate" && !(event.action.type === "success" && (event.action.manual || key[0] === "task-messages"))) return;
    if ((key[0] === "chat" && key[1] === workspaceId) || (key[0] === "workspaces" && key[1] === workspaceId) || (key[0] === "runtimes" && key[1] === workspaceId)) notify("workspace");
    for (const id of sessionObservers.keys()) {
      if (key[0] === "chat" && key[2] === id) notify("session", id);
      else if (key[0] === "task-messages" && tasksBySession.get(id)?.has(String(key[1]))) notifyTask(id, String(key[1]));
    }
  }));

  const sessionListObserver = new QueryObserver(qc, chatSessionsOptions(workspaceId));
  const agentListObserver = new QueryObserver(qc, agentListOptions(workspaceId));
  const pendingListObserver = new QueryObserver(qc, pendingChatTasksOptions(workspaceId));
  cleanup.push(sessionListObserver.subscribe(() => {}), () => sessionListObserver.destroy());
  cleanup.push(agentListObserver.subscribe(() => {}), () => agentListObserver.destroy());
  cleanup.push(pendingListObserver.subscribe(() => {}), () => pendingListObserver.destroy());

  return {
    request: request as <M extends AgentGuiRpcMethod>(method: M, params: AgentGuiRpcMethods[M]["params"]) => Promise<AgentGuiRpcMethods[M]["result"]>,
    load: workspaceLoad,
    subscribe(listener: (event: AgentGuiHostEvent) => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
    publish,
    reconnect() {
      notify("workspace");
      for (const id of sessionObservers.keys()) {
        notify("session", id);
        for (const taskId of tasksBySession.get(id) ?? []) void qc.invalidateQueries({ queryKey: chatKeys.taskMessages(taskId) });
      }
    },
    dispose() {
      disposed = true;
      cleanup.forEach((fn) => fn());
      sessionObservers.forEach((release) => release());
      taskObservers.forEach((release) => release());
      listeners.clear();
    },
  };
}

async function blobBase64(blob: Blob): Promise<string> {
  const bytes = new Uint8Array(await blob.arrayBuffer());
  let binary = "";
  for (let i = 0; i < bytes.length; i += 8192) binary += String.fromCharCode(...bytes.subarray(i, i + 8192));
  return btoa(binary);
}
