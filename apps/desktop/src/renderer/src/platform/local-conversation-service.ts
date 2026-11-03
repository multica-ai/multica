import { defaultStorage } from "@multica/core/platform";
import { AGENT_MENTION_SELECTION_KEY, AGENT_MENTION_USAGE_KEY, parseAgentMentionUsage } from "@multica/tutti-agent-gui-host/agent-mention-usage";
import { api } from "@multica/core/api";
import { parseWithFallback } from "@multica/core/api/schema";
import { z } from "zod";
import type { AgentTask, Attachment } from "@multica/core/types";
import { createAgentGuiParentService, type ParentServiceOptions } from "@multica/views/conversation-flow/service";
import type { AgentGuiHostEvent, AgentGuiRpcMethod, AgentGuiRpcMethods, AgentGuiSession, AgentGuiSessionDetail, AgentGuiWorkspaceState } from "@multica/tutti-agent-gui-host/protocol";
import { isTaskCreationReference, type LocalFolderProject, type LocalPromptQueue, type LocalProviderId, type LocalProviderUsage, type LocalQueuedPrompt } from "@multica/core/local-conversation/protocol";
import { localV2SummarySchema, type LocalV2Scope, type LocalV2Snapshot, type LocalV2Summary, type LocalV2Transport } from "@multica/core/local-conversation/v2";
import { isAutomaticLocalDirectory } from "@multica/tutti-agent-gui-host/local-projects";
import { projectForDirectory } from "./local-conversation-projects";
import { blobBase64 } from "./local-preview-file";
import { localV2, type LocalV2ScopeClient } from "./local-v2";

/** Query keys of a device's conversations; deviceId is "local" or another device's peer ID. */
export const localConversationKey = (scope: LocalV2Scope, deviceId = "local") => ["local-conversations", deviceId, scope.userId, scope.workspaceId] as const;
export const localTargetId = (provider: string) => `local:${provider}`;
export function localProviderId(target: string): LocalProviderId {
  if (target === "local:codex") return "codex";
  if (target === "local:claude") return "claude";
  throw new Error("Select a local Codex or Claude Code provider");
}

/** Translate only at the GUI boundary; these records never enter cloud APIs. */
export function projectLocalSession(summary: LocalV2Summary, scope: LocalV2Scope): AgentGuiSession {
  return { id: summary.conversation_id, workspace_id: scope.workspaceId, creator_id: scope.userId, agent_id: localTargetId(summary.provider),
    title: summary.title, status: summary.archived ? "archived" : "active", has_unread: false, pinned: summary.pinned, project_id: summary.project_id,
    last_message: summary.last_message_preview ? { content: summary.last_message_preview, role: "assistant", created_at: summary.updated_at } : null,
    localDirectory: { cwd: summary.cwd, projectPath: isAutomaticLocalDirectory(summary.cwd) ? null : summary.cwd },
    created_at: summary.created_at ?? summary.updated_at, updated_at: summary.updated_at };
}

/** The daemon's queue in the shape the GUI's desktop queue engine keeps. */
function projectQueue(raw: unknown): LocalPromptQueue {
  const queue = (raw ?? {}) as { revision?: number; paused?: boolean; error?: string; dispatching_request_id?: string; items?: unknown[] };
  return { revision: queue.revision ?? 0, paused: queue.paused ?? false, ...(queue.error ? { error: queue.error } : {}),
    ...(queue.dispatching_request_id ? { dispatchingRequestId: queue.dispatching_request_id } : {}), items: (queue.items ?? []) as LocalQueuedPrompt[] };
}

/** The text of a conversation as quoted context: user messages and answers. */
function conversationText(snapshot: LocalV2Snapshot): string {
  const lines: string[] = [];
  for (const turn of snapshot.turns) {
    for (const message of turn.messages) lines.push(`${message.role}: ${message.content}`);
    for (const event of turn.events) {
      if (event.type === "item.completed" && event.payload.kind === "assistant_text" && typeof event.payload.text === "string") lines.push(`assistant: ${event.payload.text}`);
    }
  }
  return lines.join("\n");
}

