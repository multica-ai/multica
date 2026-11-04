import { createLocalPresentationTiming, createLocalSendTiming, isLocalTimingModelOutput } from "../local-timing";
import {
  createAgentActivitySnapshotProjector,
  createAgentSessionEngine,
  type AgentActivityComposerOptions,
  type AgentActivityDurableMessage,
  type AgentActivitySendInput,
  type AgentActivitySession,
  type AgentActivitySnapshot,
  type AgentSessionActivateEffectInput,
  type EngineExtensionCommand,
  type EngineTypedCommandPort,
} from "@tutti-os/agent-activity-core";
import type { AgentGUIRuntime } from "@tutti-os/agent-gui";
import { extractRichTextMentionsFromContent } from "@tutti-os/ui-rich-text/core";
import { contentReferencesAttachment } from "@multica/core/types/attachment-url";
import type { Attachment } from "@multica/core/types";
import type {
  AgentGuiExecutionTarget,
  AgentGuiContextReference,
  AgentGuiInitSnapshot,
  AgentGuiRpcClient,
  AgentGuiRpcMethods,
  AgentGuiSessionDetail,
  AgentGuiSession,
  AgentGuiWorkspaceState,
  LocalConversationSource,
} from "../protocol";
import { executionTargets } from "../protocol";
import { ReasoningPreferences } from "./reasoning-preferences";
import { ModelPreferences } from "./model-preferences";
import { PermissionPreferences } from "./permission-preferences";
import type { LocalV2Event } from "@multica/core/local-conversation/v2";
import { isLocalReasoningEffort, localReasoningLevels, isLocalPermissionMode, type LocalReasoningEffort, type LocalPermissionMode } from "@multica/core/local-conversation/protocol";
import { RuntimeLedger } from "./ledger";
import { composerOptions, projectLocalMetadata, projectPendingSession, projectSession, projectV2Detail } from "./projection";
import { createDesktopQueueEngine } from "./desktop-queue-engine";
import { promptSubmissionError } from "./prompt-submission-policy";
import { createDesktopPendingStore } from "./desktop-pending-store";
import { snapshotPromptQueues } from "./saved-prompt-queues";
import { composerDraftAttachmentIds, createAttachmentUploadTracker } from "./attachment-upload-tracker";
import type { AgentComposerDraft } from "../../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/model/agentGuiNodeTypes";

const STORAGE_KEY = "runtime-ledger-v1";

export class MulticaAgentGuiError extends Error {
  constructor(message: string, public code: string, public status?: number) {
    super(message);
    this.name = "MulticaAgentGuiError";
  }
}

export function unsupported(capability: string): never {
  throw new MulticaAgentGuiError(`${capability} is not supported by this Multica connection.`, "unsupported");
}