export function createLocalConversationParentService(options: ParentServiceOptions & {
  onLocalSelect(id: string | null, provider?: LocalProviderId): void;
  getSelectionIntent?: () => string;
  getSelectedSessionId?: () => string | null;
  onOpenSide?: (id: string, sourceId: string) => void;
  onOpenResource?: (sessionId: string, url: string) => void;
  onLocateSource?: (sessionId: string, turnId?: string, quote?: string) => void;
  storagePrefix?: string;
  locale?: string;
}, transport: LocalV2Transport): ReturnType<typeof createAgentGuiParentService> {
  const storage = options.storage ?? defaultStorage;
  const usageStorageKey = `multica:local-agent-mentions:v1:${JSON.stringify([api.getBaseUrl(), options.userId, options.workspaceId])}`;
  const scope: LocalV2Scope = { userId: options.userId, workspaceId: options.workspaceId };
  const client: LocalV2ScopeClient = localV2(transport, scope);
  const cloud = createAgentGuiParentService(options);
  const listeners = new Set<(event: AgentGuiHostEvent) => void>();
  const subscriptions = new Map<string, () => void>();
  let disposed = false;
  let cloudState: AgentGuiWorkspaceState | undefined;
  let cloudDirty = true;
  let cloudLoad: Promise<AgentGuiWorkspaceState> | undefined;
  let selectionSequence = 0;
  let workspaceTimer: ReturnType<typeof setTimeout> | undefined;
  const assertCurrent = () => { if (disposed || !options.isCurrent()) throw new Error("This workspace is no longer active"); };
  const publish = (event: AgentGuiHostEvent) => { if (!disposed && options.isCurrent()) listeners.forEach((listener) => listener(event)); };
  const remoteWorkspace = async () => {
    if (!cloudState || cloudDirty) {
      cloudLoad ??= cloud.load().then((value) => { cloudState = value; cloudDirty = false; return value; }).finally(() => { cloudLoad = undefined; });
      return cloudLoad;
    }
    return cloudState;
  };
  const conversationPath = (id: string, suffix = "") => `/sessions/${encodeURIComponent(id)}${suffix}`;
  const received = (raw: unknown) => {
    const summary = localV2SummarySchema.parse(raw);
    client.receiveSummary(summary);
    return projectLocalSession(summary, scope);
  };
  const command = async (id: string, body: Record<string, unknown>) => { assertCurrent(); return client.command(id, body); };
  const fileData = async (path: string) => {
    const response = await fetch(client.fileUrl(path));
    if (!response.ok) throw new Error(`Local file request failed (HTTP ${response.status})`);
    const disposition = response.headers.get("content-disposition") ?? "";
    const name = /filename="?([^";]+)"?/.exec(disposition)?.[1] ?? path.split("/").at(-1) ?? "file";
    return { data: await blobBase64(await response.blob()), mimeType: response.headers.get("content-type") ?? "application/octet-stream", name: decodeURIComponent(name) };
  };

  async function load(): Promise<AgentGuiWorkspaceState> {
    const [remote, providers, folderProjects] = await Promise.all([
      remoteWorkspace(),
      client.request("GET", "/providers") as Promise<{ providers: { id: LocalProviderId; name: string; available: boolean }[] }>,
      client.request("GET", "/folder-projects") as Promise<{ projects: { id: string; title: string; path: string; created_at: string }[] }>,
      client.loadSummaries(),
    ]);
    assertCurrent();
    const summaries = client.summaries();
    return { local: true, composerDraftScope: JSON.stringify([api.getBaseUrl(), options.storagePrefix ?? ""]), queueOwner: "desktop", sideWorkspace: true, sidePane: Boolean(options.storagePrefix), agents: remote.agents, movedQueueRequests: [],
      executionTargets: providers.providers.map((entry) => ({ id: localTargetId(entry.id), name: entry.name, provider: entry.id, iconUrl: "", canInvoke: entry.available, canFork: true, runtimeBound: entry.available, runtimeOnline: entry.available, unavailableReason: entry.available ? undefined : "未在本机安装" })),
      sessions: summaries.map((summary) => projectLocalSession(summary, scope)),
      folderProjects: folderProjects.projects.map((project): LocalFolderProject => ({ id: project.id, title: project.title, path: project.path, createdAt: project.created_at })),
      pendingTasks: summaries.flatMap((summary) => summary.active_turn_id ? [{ task_id: summary.active_turn_id, status: "running" as const, chat_session_id: summary.conversation_id }] : []),
    };
  }

  async function detail(id: string): Promise<{ detail: AgentGuiSessionDetail; snapshot: LocalV2Snapshot }> {
    const snapshot = await client.snapshot(id); assertCurrent();
    client.receiveSummary(snapshot.conversation);
    // The fork's copied turns started before the fork was created; the last one is where it branched.
    const branched = snapshot.turns.filter((turn) => Date.parse(turn.started_at) <= Date.parse(snapshot.created_at)).at(-1);
    const forked = snapshot.forked_from_id && snapshot.forked_from_turn_id
      ? { forkedFrom: { sessionId: snapshot.forked_from_id, turnId: snapshot.forked_from_turn_id, targetTurnId: branched?.id ?? "", operationId: id, createdAt: snapshot.created_at } } : {};
    return { snapshot, detail: {
      session: projectLocalSession(snapshot.conversation, scope), model: snapshot.settings.model, reasoningEffort: snapshot.settings.reasoning_effort as AgentGuiSessionDetail["reasoningEffort"],
      permissionMode: snapshot.settings.permission_mode as AgentGuiSessionDetail["permissionMode"], promptQueue: projectQueue(snapshot.queue),
      messages: [], pending: {}, tasks: [], ...forked,
    } };
  }

  async function referencedContent(content: string, references: { kind: string; id: string }[]): Promise<string> {
    const excerpts: string[] = [];
    for (const reference of references) {
      if (isTaskCreationReference(reference)) continue;
      if (reference.kind === "session") {
        const snapshot = await client.snapshot(reference.id);
        const messages = conversationText(snapshot);
        excerpts.push(`Multica local conversation ${JSON.stringify(snapshot.conversation.title)} (id: ${reference.id}; workspace: ${scope.workspaceId})\n${messages.slice(-6000)}\n[Conversation snapshot only: saved tool results and attachment contents are not included.${messages.length > 6000 ? " Earlier text omitted." : ""} Use the built-in Multica MCP read_context tool with kind=conversation, source=local and this ID for current messages and pagination.]`);
      } else if (reference.kind === "issue") {
        const issue = await api.getIssue(reference.id); assertCurrent();
        if (issue.workspace_id !== scope.workspaceId) throw new Error("Referenced task is outside this workspace");
        const description = issue.description || "";
        excerpts.push(`Multica task ${issue.identifier} (id: ${issue.id}; workspace: ${scope.workspaceId}) ${issue.title}\n${description.slice(0, 6000)}\n[Task snapshot only: comments, activity and attachment contents are not included.${description.length > 6000 ? " Description excerpt truncated." : ""} Query this task with the built-in Multica MCP read_context tool (kind=task) or Multica CLI for current context.]`);
      }
    }
    return excerpts.length ? `${content}\n\nReferenced context (quoted data):\n${excerpts.join("\n\n---\n\n")}` : content;
  }

  /** Mentioned agents, checked against the official workspace. */
  async function mentionedAgents(references: { kind: string; id: string }[]) {
    const ids = [...new Set(references.filter((reference) => reference.kind === "agent").map((reference) => reference.id))];
    if (!ids.length) return [];
    if (references.some(isTaskCreationReference)) throw new Error("New task requires the current local provider (Codex / Claude Code). Remove agent invocation mentions first.");
    const state = await remoteWorkspace();
    return ids.map((id) => {
      const target = state.agents.find((agent) => agent.agent.id === id);
      if (!target?.canInvoke || !target.runtimeBound || target.agent.archived_at) throw new Error("Mentioned agent is not available");
      return { id, name: target.agent.name };
    });
  }

  function unsubscribe(id: string) {
    subscriptions.get(id)?.();
    subscriptions.delete(id);
  }

  async function request(method: AgentGuiRpcMethod, raw: unknown): Promise<unknown> {
    assertCurrent(); const params = raw as Record<string, any>;
    switch (method) {
      case "workspace.load": return load();
      case "local.snapshot": return detail(params.sessionId);
      case "session.load": return (await detail(params.sessionId)).detail;
      case "local.subscribe": {
        unsubscribe(params.sessionId);
        subscriptions.set(params.sessionId, client.subscribe(params.sessionId, params.afterSeq, params.generation, (message) => publish({ type: "local.events", sessionId: params.sessionId, message })));
        return undefined;
      }
      case "local.unsubscribe": unsubscribe(params.sessionId); return undefined;
      case "session.watch": {
        const watched = new Set<string>(params.sessionIds);
        for (const id of [...subscriptions.keys()]) if (!watched.has(id)) unsubscribe(id);
        return undefined;
      }
      case "session.markRead": return undefined;
      case "queue.enqueue": {
        // Transfer ownership before any asynchronous view-bound preparation.
        const item = params.item as LocalQueuedPrompt;
        const agents = await mentionedAgents(item.references ?? []);
        if (agents.length) item.agents = agents;
        return projectQueue(await command(params.sessionId, { type: "queue_enqueue", item, paused: params.paused === true }));
      }
      case "queue.update":
        return projectQueue(await command(params.sessionId, { type: "queue_update", action: params.action === "sendNow" ? "send_now" : params.action, request_id: params.requestId }));
      case "host.openSide": {
        const pending = params.pending as { requestId: string; content: string; model?: string; reasoningEffort?: string | null; permissionMode?: string; attachmentIds?: string[]; references?: unknown[] } | undefined;
        const raw = params.queuedRequestId
          ? await command(params.sessionId, { type: "queue_side", request_id: params.queuedRequestId })
          : await command(params.sessionId, { type: "create_side", request_id: params.requestId, turn_id: params.turnId, quote: params.quote,
            ...(pending ? { item: { ...pending, display: { id: pending.requestId, clientSubmitId: pending.requestId, createdAtUnixMs: Date.now() } } } : {}) });
        const side = received(raw);
        assertCurrent(); options.onOpenSide?.(side.id, params.sessionId); return side;
      }
      case "host.startSide": return undefined; // a moved message runs in its Side by itself
      case "host.openResource": options.onOpenResource?.(params.sessionId, params.url); return undefined;
      case "host.showFileContextMenu":
        await window.desktopAPI.localFileContextMenu.show(scope, { sessionId: params.sessionId, url: params.url, hasSelection: params.hasSelection }, (transport as { deviceId?: string }).deviceId ?? "local"); return undefined;
      case "host.locateSource": options.onLocateSource?.(params.sessionId, params.turnId, params.quote); return undefined;
      case "interaction.respond":
        await command(params.sessionId, { type: "answer", interaction_id: params.requestId, answer: params.answers }); return undefined;
      case "session.fork":
        return received(await command(params.sessionId, { type: "fork", turn_id: params.turnId, request_id: params.requestId }));
      case "session.create": {
        let directory: { folder_project_id?: string; directory_source_id?: string } = {};
        if (params.cwd) {
          const { projects } = await client.request("GET", "/folder-projects") as { projects: LocalFolderProject[] };
          const folder = projectForDirectory(projects, params.cwd);
          if (folder) directory = { folder_project_id: folder.id };
          else {
            const source = !isAutomaticLocalDirectory(params.cwd) ? await client.request("GET", `/directory-source?cwd=${encodeURIComponent(params.cwd)}`) as { id: string | null } : { id: null };
            if (!source.id) throw new Error("Local project directory not found in this workspace");
            directory = { directory_source_id: source.id };
          }
        }
        assertCurrent();
        return received(await client.request("POST", "/sessions", { id: crypto.randomUUID(), provider: localProviderId(params.agentId), title: params.title ?? "", ...directory }));
      }
      case "session.rename": return received(await command(params.sessionId, { type: "update", title: params.title }));
      case "session.pin": return received(await command(params.sessionId, { type: "update", pinned: params.pinned }));
      case "session.delete": throw new Error("Archive local conversations from the session list");
      case "session.send": {
        const references: { kind: string; id: string }[] = Array.isArray(params.references) ? params.references : [];
        const agents = await mentionedAgents(references);
        const prompt = await referencedContent(params.content, references); assertCurrent();
        if (!agents.length && (params.model !== undefined || params.reasoningEffort !== undefined || params.permissionMode !== undefined)) {
          await command(params.sessionId, { type: "update_settings",
            ...(params.model !== undefined ? { model: params.model ?? null } : {}),
            ...(params.reasoningEffort !== undefined ? { reasoning_effort: params.reasoningEffort } : {}),
            ...(params.permissionMode !== undefined ? { permission_mode: params.permissionMode } : {}) });
        }
        const requestId: string = params.requestId ?? crypto.randomUUID();
        const accepted = await command(params.sessionId, { type: "send", request_id: requestId, text: params.content, prompt, references,
          attachment_ids: Array.isArray(params.attachmentIds) ? params.attachmentIds : [], ...(agents.length ? { agents } : {}) }) as { turn_id?: string; turn_ids?: string[] };
        return { message_id: `msg-${requestId}`, task_id: accepted.turn_id ?? accepted.turn_ids?.[0] ?? "", created_at: new Date().toISOString() };
      }
      case "session.cancel": {
        await command(params.sessionId, { type: "interrupt" });
        return { id: params.taskId, agent_id: "", runtime_id: "", issue_id: "", chat_session_id: params.sessionId, kind: "chat", status: "running", priority: 0,
          created_at: new Date().toISOString(), started_at: null, dispatched_at: null, completed_at: null, result: null, error: null } as AgentTask;
      }
      case "task.messages": return [];
      case "models.list": {
        const result = await client.request("GET", `/models?provider=${localProviderId(params.agentId)}${params.force ? "&force=true" : ""}`) as Record<string, unknown>;
        return { ...result, id: "local-models", runtime_id: params.agentId, status: "completed" };
      }
      case "session.compact": {
        const result = await command(params.sessionId, { type: "compact", request_id: params.requestId });
        const parsed = parseWithFallback<{ turn_id: string } | null>(result, z.object({ turn_id: z.string().min(1) }), null, { endpoint: "local-v2.compact" });
        if (!parsed) throw new Error("Invalid context compaction response. Reload and retry.");
        return { turnId: parsed.turn_id };
      }
      case "usage.read": {
        const usage = await client.request("GET", `/usage?provider=${localProviderId(params.agentId)}${params.sessionId ? `&conversation_id=${encodeURIComponent(params.sessionId)}` : ""}${params.force ? "&force=true" : ""}`) as {
          provider: LocalProviderId; context: { used_tokens: number | null; limit_tokens: number | null; model: string | null; updated_at: string | null };
          account: { status: LocalProviderUsage["account"]["status"]; plan: string | null; windows: { id: string; label: string; used_percent: number; resets_at: string | null }[]; updated_at: string | null; stale?: boolean };
        };
        return { provider: usage.provider,
          context: { usedTokens: usage.context.used_tokens, limitTokens: usage.context.limit_tokens, model: usage.context.model, updatedAt: usage.context.updated_at },
          account: { status: usage.account.status, plan: usage.account.plan, updatedAt: usage.account.updated_at, ...(usage.account.stale ? { stale: true } : {}),
            windows: usage.account.windows.map((window) => ({ id: window.id, label: window.label, usedPercent: window.used_percent, resetsAt: window.resets_at })) },
        } satisfies LocalProviderUsage;
      }
      case "storage.read":
      case "storage.write": {
        if (method === "storage.read" && params.key === AGENT_MENTION_USAGE_KEY) return storage.getItem(usageStorageKey);
        if (method === "storage.write" && params.key === AGENT_MENTION_SELECTION_KEY) {
          if (typeof params.value !== "string" || !params.value.trim()) throw new Error("Missing selected agent ID");
          // Main and Side share a synchronous read/increment/write in the parent document.
          const agents = parseAgentMentionUsage(storage.getItem(usageStorageKey));
          agents[params.value] = { count: (agents[params.value]?.count ?? 0) + 1, lastUsedAt: Date.now() };
          storage.setItem(usageStorageKey, JSON.stringify({ version: 1, agents }));
          return undefined;
        }
        return cloud.request(method, { ...params, key: `local-provider:${options.storagePrefix ?? ""}${params.key}` } as never);
      }
      case "host.selectSession": {
        const sequence = ++selectionSequence;
        const intent = options.getSelectionIntent?.();
        const summary = params.sessionId ? client.summary(params.sessionId) ?? (await client.snapshot(params.sessionId)).conversation : null;
        assertCurrent();
        if (sequence !== selectionSequence || intent !== options.getSelectionIntent?.()) return undefined;
        if (summary) options.onLocalSelect(summary.conversation_id, summary.provider as LocalProviderId);
        else options.onLocalSelect(null, params.agentId ? localProviderId(params.agentId) : undefined);
        return undefined;
      }
      case "attachment.upload": {
        if (!(params.file instanceof File) || params.file.size > 25 * 1024 * 1024) throw new Error("Invalid file, or file exceeds 25 MB");
        const uploaded = await client.upload(params.file);
        const url = client.fileUrl(`/attachments/${uploaded.id}`);
        return { id: uploaded.id, workspace_id: scope.workspaceId, issue_id: null, comment_id: null, chat_session_id: null, chat_message_id: null, uploader_type: "user", uploader_id: scope.userId,
          filename: uploaded.name, content_type: uploaded.mime_type, size_bytes: uploaded.size, url, download_url: url, markdown_url: url, created_at: new Date().toISOString() } satisfies Attachment;
      }
      case "attachment.read": return fileData(`/attachments/${encodeURIComponent(params.attachmentId)}`);
      case "image.read": return fileData(`${conversationPath(params.sessionId, "/image")}?path=${encodeURIComponent(params.path)}`);
      default: return cloud.request(method, params as never);
    }
  }

  // List changes arrive as summaries; coalesce them into one workspace reload.
  const unsubscribeSummaries = client.onSummaries(() => {
    if (workspaceTimer) return;
    workspaceTimer = setTimeout(() => { workspaceTimer = undefined; publish({ type: "invalidate", scope: "workspace" }); }, 50);
  });
  const unsubscribeCloud = cloud.subscribe((event) => { if (event.type === "invalidate" && event.scope === "workspace") { cloudDirty = true; publish(event); } });
  const unsubscribeConnection = transport.onConnection((value, connected) => {
    if (disposed || !options.isCurrent() || value.userId !== scope.userId || value.workspaceId !== scope.workspaceId) return;
    publish({ type: "connection", connected });
    if (!connected) return;
    // A failed first snapshot has no event subscription to signal its recovery.
    const selected = options.getSelectedSessionId?.();
    if (selected && !subscriptions.has(selected)) publish({ type: "local.reload", sessionId: selected });
    publish({ type: "invalidate", scope: "workspace" });
  });
  return { load, request: request as <M extends AgentGuiRpcMethod>(method: M, params: AgentGuiRpcMethods[M]["params"]) => Promise<AgentGuiRpcMethods[M]["result"]>,
    publish, subscribe: (listener) => { listeners.add(listener); return () => { listeners.delete(listener); }; },
    reconnect: () => { cloudDirty = true; cloud.reconnect(); publish({ type: "invalidate", scope: "workspace" }); },
    dispose: () => {
      disposed = true;
      for (const id of [...subscriptions.keys()]) unsubscribe(id);
      if (workspaceTimer) clearTimeout(workspaceTimer);
      unsubscribeSummaries(); unsubscribeCloud(); unsubscribeConnection(); cloud.dispose(); listeners.clear();
    },
  };
}