export function createMulticaAgentGuiRuntime(client: AgentGuiRpcClient, init: AgentGuiInitSnapshot, source: LocalConversationSource) {
  const uploads = createAttachmentUploadTracker(client, init.locale);
  const workspaceId = init.workspaceId;
  const presentationTiming = createLocalPresentationTiming();
  const sendTiming = createLocalSendTiming();
  const ledger = new RuntimeLedger();
  const modelPreferences = new ModelPreferences();
  const modelListeners = new Set<() => void>();
  let modelRevision = 0;
  let modelWrites: Promise<void> = Promise.resolve();
  const modelHydration = client.request("storage.read", { key: "model-preferences-v1" }).then((value) => { modelPreferences.hydrate(value); });
  void modelHydration.catch(() => {});
  const reasoningPreferences = new ReasoningPreferences();
  const reasoningCatalogs = new Map<string, Awaited<ReturnType<typeof loadReasoningCatalog>>>();
  const reasoningHydration = client.request("storage.read", { key: "reasoning-preferences-v1" }).then((value) => { reasoningPreferences.hydrate(value); });
  void reasoningHydration.catch(() => {});
  const permissionPreferences = new PermissionPreferences();
  const permissionListeners = new Set<() => void>();
  let permissionRevision = 0;
  let permissionWrites: Promise<void> = Promise.resolve();
  const permissionHydration = client.request("storage.read", { key: "permission-preferences-v1" }).then((value) => { permissionPreferences.hydrate(value); });
  void permissionHydration.catch(() => {});
  const details = new Map<string, AgentGuiSessionDetail>();
  /** Local conversations in view: each one's release and its observer's. */
  const openLocal = new Map<string, () => void>();
  const v2Applies = new Set<string>();
  let hostVisible = init.hostVisible !== false;
  const attachments = new Map<string, Attachment>();
  const sessionLoads = new Map<string, Promise<AgentActivitySession>>();
  const dirtySessions = new Set<string>();
  const watched = new Map<string, number>();
  const workspaceListeners = new Set<() => void>();
  const eventListeners = new Set<(event: unknown) => void>();
  const sendPromises = new Map<string, Promise<unknown>>();
  const pendingIdentities = new Map<string, { promise: Promise<string>; resolve(id: string): void; reject(error: unknown): void }>();
  let workspace: AgentGuiWorkspaceState = { agents: init.agents, executionTargets: init.executionTargets, local: init.local, sessions: init.sessions, pendingTasks: init.pendingTasks, folderProjects: init.folderProjects };
  let workspaceRevision = 0;
  let workspaceLoad: Promise<AgentActivitySnapshot> | null = null;
  let workspaceDirty = false;
  let disposed = false;
  let initialized = false;
  let historyRevision = 0;
  let saveQueue = Promise.resolve();
  let saveTimer: ReturnType<typeof setTimeout> | null = null;
  let latestGeneration = init.generation ?? 0;
  let attention = { completed: { readIds: [] as string[], unreadIds: [] as string[] }, failed: { readIds: [] as string[], unreadIds: [] as string[] } };

  const persist = () => {
    const value = ledger.serialize();
    const next = saveQueue.catch(() => {}).then(() => client.request("storage.write", { key: STORAGE_KEY, value }));
    saveQueue = next;
    return next;
  };
  const schedulePersist = () => {
    if (saveTimer || disposed) return;
    saveTimer = setTimeout(() => {
      saveTimer = null;
      void persist().catch(reportError);
    }, 200);
  };
  const commandPort: EngineTypedCommandPort = {
    kind: "typed",
    effects: {
      activateSession: (input, options) => activate(input, options.signal),
      sendInput: (input, options) => send(input, options.signal),
      cancelTurn: async (input, options) => {
        scope(input.workspaceId);
        const result = await client.request("session.cancel", {
          sessionId: await backendSessionId(input.agentSessionId), taskId: input.turnId,
        }, { signal: options.signal });
        const detail = details.get(input.agentSessionId);
        if (detail) {
          detail.tasks = [...(detail.tasks ?? []).filter((task) => task.id !== result.id), result];
        }
        await loadFreshSession(input.agentSessionId);
        const turn = Object.values(engine.getSnapshot().sessionLifecycle.turnsById)
          .find((entry) => entry.agentSessionId === input.agentSessionId && entry.turnId === input.turnId);
        if (!turn) throw new MulticaAgentGuiError("The cancellation was accepted, but its run is still synchronizing.", "cancel_pending_sync");
        return turn.phase === "settled"
          ? { cancel: { canceled: turn.outcome === "canceled", reason: turn.outcome === "canceled" ? "turn_canceled" : "already_settled" }, turn }
          : { cancel: { canceled: false, reason: "cancel_requested" }, turn: { ...turn, phase: "settling", outcome: null } };
      },
      deleteSessions: async () => unsupported("Recoverable session deletion"),
      respondToInteraction: async (input, options) => {
        scope(input.workspaceId);
        if (input.action !== "submit" || !input.payload?.answersByQuestionId) unsupported("This interaction response");
        await client.request("interaction.respond", {
          sessionId: await backendSessionId(input.agentSessionId), turnId: input.turnId, requestId: input.requestId,
          answers: input.payload.answersByQuestionId as Record<string, string | string[]>,
        }, { signal: options.signal });
        return { session: await loadFreshSession(input.agentSessionId) };
      },
      renameSession: async (input, options) => {
        scope(input.workspaceId);
        const session = await client.request("session.rename", {
          sessionId: await backendSessionId(input.agentSessionId), title: input.title,
        }, { signal: options?.signal });
        return { session: replaceMetadata(session) };
      },
      setSessionPinned: async (input, options) => {
        scope(input.workspaceId);
        const session = await client.request("session.pin", {
          sessionId: await backendSessionId(input.agentSessionId), pinned: input.pinned,
        }, { signal: options?.signal });
        return { session: replaceMetadata(session) };
      },
      updateSessionSettings: async (input) => {
        scope(input.workspaceId);
        const session = engine.getSnapshot().sessionLifecycle.sessionsById[input.agentSessionId];
        if (!session?.agentTargetId) throw new MulticaAgentGuiError("Conversation is not loaded.", "session_not_loaded");
        if (Object.keys(input.settings).some((key) => key !== "model" && key !== "reasoningEffort")) unsupported("Non-model conversation settings");
        if ("model" in input.settings) await setModelSelection(session.agentTargetId, input.agentSessionId, input.settings.model ?? null);
        if ("reasoningEffort" in input.settings) await setReasoningSelection(session.agentTargetId, input.agentSessionId, input.settings.reasoningEffort ?? null);
        return { agentSessionId: input.agentSessionId, session: projectSnapshot(engine.getSnapshot()).sessions.find((entry) => entry.agentSessionId === input.agentSessionId) };
      },
    },
    execute: executeExtension,
  };
  const baseEngine = createAgentSessionEngine({
    identity: { workspaceId, origin: "multica" },
    clock: { nowUnixMs: () => Date.now() },
    scheduler: { schedule: (delay, callback) => {
      const timer = setTimeout(callback, delay);
      return { cancel: () => clearTimeout(timer) };
    } },
    commandPort,
  });
  const desktopQueue = createDesktopQueueEngine(baseEngine, {
    pendingStore: init.composerDraftScope
      ? createDesktopPendingStore(window.sessionStorage, JSON.stringify([init.composerDraftScope, init.userId, workspaceId]))
      : undefined,
    locale: () => init.locale,
    enqueue: enqueueDesktop,
    update: async (id, action, requestId) => client.request("queue.update", { sessionId: await backendSessionId(id), action, requestId }),
    error(error) {
      reportError(error);
      if (!disposed) void client.request("host.showMessage", {
        message: error instanceof Error ? error.message : String(error), tone: "error",
      }).catch(reportError);
    },
  });
  const engine = desktopQueue.engine;
  const projectSnapshot = createAgentActivitySnapshotProjector(workspaceId);
  const unrestoredQueueSessionIds = new Set<string>();

  async function restorePromptQueues() {
    for (const queue of ledger.savedPromptQueues()) {
      try {
        await loadFreshSession(queue.agentSessionId);
        for (const prompt of queue.prompts) {
          const receipt = ledger.submission(prompt.clientSubmitId || prompt.id);
          if (receipt?.state === "accepted") continue;
          // Ambiguous old delivery must be inspected rather than replayed.
          if (receipt) throw unconfirmed();
          await enqueueDesktop({ ...prompt, agentSessionId: queue.agentSessionId, clientSubmitId: prompt.clientSubmitId || prompt.id }, true);
        }
      } catch (error) {
        unrestoredQueueSessionIds.add(queue.agentSessionId); reportError(error);
        void client.request("host.showMessage", { message: init.locale === "zh-CN"
          ? "部分旧排队消息未能确认，原内容已保留。请先检查会话记录，避免重复发送。"
          : "Some saved queued messages could not be confirmed. Their contents are preserved. Check the conversation before resending.", tone: "warning" }).catch(reportError);
      }
    }
    ledger.savePromptQueues({ nextCommandSequence: 0, recordsBySessionId: {} }, unrestoredQueueSessionIds, []);
    await persist();
  }

  function scope(id: string) {
    if (disposed) throw new MulticaAgentGuiError("This Agent GUI connection has closed.", "disposed");
    if (id !== workspaceId) throw new MulticaAgentGuiError("The workspace changed.", "workspace_mismatch");
  }
  function descriptor(id: string): AgentGuiExecutionTarget {
    const result = executionTargets(workspace).find((entry) => entry.id === id);
    if (!result) throw new MulticaAgentGuiError("This agent is not available.", "agent_unavailable");
    return result;
  }
  function resolvedServerId(guiId: string): string | null {
    const serverId = ledger.serverId(guiId);
    return serverId !== guiId || workspace.sessions.some((session) => session.id === guiId) ? serverId : null;
  }
  async function backendSessionId(guiId: string): Promise<string> {
    const id = resolvedServerId(guiId);
    if (id) return id;
    const pending = pendingIdentities.get(guiId);
    if (pending) return pending.promise;
    throw new MulticaAgentGuiError("This conversation has not been created by Multica.", "session_not_ready");
  }
  function assertInvoke(agentId: string, sessionId?: string) {
    const agent = descriptor(agentId);
    if (!agent.canInvoke) {
      throw new MulticaAgentGuiError(agent.unavailableReason || "You cannot invoke this agent.", "invocation_not_allowed", 403);
    }
    if (!agent.runtimeBound) throw new MulticaAgentGuiError("This agent has no runtime.", "runtime_required");
    const session = sessionId ? details.get(sessionId)?.session
      ?? workspace.sessions.find((entry) => entry.id === ledger.serverId(sessionId)) : undefined;
    if (session?.status === "archived") {
      throw new MulticaAgentGuiError("This conversation is archived.", "session_archived");
    }
    return agent;
  }
  /** Claude's default used to be a catalog row. A saved one now means what no selection means, so it reaches the catalog's default model. */
  function followingClaudeDefault(agentId: string, model: string | null) {
    return model === "default" && executionTargets(workspace).find((entry) => entry.id === agentId)?.provider === "claude";
  }
  function selectedModel(agentId: string, sessionId?: string | null) {
    const session = sessionId ? details.get(sessionId)?.session ?? workspace.sessions.find((entry) => entry.id === ledger.serverId(sessionId)) : undefined;
    if (session && session.agent_id !== agentId) return null;
    const serverId = sessionId ? ledger.serverId(sessionId) : null;
    const model = modelPreferences.has(agentId, serverId) ? modelPreferences.get(agentId, serverId) : sessionId ? details.get(sessionId)?.model ?? null : null;
    return followingClaudeDefault(agentId, model) ? null : model;
  }
  async function loadReasoningCatalog(agentId: string, options?: { force?: boolean; signal?: AbortSignal }) {
    const catalog = await client.request("models.list", { agentId, ...(options?.force ? { force: true } : {}) }, { signal: options?.signal });
    reasoningCatalogs.set(agentId, catalog);
    return catalog;
  }
  function selectedReasoning(agentId: string, sessionId?: string | null): LocalReasoningEffort | null {
    const detail = sessionId ? details.get(sessionId) : undefined;
    if (detail && detail.session.agent_id !== agentId) return null;
    const model = selectedModel(agentId, sessionId);
    const selection = reasoningPreferences.get(agentId, sessionId ? ledger.serverId(sessionId) : null);
    const effort = selection ? ((followingClaudeDefault(agentId, selection.model) ? null : selection.model) === model ? selection.effort : null)
      : model === (detail?.model ?? null) ? detail?.reasoningEffort ?? null : null;
    const catalog = reasoningCatalogs.get(agentId);
    return effort && (!catalog || localReasoningLevels(catalog.models, model ?? descriptor(agentId).model).some((level) => level.value === effort)) ? effort : null;
  }
  async function reasoningForSend(agentId: string, sessionId?: string | null) {
    await reasoningHydration;
    await modelWrites.catch(() => {});
    if (selectedReasoning(agentId, sessionId)) await loadReasoningCatalog(agentId);
    return selectedReasoning(agentId, sessionId);
  }
  function setReasoningSelection(agentId: string, sessionId: string | null | undefined, effort: string | null) {
    const operation = modelWrites.catch(() => {}).then(async () => {
      await reasoningHydration;
      await modelHydration;
      scope(workspaceId);
      const agent = assertInvoke(agentId, sessionId ?? undefined);
      if (effort !== null && !isLocalReasoningEffort(effort)) throw new MulticaAgentGuiError("Invalid thinking effort.", "invalid_reasoning_effort");
      if (sessionId) {
        const session = details.get(sessionId)?.session ?? workspace.sessions.find((entry) => entry.id === ledger.serverId(sessionId));
        if (!session || session.agent_id !== agentId) throw new MulticaAgentGuiError("The thinking scope does not belong to this provider.", "invalid_session", 409);
      }
      if (isModelSelectionBusy(sessionId)) throw new MulticaAgentGuiError("Wait for this run to finish before changing thinking effort.", "run_in_progress", 409);
      const model = selectedModel(agentId, sessionId);
      if (effort !== null && !localReasoningLevels((await loadReasoningCatalog(agentId)).models, model ?? agent.model).some((level) => level.value === effort)) throw new MulticaAgentGuiError("This model does not support that thinking effort.", "invalid_reasoning_effort");
      // The catalog request can overlap a newly accepted turn.
      if (isModelSelectionBusy(sessionId)) throw new MulticaAgentGuiError("Wait for this run to finish before changing thinking effort.", "run_in_progress", 409);
      const next = new ReasoningPreferences(); next.hydrate(reasoningPreferences.serialize());
      next.set(agentId, sessionId ? ledger.serverId(sessionId) : null, model, effort);
      await client.request("storage.write", { key: "reasoning-preferences-v1", value: next.serialize() });
      scope(workspaceId); reasoningPreferences.hydrate(next.serialize());
      if (sessionId) {
        const current = projectSnapshot(engine.getSnapshot()).sessions.find((entry) => entry.agentSessionId === sessionId);
        if (current) engine.dispatch({ type: "session/upserted", session: withModel(current) });
      }
      modelRevision++; modelListeners.forEach((listener) => listener());
    });
    modelWrites = operation;
    return operation;
  }
  function withModel(session: AgentActivitySession): AgentActivitySession {
    const agent = executionTargets(workspace).find((entry) => entry.id === session.agentTargetId);
    return { ...session, settings: { ...session.settings, reasoningEffort: selectedReasoning(session.agentTargetId ?? "", session.agentSessionId), model: selectedModel(session.agentTargetId ?? "", session.agentSessionId) ?? agent?.model ?? null } };
  }
  function isModelSelectionBusy(sessionId?: string | null) {
    if (!sessionId) return false;
    const canonical = engine.getSnapshot().sessionLifecycle.sessionsById[sessionId];
    return Boolean(canonical?.activeTurnId || workspace.pendingTasks?.some((task) => task.chat_session_id === ledger.serverId(sessionId)));
  }
  function selectedPermission(agentId: string, sessionId?: string | null): LocalPermissionMode {
    const detail = sessionId ? details.get(sessionId) : undefined;
    const session = sessionId ? detail?.session ?? workspace.sessions.find((entry) => entry.id === ledger.serverId(sessionId)) : undefined;
    if (session && session.agent_id !== agentId) return "default";
    const saved = permissionPreferences.get(agentId, sessionId ? ledger.serverId(sessionId) : null) ?? detail?.permissionMode;
    if (saved !== undefined) return saved;
    // A remembered default only applies to drafts and confirmed empty conversations.
    if (sessionId && (!detail || detail.messages.length > 0 || (detail.tasks?.length ?? 0) > 0 || detail.pending.task_id || detail.forkedFrom)) return "default";
    return permissionPreferences.get(agentId) ?? "default";
  }
  function setPermissionSelection(agentId: string, sessionId: string | null | undefined, mode: LocalPermissionMode) {
    const operation = permissionWrites.catch(() => {}).then(async () => {
      await permissionHydration;
      scope(workspaceId);
      if (!isLocalPermissionMode(mode)) throw new MulticaAgentGuiError("Invalid local permission mode.", "invalid_permission_mode");
      assertInvoke(agentId, sessionId ?? undefined);
      const session = sessionId ? details.get(sessionId)?.session ?? workspace.sessions.find((entry) => entry.id === ledger.serverId(sessionId)) : undefined;
      if (sessionId && (!session || session.agent_id !== agentId)) throw new MulticaAgentGuiError("The permission scope does not belong to this provider.", "invalid_session", 409);
      if (isModelSelectionBusy(sessionId)) throw new MulticaAgentGuiError("Wait for this run to finish before changing permissions.", "run_in_progress", 409);
      const next = new PermissionPreferences();
      next.hydrate(permissionPreferences.serialize());
      next.set(agentId, null, mode);
      if (sessionId) next.set(agentId, ledger.serverId(sessionId), mode);
      await client.request("storage.write", { key: "permission-preferences-v1", value: next.serialize() });
      scope(workspaceId);
      permissionPreferences.hydrate(next.serialize());
      permissionRevision++;
      permissionListeners.forEach((listener) => listener());
    });
    permissionWrites = operation;
    return operation;
  }
  function setModelSelection(agentId: string, sessionId: string | null | undefined, model: string | null) {
    const operation = modelWrites.catch(() => {}).then(async () => {
      await modelHydration;
      scope(workspaceId);
      assertInvoke(agentId, sessionId ?? undefined);
      const session = sessionId ? details.get(sessionId)?.session ?? workspace.sessions.find((entry) => entry.id === ledger.serverId(sessionId)) : undefined;
      if (sessionId && (!session || session.agent_id !== agentId)) throw new MulticaAgentGuiError("The model scope does not belong to this agent.", "invalid_session", 409);
      if (isModelSelectionBusy(sessionId)) throw new MulticaAgentGuiError("Wait for this run to finish before changing its model.", "run_in_progress", 409);
      const next = new ModelPreferences();
      next.hydrate(modelPreferences.serialize());
      next.set(agentId, sessionId ? ledger.serverId(sessionId) : null, model);
      await client.request("storage.write", { key: "model-preferences-v1", value: next.serialize() });
      scope(workspaceId);
      modelPreferences.hydrate(next.serialize());
      if (sessionId) {
        const current = projectSnapshot(engine.getSnapshot()).sessions.find((entry) => entry.agentSessionId === sessionId);
        if (current) engine.dispatch({ type: "session/upserted", session: withModel(current) });
      }
      modelRevision++;
      modelListeners.forEach((listener) => listener());
    });
    modelWrites = operation;
    return operation;
  }
  function markAvailability(session: AgentActivitySession) {
    const agent = executionTargets(workspace).find((entry) => entry.id === session.agentTargetId);
    engine.dispatch({
      type: "session/runtimeAvailabilityChanged", agentSessionId: session.agentSessionId,
      availability: agent?.canInvoke && agent.runtimeBound && session.resumable
        ? { state: "available" }
        : { state: "blocked", reason: "agent_capability_unavailable" },
    });
  }
  function replaceMetadata(session: AgentGuiSession): AgentActivitySession {
    const id = ledger.guiId(session.id);
    const detail = details.get(id);
    if (detail) detail.session = session;
    workspace = { ...workspace, sessions: [...workspace.sessions.filter((entry) => entry.id !== session.id), session] };
    const value = detail
      ? projectLocalMetadata(detail, executionTargets(workspace).find((entry) => entry.id === session.agent_id), ledger).session
      : projectSession(session, executionTargets(workspace).find((entry) => entry.id === session.agent_id), ledger);
    engine.dispatch({ type: "session/upserted", session: withModel(value) });
    markAvailability(value);
    for (const listener of workspaceListeners) listener();
    return withModel(value);
  }
  function reportError(error: unknown) {
    if (disposed) return;
    console.error("[multica-agent-gui]", error);
  }
  /** A hidden GUI, such as a closed Side, holds nothing: it reads what it missed when shown again. */
  function syncLocalWatch() {
    const wanted = new Set(hostVisible ? [...watched.keys()].map(resolvedServerId).filter((id): id is string => Boolean(id)) : []);
    for (const [id, release] of openLocal) if (!wanted.has(id)) { release(); openLocal.delete(id); }
    for (const id of wanted) {
      if (openLocal.has(id)) continue;
      const stopObserving = source.observe(id, (events) => receiveLocal(ledger.guiId(id), events));
      const leave = source.open(id);
      openLocal.set(id, () => { leave(); stopObserving(); });
    }
  }
  function receiveWorkspace(next: AgentGuiWorkspaceState) {
    const previousPending = workspace.pendingTasks;
    workspace = next;
    workspaceRevision++;
    const pendingBySession = new Map<string, NonNullable<AgentGuiWorkspaceState["pendingTasks"]>[number]>();
    for (const task of workspace.pendingTasks ?? []) {
      const current = pendingBySession.get(task.chat_session_id);
      if (!current || current.status === "queued" && task.status !== "queued") pendingBySession.set(task.chat_session_id, task);
    }
    const sessions = workspace.sessions.map((session) => {
      const id = ledger.guiId(session.id);
      const detail = details.get(id);
      if (detail) {
        detail.session = session;
        return project(id).session;
      }
      return projectPendingSession(session, executionTargets(workspace).find((entry) => entry.id === session.agent_id), ledger, pendingBySession.get(session.id));
    });
    engine.dispatch({ type: "session/snapshotReceived", sessions: sessions.map(withModel) });
    for (const session of sessions) markAvailability(session);
    // A disappearing aggregate row does not prove a completion outcome.
    // Reconcile that previously live conversation against real detail once.
    if (workspace.pendingTasks !== undefined) {
      const refresh = new Set((previousPending ?? [])
        .filter((task) => !workspace.pendingTasks!.some((next) => next.task_id === task.task_id && next.status === task.status))
        .map((task) => ledger.guiId(task.chat_session_id)));
      for (const task of workspace.pendingTasks) {
        const id = ledger.guiId(task.chat_session_id);
        if (details.has(id) && !(previousPending ?? []).some((previous) => previous.task_id === task.task_id && previous.status === task.status)) refresh.add(id);
      }
      for (const id of refresh) void loadSession(id).catch(reportError);
    }
    for (const listener of workspaceListeners) listener();
  }

  async function loadWorkspace(): Promise<AgentActivitySnapshot> {
    scope(workspaceId);
    if (workspaceLoad) return workspaceLoad;
    workspaceLoad = (async () => {
      const revision = workspaceRevision;
      const next = await client.request("workspace.load", {});
      scope(workspaceId);
      // An init received during this request already carries newer host state.
      if (revision === workspaceRevision) receiveWorkspace(next);
      return projectSnapshot(engine.getSnapshot());
    })().finally(() => {
      workspaceLoad = null;
      if (workspaceDirty && !disposed) {
        workspaceDirty = false;
        void loadWorkspace().catch(reportError);
      }
    });
    return workspaceLoad;
  }

  /** The session's view; only metadata for a conversation the source no longer holds. */
  function project(guiId: string) {
    const detail = details.get(guiId)!;
    const agent = executionTargets(workspace).find((entry) => entry.id === detail.session.agent_id);
    const conversation = localConversation(guiId);
    return conversation
      ? projectV2Detail(detail, conversation, agent, ledger, init.locale)
      : projectLocalMetadata(detail, agent, ledger);
  }

  function localConversation(guiId: string) {
    const id = resolvedServerId(guiId);
    return id ? source.conversation(id) : undefined;
  }

  /** Projects a local conversation from the source, which keeps it current. */
  async function loadLocalSession(guiId: string, fresh: boolean) {
    const sessionId = await backendSessionId(guiId);
    await (fresh ? source.refresh(sessionId) : source.ensure(sessionId));
    const detail = source.detail(sessionId);
    if (!detail) throw new MulticaAgentGuiError("This conversation is not available.", "session_unavailable");
    scope(detail.session.workspace_id);
    details.set(guiId, detail);
    scope(workspaceId);
    return applyDetail(guiId);
  }

  /** A local conversation changed: new events, a page of history or its queue. */
  function receiveLocal(guiId: string, events: readonly LocalV2Event[]) {
    const conversation = localConversation(guiId);
    if (!conversation) return;
    for (const event of events) {
      if (event.turn_id && isLocalTimingModelOutput(event.type, event.payload.kind)) presentationTiming.received(conversation.summary.conversation_id, event.turn_id);
    }
    // Coalesce a burst of event batches into one projection.
    if (v2Applies.has(guiId)) return;
    v2Applies.add(guiId);
    queueMicrotask(() => {
      v2Applies.delete(guiId);
      if (disposed || !details.has(guiId)) return;
      const id = resolvedServerId(guiId);
      const detail = id ? source.detail(id) : null;
      if (detail) details.set(guiId, detail);
      applyDetail(guiId);
    });
  }

  async function loadSession(guiId: string, fresh = false): Promise<AgentActivitySession> {
    scope(workspaceId);
    const current = sessionLoads.get(guiId);
    if (current) return current;
    const operation = loadLocalSession(guiId, fresh).finally(() => {
      sessionLoads.delete(guiId);
      if (dirtySessions.delete(guiId) && watched.has(guiId) && !disposed) void loadSession(guiId, true).catch(reportError);
    });
    sessionLoads.set(guiId, operation);
    return operation;
  }

  async function loadFreshSession(guiId: string) {
    await sessionLoads.get(guiId)?.catch(() => {});
    return loadSession(guiId, true);
  }

  function applyDetail(guiId: string): AgentActivitySession {
    const detail = details.get(guiId)!;
    desktopQueue.receive(guiId, detail.promptQueue);
    const result = project(guiId);
    const unsettled = new Set(Object.values(engine.getSnapshot().sessionLifecycle.turnsById)
      .filter((turn) => turn.agentSessionId === guiId && turn.phase !== "settled").map((turn) => turn.turnId));
    engine.dispatch({
      type: "session/historyAuthoritativeSnapshotReceived", workspaceId,
      agentSessionId: guiId, childSessions: [], historyRevision: ++historyRevision,
      session: withModel(result.session), turns: result.turns, messages: result.messages,
      sessionMessageWindows: [{ agentSessionId: guiId, hasOlderMessages: localConversation(guiId)?.hasOlderTurns ?? false,
        oldestLoadedVersion: result.messages.reduce<number | null>((oldest, message) => oldest === null || message.version < oldest ? message.version : oldest, null) }],
    });
    // A same-timestamp terminal result can leave the engine's previous active
    // pointer intact. Fence that specific transition once: replaying every
    // historical settlement would itself request another live reconciliation.
    for (const turn of result.turns) {
      if (turn.phase === "settled" && engine.getSnapshot().sessionLifecycle.sessionsById[guiId]?.activeTurnId === turn.turnId) engine.dispatch({
        type: "turn/projectionReceived", workspaceId, turn, activeTurnId: null,
        hostFencedSameTurnSettlement: true,
      });
    }
    // History snapshots never raise attention. A run this runtime saw unsettled
    // that history now settles is a real completion: replay it as an accepted
    // live completion so the conversation list shows its unread mark.
    for (const turn of Object.values(engine.getSnapshot().sessionLifecycle.turnsById)) {
      if (turn.agentSessionId === guiId && turn.phase === "settled" && unsettled.has(turn.turnId)) engine.dispatch({
        type: "turn/upserted", live: true, replayAcceptedLiveCompletion: true, turn,
      });
    }
    const conversation = localConversation(guiId);
    for (const turn of result.turns) {
      presentationTiming.applied(detail.session.id, turn.turnId, (conversation?.turnItems(turn.turnId).length ?? 0) > 0, turn.phase === "settled");
    }
    markAvailability(result.session);
    schedulePersist();
    return withModel(result.session);
  }

  async function activate(input: AgentSessionActivateEffectInput, signal?: AbortSignal) {
    scope(input.workspaceId);
    if (input.mode === "existing") {
      const session = await loadSession(input.agentSessionId);
      return { activation: { mode: "existing" as const, status: "already_attached" as const }, session,
        detail: { projection: "authoritative" as const, lifecycleCapabilitiesProjected: true, session, childSessions: [], turns: Object.values(engine.getSnapshot().sessionLifecycle.turnsById).filter((turn) => turn.agentSessionId === input.agentSessionId) } };
    }
    await modelHydration;
    await modelWrites.catch(() => {});
    const agent = assertInvoke(input.agentTargetId);
    if (input.initialGoalControl || input.initialTuttiModeActivation || input.isolation) unsupported("Goal, Tutti mode and local project activation");
    if (input.settings && Object.entries(input.settings).some(([key, value]) => key !== "model" && key !== "reasoningEffort" && value != null && value !== false)) unsupported("Non-model conversation settings");
    let serverId = ledger.serverId(input.agentSessionId);
    if (serverId === input.agentSessionId) {
      if (ledger.creation(input.agentSessionId)) throw unconfirmed();
      ledger.beginCreation(input.agentSessionId);
      let resolveIdentity!: (id: string) => void;
      let rejectIdentity!: (error: unknown) => void;
      const identity = new Promise<string>((resolve, reject) => { resolveIdentity = resolve; rejectIdentity = reject; });
      void identity.catch(() => {});
      pendingIdentities.set(input.agentSessionId, { promise: identity, resolve: resolveIdentity, reject: rejectIdentity });
      try {
        await persist();
        const session = await client.request("session.create", { agentId: input.agentTargetId, title: input.title, ...(input.cwd ? { cwd: input.cwd } : {}) }, { signal });
        ledger.bind(input.agentSessionId, session.id);
        serverId = session.id;
        const model = selectedModel(agent.id) ?? (input.settings?.model && input.settings.model !== agent.model ? input.settings.model : null);
        modelPreferences.set(agent.id, session.id, model);
        await client.request("storage.write", { key: "model-preferences-v1", value: modelPreferences.serialize() });
        await reasoningHydration;
        const effort = reasoningPreferences.get(agent.id) ? selectedReasoning(agent.id) : (isLocalReasoningEffort(input.settings?.reasoningEffort) ? input.settings.reasoningEffort : null);
        reasoningPreferences.set(agent.id, session.id, model, effort);
        await client.request("storage.write", { key: "reasoning-preferences-v1", value: reasoningPreferences.serialize() });
        await permissionHydration;
        await permissionWrites.catch(() => {});
        permissionPreferences.set(agent.id, session.id, selectedPermission(agent.id));
        await client.request("storage.write", { key: "permission-preferences-v1", value: permissionPreferences.serialize() });
        await persist();
        replaceMetadata(session);
        resolveIdentity(session.id);
        syncLocalWatch();
      } catch (error) {
        rejectIdentity(error);
        if (definitiveRejection(error)) ledger.discardCreation(input.agentSessionId);
        else ledger.failCreation(input.agentSessionId);
        await persist();
        throw error;
      } finally {
        pendingIdentities.delete(input.agentSessionId);
      }
    }
    const session = await loadSession(input.agentSessionId);
    // Creation is complete. The queue owns admission failures and explicit
    // retries, including the first prompt's content and stable submit ID.
    if (input.initialContent?.length) engine.submitPrompt({
      agentSessionId: input.agentSessionId, clientSubmitId: input.clientSubmitId,
      content: input.initialContent,
      displayPrompt: input.initialDisplayPrompt,
    });
    return { activation: { mode: "new" as const, status: "attached" as const }, session };
  }

  const desktopAdmissions = new WeakMap<Parameters<typeof baseEngine.submitPrompt>[0], AgentGuiRpcMethods["queue.enqueue"]["params"]>();
  async function enqueueDesktop(input: Parameters<typeof baseEngine.submitPrompt>[0], paused = false) {
    const startedAt = Date.now();
    const previous = desktopAdmissions.get(input);
    if (previous) {
      await uploads.ensure(previous.item.attachmentIds ?? []);
      const retry = () => client.request("queue.enqueue", previous);
      const result = await sendTiming.begin(previous.sessionId, input.clientSubmitId, startedAt).measure("client.request", retry);
      uploads.forget(previous.item.attachmentIds ?? []);
      desktopQueue.receive(input.agentSessionId, result);
      return result;
    }
    const sessionId = await backendSessionId(input.agentSessionId);
    const session = details.get(input.agentSessionId)?.session ?? workspace.sessions.find((item) => item.id === sessionId);
    if (!session) throw new Error("Conversation is not loaded");
    const submissionError = promptSubmissionError(input, init.locale);
    if (submissionError) throw new MulticaAgentGuiError(submissionError, "unsupported");
    const encoded = encodeContent([...(input.runtimeContent ?? input.content)]);
    // The prompt waits here in the visible pending queue, with its failure and retry.
    await uploads.ensure(encoded.attachmentIds ?? []);
    const display = snapshotPromptQueues({ nextCommandSequence: 0, recordsBySessionId: {
      [input.agentSessionId]: { agentSessionId: input.agentSessionId, workspaceId, prompts: [{ ...input, id: input.clientSubmitId, createdAtUnixMs: Date.now() }],
        deliveryBarrierTurnId: null, failedPromptId: null, failureMessage: null, inFlight: null, sendNextPromptId: null, suspendReason: null, uncertainDelivery: null },
    } }, [...attachments.values()])[0]!.prompts[0]!;
    // Restoring a saved queue is not the user sending anything.
    const timing = !paused ? sendTiming.begin(sessionId, input.clientSubmitId, startedAt) : undefined;
    const reasoningEffort = await reasoningForSend(session.agent_id, input.agentSessionId);
    const params: AgentGuiRpcMethods["queue.enqueue"]["params"] = { sessionId, paused, queued: true, item: { ...encoded, requestId: input.clientSubmitId, display,
      ...(selectedModel(session.agent_id, input.agentSessionId) ? { model: selectedModel(session.agent_id, input.agentSessionId)! } : {}),
      reasoningEffort, permissionMode: selectedPermission(session.agent_id, input.agentSessionId),
    } };
    // Keep the admitted payload stable even if preferences change before an
    // explicit retry of a failed or lost acknowledgement.
    desktopAdmissions.set(input, params);
    const enqueue = () => client.request("queue.enqueue", params);
    const result = await (timing ? timing.measure("client.request", enqueue) : enqueue());
    uploads.forget(encoded.attachmentIds ?? []);
    desktopQueue.receive(input.agentSessionId, result);
    return result;
  }

  async function send(input: AgentActivitySendInput, signal?: AbortSignal): Promise<unknown> {
    const startedAt = Date.now();
    scope(input.workspaceId);
    await modelHydration;
    await modelWrites.catch(() => {});
    await permissionHydration;
    await permissionWrites.catch(() => {});
    scope(input.workspaceId);
    const submissionError = promptSubmissionError(input, init.locale);
    if (submissionError) throw new MulticaAgentGuiError(submissionError, "unsupported");
    const detail = details.get(input.agentSessionId);
    const session = detail?.session ?? workspace.sessions.find((entry) => entry.id === ledger.serverId(input.agentSessionId));
    if (!session) throw new MulticaAgentGuiError("Conversation is not loaded.", "session_not_loaded");
    if (session.status === "archived") throw new MulticaAgentGuiError("This conversation is archived.", "session_archived");
    const pending = sendPromises.get(input.clientSubmitId);
    if (pending) return pending;
    const previous = ledger.submission(input.clientSubmitId);
    if (previous && previous.state !== "accepted") throw unconfirmed();
    const operation = (async () => {
      if (!previous?.response) {
        const encoded = encodeContent(input.content);
        await uploads.ensure(encoded.attachmentIds ?? []);
        const recipients = encoded.references?.filter((entry) => entry.kind === "agent") ?? [];
        if (recipients.length) recipients.forEach((entry) => {
          const target = workspace.agents.find((item) => item.agent.id === entry.id);
          if (!target?.canInvoke || !target.runtimeBound || target.agent.archived_at) throw new MulticaAgentGuiError("Mentioned agent is unavailable.", "agent_unavailable", 403);
        });
        else assertInvoke(session.agent_id, input.agentSessionId);
        ledger.setSubmission(input.clientSubmitId, { sessionId: session.id, state: "pending" });
        await persist();
        try {
          const timing = sendTiming.begin(session.id, input.clientSubmitId, startedAt);
          const reasoningEffort = !recipients.length ? await reasoningForSend(session.agent_id, input.agentSessionId) : null;
          const submit = () => client.request("session.send", {
            sessionId: session.id, ...encoded, requestId: input.clientSubmitId,
            ...(input.submitDiagnostics?.queued === true ? { queued: true as const } : {}),
            ...(!recipients.length && selectedModel(session.agent_id, input.agentSessionId) ? { model: selectedModel(session.agent_id, input.agentSessionId)! } : {}),
            ...(!recipients.length ? { reasoningEffort, permissionMode: selectedPermission(session.agent_id, input.agentSessionId) } : {}),
          }, { signal });
          const response = await timing.measure("client.request", submit);
          uploads.forget(encoded.attachmentIds ?? []);
          ledger.setSubmission(input.clientSubmitId, { sessionId: session.id, state: "accepted", response });
          await persist();
        } catch (error) {
          if (definitiveRejection(error)) ledger.discardSubmission(input.clientSubmitId);
          else ledger.setSubmission(input.clientSubmitId, { sessionId: session.id, state: "unknown" });
          await persist();
          throw error;
        }
      }
      const projected = await loadFreshSession(input.agentSessionId);
      const response = ledger.submission(input.clientSubmitId)!.response!;
      const turn = Object.values(engine.getSnapshot().sessionLifecycle.turnsById).find((item) => item.agentSessionId === input.agentSessionId && item.turnId === response.task_id);
      if (!turn) throw new MulticaAgentGuiError("The message was accepted; the run is still synchronizing. Refresh this conversation.", "accepted_pending_sync");
      return { kind: "turn", session: projected, turnId: response.task_id, turn };
    })().finally(() => { sendPromises.delete(input.clientSubmitId); });
    sendPromises.set(input.clientSubmitId, operation);
    return operation;
  }

  async function uploadAttachment(file: File) {
    const attachment = await client.request("attachment.upload", { file, background: true });
    registerAttachments([attachment]);
    if (attachment.uploading) uploads.track(attachment.id, attachment.filename);
    return attachment;
  }

  function encodeContent(blocks: AgentActivitySendInput["content"]) {
    const text: string[] = [];
    const attachmentIds: string[] = [];
    const references = new Map<string, AgentGuiContextReference>();
    for (const block of blocks) {
      if (block.type === "text") {
        if (block.text) {
          text.push(block.text);
          for (const mention of extractRichTextMentionsFromContent(block.text)) {
            const kind = mention.providerId === "workspace-issue" ? "issue" : mention.providerId === "agent-session" ? "session" : mention.providerId === "agent-target" ? "agent" : null;
            if (kind) {
              if (mention.scope?.workspaceId !== workspaceId) throw new MulticaAgentGuiError("The reference belongs to another workspace.", "invalid_reference", 403);
              references.set(`${kind}:${mention.entityId}`, { kind, id: mention.entityId });
            }
            if (mention.providerId !== "file") continue;
            const attachment = [...attachments.values()].find((entry) =>
              [entry.url, entry.markdown_url, entry.download_url, entry.attachment_download_url].some((url) => Boolean(url) && url === mention.entityId),
            );
            if (attachment) attachmentIds.push(attachment.id);
          }
          // Ordinary files use source-generated Markdown links, while image
          // and staged-file blocks can carry explicit asset identities.
          for (const attachment of attachments.values()) {
            if (contentReferencesAttachment(block.text, attachment)) attachmentIds.push(attachment.id);
          }
        }
        continue;
      }
      if (block.type !== "file" && block.type !== "image") unsupported(`${block.type} prompt content`);
      const id = block.attachmentId || block.assetId;
      if (!id) unsupported("Files that have not been uploaded to Multica");
      attachmentIds.push(id);
      const attachment = attachments.get(id);
      if (block.type === "image") {
        text.push(`[Image attachment: ${block.name || attachment?.filename || "Image"}]`);
        continue;
      }
      const url = attachment?.markdown_url || block.url;
      if (url) text.push(`[${block.name || attachment?.filename || "Attachment"}](${url})`);
    }
    if (!text.length && !attachmentIds.length) throw new MulticaAgentGuiError("Enter a message before sending.", "empty_message");
    return { content: text.join("\n\n"), ...(attachmentIds.length ? { attachmentIds: [...new Set(attachmentIds)] } : {}),
      ...(references.size ? { references: [...references.values()] } : {}) };
  }

  async function executeExtension(command: EngineExtensionCommand, options?: { signal?: AbortSignal }): Promise<unknown> {
    switch (command.type) {
      case "session/forkThroughTurn": {
        scope(command.workspaceId);
        const session = await client.request("session.fork", { sessionId: await backendSessionId(command.sourceAgentSessionId), turnId: command.turnId, requestId: command.requestId }, { signal: options?.signal });
        scope(command.workspaceId);
        ledger.bind(command.targetAgentSessionId, session.id);
        await persist(); replaceMetadata(session);
        const result = await loadFreshSession(command.targetAgentSessionId);
        return { operationId: session.id, requestId: command.requestId, sourceAgentSessionId: command.sourceAgentSessionId, targetAgentSessionId: command.targetAgentSessionId, turnId: command.turnId, status: "committed", session: result, error: null };
      }
      case "session/ackForkObserved": return undefined;
      case "engine/reconcileWorkspace": return loadWorkspace();
      case "session/reconcile": await loadSession(command.agentSessionId); return { status: "applied" };
      case "composerOptions/load": {
        const agent = descriptor(command.targetKey);
        const options = composerOptions(agent);
        const model = selectedModel(agent.id, command.agentSessionId) ?? agent.model ?? null;
        const catalog = await loadReasoningCatalog(agent.id);
        const reasoningEffort = selectedReasoning(agent.id, command.agentSessionId);
        const levels = localReasoningLevels(catalog.models, model);
        return { ...options, models: catalog.models.map((entry) => ({ value: entry.id, label: entry.label })), modelConfigurable: catalog.supported !== false,
          reasoningEfforts: levels.map((entry) => ({ value: entry.value, label: entry.label })), reasoningConfigurable: levels.length > 0,
          effectiveModel: model, effectiveSettings: { ...options.effectiveSettings, model, reasoningEffort } };
      }
      case "session/unactivate": return undefined;
      case "attention/readState/read": return attention;
      case "attention/readState/write": {
        const previousUnread = [...attention.completed.unreadIds, ...attention.failed.unreadIds];
        attention = {
          completed: { readIds: [...command.completed.readIds], unreadIds: [...command.completed.unreadIds] },
          failed: { readIds: [...command.failed.readIds], unreadIds: [...command.failed.unreadIds] },
        };
        await client.request("storage.write", { key: "attention-read-state-v1", value: JSON.stringify(attention) });
        const nextUnread = new Set([...attention.completed.unreadIds, ...attention.failed.unreadIds]);
        const read = new Set([...command.completed.readIds, ...command.failed.readIds, ...previousUnread.filter((id) => !nextUnread.has(id))]);
        const sessionsToMark = new Set<string>();
        for (const completionId of read) {
          const guiId = /^turn:([^:]+):[^:]+:(completed|failed)$/.exec(completionId)?.[1];
          if (guiId) sessionsToMark.add(ledger.serverId(guiId));
        }
        await Promise.all([...sessionsToMark].map((sessionId) => client.request("session.markRead", { sessionId })));
        return undefined;
      }
      default: return unsupported(command.type);
    }
  }

  function registerAttachments(values: Attachment[]) {
    for (const attachment of values) attachments.set(attachment.id, attachment);
  }

  const runtime: AgentGUIRuntime = {
    origin: "multica", projectPathIsRemote: false, conversationActivityViewEnabled: true,
    promptContentUploadSupport: { file: true, image: true }, devDiagnosticConsoleSink: false,
    getSessionEngine: (id) => { scope(id); return engine; },
    getSnapshot: (id) => { scope(id); return projectSnapshot(engine.getSnapshot()); },
    subscribe: (id, listener) => { scope(id); return engine.subscribe((state) => listener(projectSnapshot(state))); },
    subscribeSessionEvents: (id, listener) => { scope(id); eventListeners.add(listener); return () => { eventListeners.delete(listener); }; },
    load: (id) => { scope(id); return loadWorkspace(); },
    getSession: (id, sessionId) => { scope(id); return loadSession(sessionId); },
    getComposerOptions: async (input): Promise<AgentActivityComposerOptions> => {
      scope(input.workspaceId);
      return engine.loadComposerOptions({
        targetKey: input.agentTargetId,
        provider: input.provider ?? descriptor(input.agentTargetId).provider,
        agentSessionId: input.agentSessionId,
        cwd: input.cwd,
        force: input.force,
        waitForFreshModelCatalog: input.waitForFreshModelCatalog,
        settings: input.settings,
        section: input.section === "full" ? undefined : input.section,
      });
    },
    loadOlderSessionMessages: async (input) => {
      scope(input.workspaceId);
      await source.loadOlder(await backendSessionId(input.agentSessionId));
    },
    listSessionMessages: async (input) => {
      scope(input.workspaceId);
      await loadSession(input.agentSessionId);
      let messages = (projectSnapshot(engine.getSnapshot()).sessionMessagesById[input.agentSessionId] ?? []) as AgentActivityDurableMessage[];
      if (input.afterVersion !== undefined) messages = messages.filter((message) => message.version > input.afterVersion!);
      if (input.beforeVersion !== undefined) messages = messages.filter((message) => message.version < input.beforeVersion!);
      if (input.order === "desc") messages = [...messages].reverse();
      const limit = input.limit ?? messages.length;
      return { messages: messages.slice(0, limit), hasMore: messages.length > limit, latestVersion: ledger.latestVersion(input.agentSessionId) };
    },
    ensureSessionSynchronized: (input) => {
      scope(input.workspaceId);
      watched.set(input.agentSessionId, (watched.get(input.agentSessionId) ?? 0) + 1);
      syncLocalWatch();
      void loadSession(input.agentSessionId).catch(input.onError ?? reportError);
      return () => {
        const count = (watched.get(input.agentSessionId) ?? 1) - 1;
        if (count > 0) watched.set(input.agentSessionId, count); else watched.delete(input.agentSessionId);
        if (!disposed) syncLocalWatch();
      };
    },
    renameSession: (input) => engine.renameSession(input),
    setSessionPinned: (input) => engine.setSessionPinned(input),
    deleteSession: async () => unsupported("Recoverable session deletion"),
    uploadPromptContent: async (input) => {
      scope(input.workspaceId);
      const content = await Promise.all(input.content.map(async (block) => {
        if (block.type !== "file" && block.type !== "image") return block;
        if (block.attachmentId || block.assetId) return block;
        let blob: Blob;
        if (block.data) {
          const data = atob(block.data.includes(",") ? block.data.split(",").slice(1).join(",") : block.data);
          blob = new Blob([Uint8Array.from(data, (character) => character.charCodeAt(0))], { type: block.mimeType || "application/octet-stream" });
        } else if (block.url?.startsWith("blob:")) {
          blob = await fetch(block.url).then((response) => response.blob());
        } else return unsupported("Unprepared local file paths");
        const file = new File([blob], block.name || "attachment", { type: block.mimeType || blob.type });
        const attachment = await uploadAttachment(file);
        return { ...block, data: undefined, attachmentId: attachment.id, assetId: attachment.id,
          name: attachment.filename, mimeType: attachment.content_type, sizeBytes: attachment.size_bytes,
          url: block.type === "image" ? undefined : attachment.markdown_url || attachment.url, uploadStatus: "uploaded" };
      }));
      return { content };
    },
    stagePastedText: async (input) => {
      scope(input.workspaceId);
      const attachment = await uploadAttachment(new File([input.text], input.name, { type: "text/plain" }));
      return { name: attachment.filename, url: attachment.markdown_url || attachment.url, assetId: attachment.id,
        mimeType: attachment.content_type, sizeBytes: attachment.size_bytes, uploadStatus: "uploaded" };
    },
    readSessionAttachment: async (input) => {
      scope(input.workspaceId);
      return { attachmentId: input.attachmentId, ...await client.request("attachment.read", { attachmentId: input.attachmentId }) };
    },
    readSessionAttachmentSource: async (input: { workspaceId: string; attachmentId: string }) => {
      scope(input.workspaceId);
      await uploads.ensure([input.attachmentId]);
      return { attachmentId: input.attachmentId, ...await client.request("attachment.url", { attachmentId: input.attachmentId }) };
    },
    promptAttachmentsRemoved: (before: AgentComposerDraft, after: AgentComposerDraft) => {
      const kept = composerDraftAttachmentIds(after);
      uploads.remove([...composerDraftAttachmentIds(before)].filter((id) => !kept.has(id)));
    },
    readPromptAsset: async (input) => {
      scope(input.workspaceId);
      if (!input.assetId) unsupported("Unprepared prompt assets");
      const value = await client.request("attachment.read", { attachmentId: input.assetId });
      return { ...value, assetId: input.assetId, path: input.path || input.uri || `multica-attachment:${input.assetId}` };
    },
  };

  const unsubscribe = client.subscribe((event) => {
    if (event.type === "attachment.upload") { uploads.settle(event); return; }
    if (disposed) return;
    if (event.type === "init") {
      const snapshot = event.snapshot;
      if (snapshot.workspaceId !== workspaceId || snapshot.userId !== init.userId) return;
      if (snapshot.generation !== undefined && snapshot.generation < latestGeneration) return;
      latestGeneration = Math.max(latestGeneration, snapshot.generation ?? 0);
      if (snapshot.hostVisible !== undefined && snapshot.hostVisible !== hostVisible) { hostVisible = snapshot.hostVisible; syncLocalWatch(); }
      // This subscription precedes the application's selection subscription.
      receiveWorkspace(snapshot);
      return;
    }
    if (!initialized) return;
    if (event.type === "connection") {
      engine.dispatch({ type: "engine/connectionChanged", status: event.connected ? "connected" : "disconnected", workspaceId });
      return;
    }
    if (event.type !== "invalidate") return;
    if (event.generation !== undefined && event.generation < latestGeneration) return;
    latestGeneration = Math.max(latestGeneration, event.generation ?? 0);
    if (event.scope === "workspace") {
      if (workspaceLoad) workspaceDirty = true;
      else void loadWorkspace().catch(reportError);
    }
    else {
      const id = ledger.guiId(event.sessionId);
      if (watched.has(id)) {
        if (sessionLoads.has(id)) dirtySessions.add(id);
        else void loadSession(id, true).catch(reportError);
      }
    }
  });

  return {
    runtime,
    async moveQueuedToSide(agentSessionId: string, promptId: string) {
      const queue = engine.getSnapshot().promptQueue.recordsBySessionId[agentSessionId];
      const prompt = queue?.prompts.find((item) => item.id === promptId);
      if (!queue || !prompt || queue.inFlight?.promptId === promptId || queue.uncertainDelivery?.promptId === promptId) throw new Error("This queued message has already started");
      const sessionId = await backendSessionId(agentSessionId);
      const side = await client.request("host.openSide", { sessionId, requestId: `side:${sessionId}:${prompt.clientSubmitId || prompt.id}`, queued: true, queuedRequestId: prompt.clientSubmitId || prompt.id });
      await loadFreshSession(agentSessionId);
      return side;
    },
    async initialize() {
      if (initialized) return;
      ledger.hydrate(await client.request("storage.read", { key: STORAGE_KEY }));
      await modelHydration;
      await permissionHydration;
      await reasoningHydration;
      const readState = await client.request("storage.read", { key: "attention-read-state-v1" });
      if (readState) {
        const value = JSON.parse(readState);
        if (value && typeof value === "object" && [value.completed, value.failed].every((entry) => entry && Array.isArray(entry.readIds) && Array.isArray(entry.unreadIds) && [...entry.readIds, ...entry.unreadIds].every((id) => typeof id === "string"))) attention = value;
      }
      initialized = true;
      await loadWorkspace();
      const active = init.activeSessionId ? workspace.sessions.find((session) => session.id === init.activeSessionId) : undefined;
      if (active && permissionPreferences.get(active.agent_id) === undefined) {
        const saved = permissionPreferences.get(active.agent_id, active.id);
        // Retain an explicit choice made before provider defaults were remembered.
        if (saved !== undefined) await setPermissionSelection(active.agent_id, null, saved).catch(reportError);
      }
      await restorePromptQueues();
      engine.dispatch({ type: "engine/connectionChanged", status: "connected", workspaceId });
    },
    getWorkspaceState: () => workspace,
    /**
     * Whether a local conversation can be continued from the empty composer:
     * its latest turn was stopped by the user or cut off by a restart, and
     * nothing is running or queued.
     */
    continuable(agentSessionId: string) {
      if (engine.getSnapshot().promptQueue.recordsBySessionId[agentSessionId]?.prompts.length) return false;
      return localConversation(agentSessionId)?.continuable() ?? false;
    },
    /** Asks the conversation's provider to continue where its turn stopped. */
    continueSession(agentSessionId: string) {
      engine.submitPrompt({ agentSessionId, clientSubmitId: crypto.randomUUID(), content: [{ type: "text", text: init.locale === "zh-CN" ? "继续" : "Continue" }] });
    },
    subscribeTurns: (listener: () => void) => engine.subscribe(() => listener()),
    loadModelCatalog: async (agentId: string, options?: { force?: boolean; signal?: AbortSignal }) => {
      const result = await loadReasoningCatalog(agentId, options);
      modelRevision++; modelListeners.forEach((listener) => listener());
      return result;
    },
    getReasoningSelection: selectedReasoning,
    setReasoningSelection,
    getModelSelection: selectedModel,
    setModelSelection,
    getPermissionSelection: selectedPermission,
    setPermissionSelection,
    getPermissionRevision: () => permissionRevision,
    subscribePermissions: (listener: () => void) => { permissionListeners.add(listener); return () => { permissionListeners.delete(listener); }; },
    isModelSelectionBusy,
    getModelRevision: () => modelRevision,
    subscribeModels: (listener: () => void) => { modelListeners.add(listener); return () => { modelListeners.delete(listener); }; },
    subscribeWorkspace: (listener: () => void) => { workspaceListeners.add(listener); return () => { workspaceListeners.delete(listener); }; },
    toServerSessionId: resolvedServerId,
    toGuiSessionId: (id: string) => ledger.guiId(id),
    registerAttachments,
    uploadAttachment,
    subscribeUploads: uploads.subscribe,
    activeUploadCount: uploads.activeCount,
    localConversations: source,
    dispose() {
      if (disposed) return;
      disposed = true;
      unsubscribe();
      for (const pending of pendingIdentities.values()) pending.reject(new MulticaAgentGuiError("This connection has closed.", "disposed"));
      pendingIdentities.clear();
      if (saveTimer) { clearTimeout(saveTimer); saveTimer = null; void persist().catch(() => {}); }
      for (const release of openLocal.values()) release();
      openLocal.clear();
      engine.dispose();
      workspaceListeners.clear();
      modelListeners.clear();
      eventListeners.clear();
    },
  };
}

function unconfirmed() {
  return new MulticaAgentGuiError("The previous request was not confirmed. Refresh the conversation before submitting a new request.", "submission_unconfirmed");
}

function definitiveRejection(error: unknown): boolean {
  const status = error && typeof error === "object" && "status" in error ? error.status : null;
  return typeof status === "number" && status >= 400 && status < 500 && status !== 408;
}
