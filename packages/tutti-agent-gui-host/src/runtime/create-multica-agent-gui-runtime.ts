import {
TranscriptModel,
WorkspaceLoadModel
} from "@multica/conversation-client/models";
import {
CommitCadence,
ComposerService,
ConversationMutationService
} from "@multica/conversation-client/services";
import {
localReasoningLevels,
type LocalPermissionMode
} from "@multica/core/local-conversation/protocol";
import type { LocalV2Event } from "@multica/core/local-conversation/v2";
import type { Attachment } from "@multica/core/types";
import { contentReferencesAttachment } from "@multica/core/types/attachment-url";
import {
providerForkBindingAllowsAttempt,
type AgentActivityComposerOptions,
type AgentActivityDurableMessage,
type AgentActivitySendInput,
type AgentActivitySession,
type AgentActivitySnapshot,
type AgentSessionActivationInput,
type AgentSessionSubmitPromptInput
} from "@tutti-os/agent-activity-core";
import {
AgentSessionsModel,
ConversationRowsModel,
type ConversationServices
} from "@tutti-os/agent-gui";
import { extractRichTextMentionsFromContent } from "@tutti-os/ui-rich-text/core";
import { observable,reaction,runInAction } from "mobx";
import type { AgentComposerDraft } from "../../../../vendor/tutti/packages/agent/gui/agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import {
CommittedConversationMessages,
mergeConversationMessageChanges,
type ConversationMessageChanges
} from "../../../../vendor/tutti/packages/agent/gui/state/CommittedConversationMessages";
import {
createLocalCostProfile,
createLocalPresentationTiming,
createLocalSendTiming,
isLocalTimingModelOutput
} from "../local-timing";
import type {
AgentGuiContextReference,
AgentGuiExecutionTarget,
AgentGuiHostServices,
AgentGuiInitSnapshot,
AgentGuiOperations,
AgentGuiSession,
AgentGuiSessionDetail,
AgentGuiWorkspaceState,
LocalConversationSource
} from "../protocol";
import { executionTargets } from "../protocol";
import {
composerDraftAttachmentIds,
createAttachmentUploadTracker
} from "./attachment-upload-tracker";
import { createDesktopPendingStore } from "./desktop-pending-store";
import {
CONVERSATION_MIGRATION_KEY,
LegacyConversationMigration
} from "./legacy-conversation-migration";
import { createPromptRuntime } from "./prompt-runtime";
import { promptSubmissionError } from "./prompt-submission-policy";
import { snapshotPromptQueues } from "./saved-prompt-queues";
import {
composerOptions,
readLocalSessionMetadata,
readPendingSessionMetadata,
readSessionMetadata
} from "./session-metadata";

const STORAGE_KEY = "runtime-ledger-v1";

export class MulticaAgentGuiError extends Error {
  constructor(
    message: string,
    public code: string,
    public status?: number
  ) {
    super(message);
    this.name = "MulticaAgentGuiError";
  }
}

export function unsupported(capability: string): never {
  throw new MulticaAgentGuiError(
    `${capability} is not supported by this Multica connection.`,
    "unsupported"
  );
}

export function createMulticaAgentGuiRuntime(
  client: AgentGuiHostServices,
  init: AgentGuiInitSnapshot,
  source: LocalConversationSource
) {
  const uploads = createAttachmentUploadTracker(client, init.locale);
  const workspaceId = init.workspaceId;
  const presentationTiming = createLocalPresentationTiming();
  const costProfile = createLocalCostProfile({
    itemCount: (id) => source.conversation(id)?.items.size ?? 0
  });
  const sendTiming = createLocalSendTiming();
  const migration = new LegacyConversationMigration();
  const details = observable.map<string, AgentGuiSessionDetail>([], {
    deep: false
  });
  const workspaceVersion = observable.box(0);
  /** Local conversations in view: each one's release and its observer's. */
  const openLocal = new Map<string, () => void>();
  /** The turns events changed since the last apply; "all" when a change was not events. */
  const v2Changes = new Map<string, ConversationMessageChanges>();
  const transcripts = new Map<string, CommittedConversationMessages>();
  const transcriptModel = new TranscriptModel<AgentActivityDurableMessage>();
  const workspaceLoadState = new WorkspaceLoadModel();
  let isConnected = false;
  const commits = new CommitCadence<string>(commitLocal);
  let hostVisible = init.hostVisible !== false;
  const attachments = new Map<string, Attachment>();
  const sessionLoads = new Map<string, Promise<AgentActivitySession>>();
  const dirtySessions = new Set<string>();
  const watched = new Map<string, number>();
  const workspaceListeners = new Set<() => void>();
  const sendPromises = new Map<string, Promise<unknown>>();
  const pendingIdentities = new Map<
    string,
    {
      promise: Promise<string>;
      resolve(id: string): void;
      reject(error: unknown): void;
    }
  >();
  let workspace: AgentGuiWorkspaceState = {
    agents: init.agents,
    executionTargets: init.executionTargets,
    local: init.local,
    sessions: init.sessions,
    pendingTasks: init.pendingTasks,
    folderProjects: init.folderProjects
  };
  let workspaceRevision = 0;
  let workspaceLoad: Promise<AgentActivitySnapshot> | null = null;
  let workspaceDirty = false;
  let disposed = false;
  let initialized = false;
  let historyRevision = 0;
  let saveQueue = Promise.resolve();
  let latestGeneration = init.generation ?? 0;

  const persist = () => {
    const value = migration.serialize();
    const next = saveQueue
      .catch(() => {})
      .then(() =>
        client.storage.write({ key: CONVERSATION_MIGRATION_KEY, value })
      );
    saveQueue = next;
    return next;
  };
  const composer: ComposerService<AgentActivityComposerOptions> =
    new ComposerService<AgentActivityComposerOptions>(
      async (request) => {
        const agent = descriptor(request.targetKey);
        const options = composerOptions(agent);
        const model =
          composer.selectedModel(agent.id, request.agentSessionId) ??
          agent.model ??
          null;
        const catalog = await composer.loadCatalog(agent.id);
        const reasoningEffort = composer.selectedReasoning(
          agent.id,
          request.agentSessionId
        );
        const levels = localReasoningLevels(catalog.models, model);
        return {
          ...options,
          models: catalog.models.map((entry) => ({
            value: entry.id,
            label: entry.label
          })),
          modelConfigurable: catalog.supported !== false,
          reasoningEfforts: levels.map((entry) => ({
            value: entry.value,
            label: entry.label
          })),
          reasoningConfigurable: levels.length > 0,
          effectiveModel: model,
          effectiveSettings: {
            ...options.effectiveSettings,
            model,
            reasoningEffort
          }
        };
      },
      {
        storage: {
          read: (key) => client.storage.read({ key }),
          write: (key, value) => client.storage.write({ key, value })
        },
        listModels: (agentId, options) =>
          client.models.list(
            { agentId, ...(options?.force ? { force: true } : {}) },
            { signal: options?.signal }
          ),
        agent: (agentId) =>
          executionTargets(workspace).find((entry) => entry.id === agentId),
        requireAgent: descriptor,
        assertInvokable: assertInvoke,
        assertOpen: () => scope(workspaceId),
        conversation(sessionId) {
          const detail = details.get(sessionId);
          const session =
            detail?.session ??
            workspace.sessions.find(
              (entry) => entry.id === migration.canonicalId(sessionId)
            );
          if (!session) return undefined;
          return {
            agentId: session.agent_id,
            detail: detail
              ? {
                  model: detail.model,
                  reasoningEffort: detail.reasoningEffort,
                  permissionMode: detail.permissionMode,
                  untouched:
                    detail.messages.length === 0 &&
                    (detail.tasks?.length ?? 0) === 0 &&
                    !detail.pending.task_id &&
                    !detail.forkedFrom
                }
              : null
          };
        },
        storedId: (sessionId) => migration.canonicalId(sessionId),
        busy: isModelSelectionBusy,
        selectionChanged() {
          runInAction(() => workspaceVersion.set(workspaceVersion.get() + 1));
        },
        session(sessionId) {
          const session = sessionState.session(sessionId);
          return session
            ? {
                agentId: session.agentTargetId ?? null,
                settings: session.settings,
                runtimeBlocked:
                  sessionState.runtimeAvailability(sessionId)?.state ===
                  "blocked"
              }
            : null;
        }
      }
    );
  const mutations = new ConversationMutationService<AgentActivitySession>({
    // These decide against turn state, which must not be behind what has already arrived.
    session(id) {
      commits.flush();
      const session = sessionState.session(id);
      if (!session || sessionState.isDeleted(id)) return null;
      return {
        title: session.title,
        pinned: session.pinnedAtUnixMs != null,
        forkable:
          session.kind === "root" &&
          session.lifecycleCapabilities.forkThroughTurn === true
      };
    },
    deleted: (id) => sessionState.isDeleted(id),
    turnForkable(sourceId, turnId) {
      commits.flush();
      const turn = sessionState.turn(sourceId, turnId);
      return turn ? providerForkBindingAllowsAttempt(turn) : false;
    },
    current: (id) =>
      sessionState
        .snapshot(workspaceId)
        .sessions.find((entry) => entry.agentSessionId === id) ?? null,
    async rename(input, signal) {
      scope(workspaceId);
      return replaceMetadata(
        await client.session.rename(
          {
            sessionId: await backendSessionId(input.agentSessionId),
            title: input.title
          },
          { signal }
        )
      );
    },
    async setPinned(input, signal) {
      scope(workspaceId);
      return replaceMetadata(
        await client.session.pin(
          {
            sessionId: await backendSessionId(input.agentSessionId),
            pinned: input.pinned
          },
          { signal }
        )
      );
    },
    deleteSessions: async () => unsupported("Recoverable session deletion"),
    async fork(input, signal) {
      scope(workspaceId);
      const session = await client.session.fork(
        {
          sessionId: await backendSessionId(input.sourceAgentSessionId),
          turnId: input.turnId,
          requestId: input.requestId
        },
        { signal }
      );
      scope(workspaceId);
      replaceMetadata(session);
      return loadFreshSession(session.id);
    },
    removed: (id) => {
      sessionState.removeSession(id);
      desktopQueue.activations.removeSession(id);
      desktopQueue.loads.remove(id);
    }
  });
  const sessionState = new AgentSessionsModel({
    ids: () => {
      workspaceVersion.get();
      return [
        ...new Set([
          ...workspace.sessions.map((session) => session.id),
          ...details.keys()
        ])
      ];
    },
    metadata(id) {
      workspaceVersion.get();
      const detail = details.get(id);
      const raw =
        detail?.session ??
        workspace.sessions.find(
          (session) => session.id === migration.canonicalId(id)
        );
      if (!raw) return null;
      const agent = executionTargets(workspace).find(
        (entry) => entry.id === raw.agent_id
      );
      return withModel(
        detail
          ? readLocalSessionMetadata(detail, agent).session
          : readPendingSessionMetadata(
              raw,
              agent,
              workspace.pendingTasks?.find(
                (task) => task.chat_session_id === raw.id
              )
            )
      );
    },
    conversation: localConversation,
    commitVersion: (id) => transcriptModel.revision(id),
    availability(id) {
      workspaceVersion.get();
      const session =
        details.get(id)?.session ??
        workspace.sessions.find(
          (session) => session.id === migration.canonicalId(id)
        );
      const agent = executionTargets(workspace).find(
        (entry) => entry.id === session?.agent_id
      );
      return session
        ? agent?.canInvoke && agent.runtimeBound && session.status === "active"
          ? { state: "available" }
          : { state: "blocked", reason: "agent_capability_unavailable" }
        : null;
    }
  });
  const pendingStore = init.composerDraftScope
    ? createDesktopPendingStore(
        window.sessionStorage,
        JSON.stringify([init.composerDraftScope, init.userId, workspaceId])
      )
    : undefined;
  const desktopQueue = createPromptRuntime(sessionState, workspaceId, {
    activate: (request, signal) => activate(request, signal),
    applyActivationResult: () => commits.flush(),
    loadSession: (id) => loadSession(id),
    updateActivationSettings: async (id, settings) => {
      await composer.applyActivationSettings(id, settings);
    },
    describeActivationError: (error) => ({
      code:
        error instanceof Error &&
        "code" in error &&
        typeof error.code === "string"
          ? error.code.trim() || null
          : null,
      message: error instanceof Error ? error.message : String(error)
    }),
    messages: (id) => transcriptModel.messages(id),

    // A stop or an answer decides against turn state, which must not be behind what has arrived.
    beforeTurnOperation: () => commits.flush(),
    pendingStore,
    locale: () => init.locale,
    enqueue: enqueueDesktop,
    update: async (id, action, requestId) =>
      client.queue.update({
        sessionId: await backendSessionId(id),
        action,
        requestId
      }),
    statuses: async (id, requestIds) =>
      client.queue.statuses({
        sessionId: await backendSessionId(id),
        requestIds
      }),
    respondToInteraction: async (input, options) => {
      scope(input.workspaceId);
      if (input.action !== "submit" || !input.payload?.answersByQuestionId)
        unsupported("This interaction response");
      await client.interaction.respond(
        {
          sessionId: await backendSessionId(input.agentSessionId),
          turnId: input.turnId,
          requestId: input.requestId,
          answers: input.payload.answersByQuestionId as Record<
            string,
            string | string[]
          >
        },
        { signal: options.signal }
      );
      return { session: await loadFreshSession(input.agentSessionId) };
    },
    editRetry: async () => unsupported("turn/editRetry"),
    recoverEditRetry: async () => unsupported("turn/recoverEditRetry"),
    reconcileWorkspace: () => {
      void loadWorkspace().catch(reportError);
    },
    error(error) {
      reportError(error);
      if (!disposed)
        void client.hostApi
          .showMessage({
            message: error instanceof Error ? error.message : String(error),
            tone: "error"
          })
          .catch(reportError);
    }
  });
  const stopComposerReconcile = reaction(
    () => sessionState.allConsumerSessions,
    () => composer.reconcileSettings()
  );
  const unrestoredQueueSessionIds = new Set<string>();

  async function restorePromptQueues() {
    if (!migration.queuedDrafts().length) return;
    for (const queue of migration.queuedDrafts()) {
      try {
        await loadFreshSession(queue.agentSessionId);
        for (const prompt of queue.prompts) {
          const receipt = migration.receipt(prompt.clientSubmitId || prompt.id);
          if (receipt?.state === "accepted") continue;
          // Ambiguous old delivery must be inspected rather than replayed.
          if (receipt) {
            desktopQueue.prompts.restorePending([
              {
                agentSessionId: queue.agentSessionId,
                input: {
                  ...prompt,
                  agentSessionId: queue.agentSessionId,
                  clientSubmitId: prompt.clientSubmitId || prompt.id
                },
                display: { ...prompt, visibleInQueue: true }
              }
            ]);
            if (!pendingStore?.getState().durable)
              unrestoredQueueSessionIds.add(queue.agentSessionId);
            continue;
          }
          await enqueueDesktop(
            {
              ...prompt,
              agentSessionId: queue.agentSessionId,
              clientSubmitId: prompt.clientSubmitId || prompt.id
            },
            true
          );
        }
      } catch (error) {
        unrestoredQueueSessionIds.add(queue.agentSessionId);
        reportError(error);
        void client.hostApi
          .showMessage({
            message:
              init.locale === "zh-CN"
                ? "部分旧排队消息未能确认，原内容已保留。请先检查会话记录，避免重复发送。"
                : "Some saved queued messages could not be confirmed. Their contents are preserved. Check the conversation before resending.",
            tone: "warning"
          })
          .catch(reportError);
      }
    }
    migration.retainQueues(unrestoredQueueSessionIds);
    await persist();
  }

  function scope(id: string) {
    if (disposed)
      throw new MulticaAgentGuiError(
        "This Agent GUI connection has closed.",
        "disposed"
      );
    if (id !== workspaceId)
      throw new MulticaAgentGuiError(
        "The workspace changed.",
        "workspace_mismatch"
      );
  }
  function descriptor(id: string): AgentGuiExecutionTarget {
    const result = executionTargets(workspace).find((entry) => entry.id === id);
    if (!result)
      throw new MulticaAgentGuiError(
        "This agent is not available.",
        "agent_unavailable"
      );
    return result;
  }
  function resolvedServerId(guiId: string): string | null {
    const serverId = migration.canonicalId(guiId);
    return serverId !== guiId ||
      workspace.sessions.some((session) => session.id === guiId)
      ? serverId
      : null;
  }
  async function backendSessionId(guiId: string): Promise<string> {
    const id = resolvedServerId(guiId);
    if (id) return id;
    const pending = pendingIdentities.get(guiId);
    if (pending) return pending.promise;
    throw new MulticaAgentGuiError(
      "This conversation has not been created by Multica.",
      "session_not_ready"
    );
  }
  function assertInvoke(agentId: string, sessionId?: string) {
    const agent = descriptor(agentId);
    if (!agent.canInvoke) {
      throw new MulticaAgentGuiError(
        agent.unavailableReason || "You cannot invoke this agent.",
        "invocation_not_allowed",
        403
      );
    }
    if (!agent.runtimeBound)
      throw new MulticaAgentGuiError(
        "This agent has no runtime.",
        "runtime_required"
      );
    const session = sessionId
      ? (details.get(sessionId)?.session ??
        workspace.sessions.find(
          (entry) => entry.id === migration.canonicalId(sessionId)
        ))
      : undefined;
    if (session?.status === "archived") {
      throw new MulticaAgentGuiError(
        "This conversation is archived.",
        "session_archived"
      );
    }
    return agent;
  }
  function withModel(session: AgentActivitySession): AgentActivitySession {
    const agent = executionTargets(workspace).find(
      (entry) => entry.id === session.agentTargetId
    );
    return {
      ...session,
      settings: {
        ...session.settings,
        reasoningEffort: composer.selectedReasoning(
          session.agentTargetId ?? "",
          session.agentSessionId
        ),
        model:
          composer.selectedModel(
            session.agentTargetId ?? "",
            session.agentSessionId
          ) ??
          agent?.model ??
          null
      }
    };
  }
  function isModelSelectionBusy(sessionId?: string | null) {
    commits.flush();
    if (!sessionId) return false;
    const canonical = sessionState.session(sessionId);
    return Boolean(
      canonical?.activeTurnId ||
        workspace.pendingTasks?.some(
          (task) => task.chat_session_id === migration.canonicalId(sessionId)
        )
    );
  }
  function replaceMetadata(session: AgentGuiSession): AgentActivitySession {
    const id = session.id;
    const detail = details.get(id);
    if (detail) detail.session = session;
    workspace = {
      ...workspace,
      sessions: [
        ...workspace.sessions.filter((entry) => entry.id !== session.id),
        session
      ]
    };
    const value = detail
      ? readLocalSessionMetadata(
          detail,
          executionTargets(workspace).find(
            (entry) => entry.id === session.agent_id
          )
        ).session
      : readSessionMetadata(
          session,
          executionTargets(workspace).find(
            (entry) => entry.id === session.agent_id
          )
        );
    runInAction(() => workspaceVersion.set(workspaceVersion.get() + 1));
    for (const listener of workspaceListeners) listener();
    return withModel(value);
  }
  function reportError(error: unknown) {
    if (disposed) return;
    console.error("[multica-agent-gui]", error);
  }
  /** A hidden GUI, such as a closed Side, holds nothing: it reads what it missed when shown again. */
  function syncLocalWatch() {
    const wanted = new Set(
      hostVisible
        ? [...watched.keys()]
            .map(resolvedServerId)
            .filter((id): id is string => Boolean(id))
        : []
    );
    for (const [id, release] of openLocal)
      if (!wanted.has(id)) {
        release();
        openLocal.delete(id);
      }
    for (const id of wanted) {
      if (openLocal.has(id)) continue;
      const stopObserving = source.observe(id, (events) =>
        receiveLocal(id, events)
      );
      const leave = source.open(id);
      openLocal.set(id, () => {
        leave();
        stopObserving();
      });
    }
  }
  function receiveWorkspace(next: AgentGuiWorkspaceState) {
    // Projecting below reads each conversation as it is now, ahead of the engine otherwise.
    commits.flush();
    const previousPending = workspace.pendingTasks;
    workspace = next;
    workspaceRevision++;
    for (const session of workspace.sessions) {
      const detail = details.get(session.id);
      if (detail) detail.session = session;
    }
    runInAction(() => workspaceVersion.set(workspaceVersion.get() + 1));
    // A disappearing aggregate row does not prove a completion outcome.
    // Reconcile that previously live conversation against real detail once.
    if (workspace.pendingTasks !== undefined) {
      const refresh = new Set(
        (previousPending ?? [])
          .filter(
            (task) =>
              !workspace.pendingTasks!.some(
                (next) =>
                  next.task_id === task.task_id && next.status === task.status
              )
          )
          .map((task) => task.chat_session_id)
      );
      for (const task of workspace.pendingTasks) {
        const id = task.chat_session_id;
        if (
          details.has(id) &&
          !(previousPending ?? []).some(
            (previous) =>
              previous.task_id === task.task_id &&
              previous.status === task.status
          )
        )
          refresh.add(id);
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
      const next = await client.workspace.load({});
      scope(workspaceId);
      // An init received during this request already carries newer host state.
      if (revision === workspaceRevision) receiveWorkspace(next);
      return sessionState.snapshot(workspaceId);
    })().finally(() => {
      workspaceLoad = null;
      if (workspaceDirty && !disposed) {
        workspaceDirty = false;
        void loadWorkspace().catch(reportError);
      }
    });
    return workspaceLoad;
  }

  /** A connection that comes back may have missed anything, so everything is read again. */
  function receiveConnection(connected: boolean) {
    const reconnected = connected && !isConnected;
    isConnected = connected;
    if (reconnected) reconcileWorkspace();
  }

  function reconcileWorkspace() {
    if (workspaceLoadState.status === "loading") return;
    workspaceLoadState.begin();
    void loadWorkspace().then(
      () => {
        if (disposed) return;
        workspaceLoadState.succeed();
        // A running conversation's turn state cannot wait until it is opened.
        for (const session of sessionState.allConsumerSessions.map(
          ({ session }) => session
        )) {
          if (session.kind !== "root" || !session.activeTurnId?.trim())
            continue;
          desktopQueue.loads.request(session.agentSessionId, {
            messages: false,
            state: true
          });
        }
      },
      (error: unknown) => {
        if (disposed) return;
        workspaceLoadState.fail(
          error instanceof Error ? error.message.trim() || null : null
        );
      }
    );
  }

  /** The session's view; only metadata for a conversation the source no longer holds. */
  function project(guiId: string, changedTurns: ConversationMessageChanges) {
    const detail = details.get(guiId)!;
    const agent = executionTargets(workspace).find(
      (entry) => entry.id === detail.session.agent_id
    );
    const conversation = localConversation(guiId);
    if (!conversation) {
      transcripts.delete(guiId);
      transcriptModel.commit(guiId, []);
      return readLocalSessionMetadata(detail, agent);
    }
    let transcript = transcripts.get(guiId);
    if (!transcript)
      transcripts.set(
        guiId,
        (transcript = new CommittedConversationMessages(
          transcriptModel,
          detail.session.workspace_id,
          detail.session.id,
          init.locale,
          (id) => migration.clientSubmitId(id)
        ))
      );
    const messages = transcript.update(conversation, changedTurns);
    return {
      session: sessionState.presentedSession(guiId)!,
      turns: [...sessionState.turns(guiId)],
      messages
    };
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
    if (!detail)
      throw new MulticaAgentGuiError(
        "This conversation is not available.",
        "session_unavailable"
      );
    scope(detail.session.workspace_id);
    scope(workspaceId);
    return applyDetail(guiId, "all", detail);
  }

  /** A local conversation changed: new events, a page of history or its queue. */
  function receiveLocal(guiId: string, events: readonly LocalV2Event[]) {
    const conversation = localConversation(guiId);
    if (!conversation) return;
    for (const event of events) {
      if (
        event.turn_id &&
        isLocalTimingModelOutput(event.type, event.payload.kind)
      )
        presentationTiming.received(
          conversation.summary.conversation_id,
          event.turn_id
        );
    }
    v2Changes.set(
      guiId,
      mergeConversationMessageChanges(v2Changes.get(guiId), events)
    );
    commits.schedule(guiId);
    // A page of history or a queue change is not a stream: only this tick's changes can join it.
    if (!events.length) queueMicrotask(() => commits.flush());
  }

  function commitLocal(guiId: string) {
    if (disposed || !details.has(guiId)) return;
    const id = resolvedServerId(guiId);
    const detail = id ? source.detail(id) : null;
    const change = v2Changes.get(guiId);
    v2Changes.delete(guiId);
    applyDetail(guiId, change, detail ?? undefined);
  }

  async function loadSession(
    guiId: string,
    fresh = false
  ): Promise<AgentActivitySession> {
    scope(workspaceId);
    const current = sessionLoads.get(guiId);
    if (current) return current;
    const operation = loadLocalSession(guiId, fresh).finally(() => {
      sessionLoads.delete(guiId);
      if (dirtySessions.delete(guiId) && watched.has(guiId) && !disposed)
        void loadSession(guiId, true).catch(reportError);
    });
    sessionLoads.set(guiId, operation);
    return operation;
  }

  async function loadFreshSession(guiId: string) {
    await sessionLoads.get(guiId)?.catch(() => {});
    return loadSession(guiId, true);
  }

  /**
   * One commit: observers of the transcript run when it ends, by which time the
   * session model holds the same turns, so no view sees messages and turn state disagree.
   */
  function applyDetail(
    guiId: string,
    changedTurns: ConversationMessageChanges = "all",
    detail?: AgentGuiSessionDetail
  ): AgentActivitySession {
    return runInAction(() => {
      if (detail) details.set(guiId, detail);
      return commitDetail(guiId, changedTurns);
    });
  }

  function commitDetail(
    guiId: string,
    changedTurns: ConversationMessageChanges
  ): AgentActivitySession {
    const detail = details.get(guiId)!;
    desktopQueue.receive(guiId, detail.promptQueue);
    const result = costProfile.measure("projection", detail.session.id, () =>
      project(guiId, changedTurns)
    );
    costProfile.measure("dispatch", detail.session.id, () => {
      desktopQueue.loads.markHydrated(guiId);
    });
    desktopQueue.transcriptCommitted();
    const conversation = localConversation(guiId);
    for (const turn of result.turns) {
      presentationTiming.applied(
        detail.session.id,
        turn.turnId,
        (conversation?.turnItems(turn.turnId).length ?? 0) > 0,
        turn.phase === "settled"
      );
    }
    return withModel(result.session);
  }

  async function activate(
    input: AgentSessionActivationInput,
    signal?: AbortSignal
  ) {
    scope(workspaceId);
    const initialContent = input.runtimeContent ?? input.initialContent;
    if (input.mode === "existing") {
      const session = await loadSession(input.agentSessionId);
      return {
        activation: {
          mode: "existing" as const,
          status: "already_attached" as const
        },
        session,
        detail: {
          projection: "authoritative" as const,
          lifecycleCapabilitiesProjected: true,
          session,
          childSessions: [],
          turns: sessionState.turns(input.agentSessionId)
        }
      };
    }
    await composer.modelsSettled();
    const agent = assertInvoke(input.agentTargetId);
    if (input.isolation) unsupported("Local project activation");
    if (
      input.settings &&
      Object.entries(input.settings).some(
        ([key, value]) =>
          key !== "model" &&
          key !== "reasoningEffort" &&
          value != null &&
          value !== false
      )
    )
      unsupported("Non-model conversation settings");
    if (migration.creationUnknown(input.agentSessionId)) throw unconfirmed();
    const id = migration.canonicalId(input.agentSessionId);
    if (!workspace.sessions.some((session) => session.id === id)) {
      let resolveIdentity!: (id: string) => void;
      let rejectIdentity!: (error: unknown) => void;
      const identity = new Promise<string>((resolve, reject) => {
        resolveIdentity = resolve;
        rejectIdentity = reject;
      });
      void identity.catch(() => {});
      pendingIdentities.set(id, {
        promise: identity,
        resolve: resolveIdentity,
        reject: rejectIdentity
      });
      try {
        let session: AgentGuiSession;
        try {
          session = await client.session.create(
            {
              sessionId: id,
              agentId: input.agentTargetId,
              title: input.title,
              ...(input.cwd ? { cwd: input.cwd } : {})
            },
            { signal }
          );
        } catch (error) {
          const status = (error as { status?: number } | null)?.status;
          if (signal?.aborted || status === 401 || status === 403) throw error;
          // A lost create response or an explicit retry can find the same scoped
          // identity. Reading it does not submit a second creation or prompt.
          try {
            await source.ensure(id);
            const recovered = source.detail(id)?.session;
            if (
              !recovered ||
              recovered.id !== id ||
              recovered.agent_id !== input.agentTargetId
            )
              throw error;
            if (
              input.cwd &&
              recovered.localDirectory?.projectPath !== input.cwd
            )
              throw error;
            session = recovered;
          } catch {
            throw error;
          }
        }
        if (session.id !== id || session.agent_id !== input.agentTargetId)
          throw new MulticaAgentGuiError(
            "The created conversation identity did not match the request.",
            "invalid_session_identity"
          );
        await composer.seedConversation(agent, id, input.settings);
        replaceMetadata(session);
        resolveIdentity(id);
        syncLocalWatch();
      } catch (error) {
        rejectIdentity(error);
        throw error;
      } finally {
        pendingIdentities.delete(id);
      }
    }
    const session = await loadSession(input.agentSessionId);
    // Creation is complete. The queue owns admission failures and explicit
    // retries, including the first prompt's content and stable submit ID.
    if (initialContent?.length)
      desktopQueue.prompts.submitPrompt({
        agentSessionId: input.agentSessionId,
        clientSubmitId: input.clientSubmitId,
        content: initialContent,
        displayPrompt: input.initialDisplayPrompt
      });
    return {
      activation: { mode: "new" as const, status: "attached" as const },
      session
    };
  }

  const desktopAdmissions = new WeakMap<
    AgentSessionSubmitPromptInput,
    AgentGuiOperations["queue.enqueue"]["params"]
  >();
  async function enqueueDesktop(
    input: AgentSessionSubmitPromptInput,
    paused = false
  ) {
    const startedAt = Date.now();
    const previous = desktopAdmissions.get(input);
    if (previous) {
      await uploads.ensure(previous.item.attachmentIds ?? []);
      const retry = () => client.queue.enqueue(previous);
      const result = await sendTiming
        .begin(previous.sessionId, input.clientSubmitId, startedAt)
        .measure("client.request", retry);
      uploads.forget(previous.item.attachmentIds ?? []);
      desktopQueue.receive(input.agentSessionId, result);
      return result;
    }
    const sessionId = await backendSessionId(input.agentSessionId);
    const session =
      details.get(input.agentSessionId)?.session ??
      workspace.sessions.find((item) => item.id === sessionId);
    if (!session) throw new Error("Conversation is not loaded");
    const submissionError = promptSubmissionError(input, init.locale);
    if (submissionError)
      throw new MulticaAgentGuiError(submissionError, "unsupported");
    const encoded = encodeContent([...(input.runtimeContent ?? input.content)]);
    // The prompt waits here in the visible pending queue, with its failure and retry.
    await uploads.ensure(encoded.attachmentIds ?? []);
    const display = snapshotPromptQueues(
      {
        nextCommandSequence: 0,
        recordsBySessionId: {
          [input.agentSessionId]: {
            agentSessionId: input.agentSessionId,
            workspaceId,
            prompts: [
              {
                ...input,
                id: input.clientSubmitId,
                createdAtUnixMs: Date.now()
              }
            ],
            deliveryBarrierTurnId: null,
            failedPromptId: null,
            failureMessage: null,
            inFlight: null,
            sendNextPromptId: null,
            suspendReason: null,
            uncertainDelivery: null
          }
        }
      },
      [...attachments.values()]
    )[0]!.prompts[0]!;
    // Restoring a saved queue is not the user sending anything.
    const timing = !paused
      ? sendTiming.begin(sessionId, input.clientSubmitId, startedAt)
      : undefined;
    const reasoningEffort = await composer.reasoningForSend(
      session.agent_id,
      input.agentSessionId
    );
    const params: AgentGuiOperations["queue.enqueue"]["params"] = {
      sessionId,
      paused,
      queued: true,
      item: {
        ...encoded,
        requestId: input.clientSubmitId,
        display,
        ...(composer.selectedModel(session.agent_id, input.agentSessionId)
          ? {
              model: composer.selectedModel(
                session.agent_id,
                input.agentSessionId
              )!
            }
          : {}),
        reasoningEffort,
        permissionMode: composer.selectedPermission(
          session.agent_id,
          input.agentSessionId
        )
      }
    };
    // Keep the admitted payload stable even if preferences change before an
    // explicit retry of a failed or lost acknowledgement.
    desktopAdmissions.set(input, params);
    const enqueue = () => client.queue.enqueue(params);
    const result = await (timing
      ? timing.measure("client.request", enqueue)
      : enqueue());
    uploads.forget(encoded.attachmentIds ?? []);
    desktopQueue.receive(input.agentSessionId, result);
    return result;
  }

  async function uploadAttachment(file: File) {
    const attachment = await client.attachment.upload({
      file,
      background: true
    });
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
          for (const mention of extractRichTextMentionsFromContent(
            block.text
          )) {
            const kind =
              mention.providerId === "workspace-issue"
                ? "issue"
                : mention.providerId === "agent-session"
                  ? "session"
                  : mention.providerId === "agent-target"
                    ? "agent"
                    : null;
            if (kind) {
              if (mention.scope?.workspaceId !== workspaceId)
                throw new MulticaAgentGuiError(
                  "The reference belongs to another workspace.",
                  "invalid_reference",
                  403
                );
              references.set(`${kind}:${mention.entityId}`, {
                kind,
                id: mention.entityId
              });
            }
            if (mention.providerId !== "file") continue;
            const attachment = [...attachments.values()].find((entry) =>
              [
                entry.url,
                entry.markdown_url,
                entry.download_url,
                entry.attachment_download_url
              ].some((url) => Boolean(url) && url === mention.entityId)
            );
            if (attachment) attachmentIds.push(attachment.id);
          }
          // Ordinary files use source-generated Markdown links, while image
          // and staged-file blocks can carry explicit asset identities.
          for (const attachment of attachments.values()) {
            if (contentReferencesAttachment(block.text, attachment))
              attachmentIds.push(attachment.id);
          }
        }
        continue;
      }
      if (block.type !== "file" && block.type !== "image")
        unsupported(`${block.type} prompt content`);
      const id = block.attachmentId || block.assetId;
      if (!id) unsupported("Files that have not been uploaded to Multica");
      attachmentIds.push(id);
      const attachment = attachments.get(id);
      if (block.type === "image") {
        text.push(
          `[Image attachment: ${block.name || attachment?.filename || "Image"}]`
        );
        continue;
      }
      const url = attachment?.markdown_url || block.url;
      if (url)
        text.push(
          `[${block.name || attachment?.filename || "Attachment"}](${url})`
        );
    }
    if (!text.length && !attachmentIds.length)
      throw new MulticaAgentGuiError(
        "Enter a message before sending.",
        "empty_message"
      );
    return {
      content: text.join("\n\n"),
      ...(attachmentIds.length
        ? { attachmentIds: [...new Set(attachmentIds)] }
        : {}),
      ...(references.size ? { references: [...references.values()] } : {})
    };
  }

  function registerAttachments(values: Attachment[]) {
    for (const attachment of values) attachments.set(attachment.id, attachment);
  }

  const rowModels = new Map<string, ConversationRowsModel>();
  const runtime: ConversationServices = {
    conversations: {
      refreshWorkspace: async (id) => {
        scope(id);
        await loadWorkspace();
      },
      load: (id, sessionId) => {
        scope(id);
        return loadSession(sessionId);
      },
      loadOlder: async (input) => {
        scope(input.workspaceId);
        await source.loadOlder(await backendSessionId(input.agentSessionId));
      },
      watch: (input) => {
        scope(input.workspaceId);
        watched.set(
          input.agentSessionId,
          (watched.get(input.agentSessionId) ?? 0) + 1
        );
        syncLocalWatch();
        void loadSession(input.agentSessionId).catch(
          input.onError ?? reportError
        );
        return () => {
          const count = (watched.get(input.agentSessionId) ?? 1) - 1;
          if (count > 0) watched.set(input.agentSessionId, count);
          else watched.delete(input.agentSessionId);
          if (!disposed) syncLocalWatch();
        };
      },
      loadAll: async (input) => {
        scope(input.workspaceId);
        await loadSession(input.agentSessionId);
        const conversation = localConversation(input.agentSessionId);
        for (let page = 0; conversation?.hasOlderTurns; page++) {
          if (page >= 1000)
            throw new Error("Conversation history exceeded the safety limit.");
          const before = conversation.turns.size;
          await source.loadOlder(await backendSessionId(input.agentSessionId));
          const detail = source.detail(
            await backendSessionId(input.agentSessionId)
          );
          applyDetail(input.agentSessionId, "all", detail ?? undefined);
          if (conversation.hasOlderTurns && conversation.turns.size <= before)
            throw new Error("Conversation history did not advance.");
        }
        commits.flush();
        return transcriptModel.messages(input.agentSessionId);
      }
    },
    attachments: {
      promptContentUploadSupport: { file: true, image: true },
      uploadPromptContent: async (input) => {
        scope(input.workspaceId);
        const content = await Promise.all(
          input.content.map(async (block) => {
            if (block.type !== "file" && block.type !== "image") return block;
            if (block.attachmentId || block.assetId) return block;
            let blob: Blob;
            if (block.data) {
              const data = atob(
                block.data.includes(",")
                  ? block.data.split(",").slice(1).join(",")
                  : block.data
              );
              blob = new Blob(
                [Uint8Array.from(data, (character) => character.charCodeAt(0))],
                { type: block.mimeType || "application/octet-stream" }
              );
            } else if (block.url?.startsWith("blob:")) {
              blob = await fetch(block.url).then((response) => response.blob());
            } else return unsupported("Unprepared local file paths");
            const file = new File([blob], block.name || "attachment", {
              type: block.mimeType || blob.type
            });
            const attachment = await uploadAttachment(file);
            return {
              ...block,
              data: undefined,
              attachmentId: attachment.id,
              assetId: attachment.id,
              name: attachment.filename,
              mimeType: attachment.content_type,
              sizeBytes: attachment.size_bytes,
              url:
                block.type === "image"
                  ? undefined
                  : attachment.markdown_url || attachment.url,
              uploadStatus: "uploaded"
            };
          })
        );
        return { content };
      },
      stagePastedText: async (input) => {
        scope(input.workspaceId);
        const attachment = await uploadAttachment(
          new File([input.text], input.name, { type: "text/plain" })
        );
        return {
          name: attachment.filename,
          url: attachment.markdown_url || attachment.url,
          assetId: attachment.id,
          mimeType: attachment.content_type,
          sizeBytes: attachment.size_bytes,
          uploadStatus: "uploaded"
        };
      },
      readSessionAttachment: async (input) => {
        scope(input.workspaceId);
        return {
          attachmentId: input.attachmentId,
          ...(await client.attachment.read({
            attachmentId: input.attachmentId
          }))
        };
      },
      readSessionAttachmentSource: async (input: {
        workspaceId: string;
        attachmentId: string;
      }) => {
        scope(input.workspaceId);
        await uploads.ensure([input.attachmentId]);
        return {
          attachmentId: input.attachmentId,
          ...(await client.attachment.url({ attachmentId: input.attachmentId }))
        };
      },
      promptAttachmentsRemoved: (
        before: AgentComposerDraft,
        after: AgentComposerDraft
      ) => {
        const kept = composerDraftAttachmentIds(after);
        uploads.remove(
          [...composerDraftAttachmentIds(before)].filter((id) => !kept.has(id))
        );
      },
      readPromptAsset: async (input) => {
        scope(input.workspaceId);
        if (!input.assetId) unsupported("Unprepared prompt assets");
        const value = await client.attachment.read({
          attachmentId: input.assetId
        });
        return {
          ...value,
          assetId: input.assetId,
          path: input.path || input.uri || `multica-attachment:${input.assetId}`
        };
      }
    },
    conversationRows(id, options = { workspaceId, userId: init.userId }) {
      const key = id;
      let model = rowModels.get(key);
      if (!model) {
        model = new ConversationRowsModel(runtime, id, options);
        rowModels.set(key, model);
      }
      return model;
    },
    sessions: sessionState,
    activations: desktopQueue.activations,
    sessionLoads: desktopQueue.loads,
    environment: { origin: "multica", projectPathIsRemote: false },
    diagnostics: { devDiagnosticConsoleSink: false },
    composer,
    workspaceLoad: workspaceLoadState,
    transcript: {
      messages: (id) => transcriptModel.messages(id),
      turnIds: (id) => transcriptModel.turnIds(id),
      turnMessages: (id, turnId) => transcriptModel.turnMessages(id, turnId),
      hasOlderMessages: (id) => {
        transcriptModel.revision(id);
        return localConversation(id)?.hasOlderTurns ?? false;
      },
      conversationIds: () => transcriptModel.conversationIds()
    },
    mutations,
    prompts: desktopQueue.prompts
  };

  const unsubscribe = client.subscribe((event) => {
    if (event.type === "attachment.upload") {
      uploads.settle(event);
      return;
    }
    if (disposed) return;
    if (event.type === "init") {
      const snapshot = event.snapshot;
      if (
        snapshot.workspaceId !== workspaceId ||
        snapshot.userId !== init.userId
      )
        return;
      if (
        snapshot.generation !== undefined &&
        snapshot.generation < latestGeneration
      )
        return;
      latestGeneration = Math.max(latestGeneration, snapshot.generation ?? 0);
      if (
        snapshot.hostVisible !== undefined &&
        snapshot.hostVisible !== hostVisible
      ) {
        hostVisible = snapshot.hostVisible;
        syncLocalWatch();
      }
      // This subscription precedes the application's selection subscription.
      receiveWorkspace(snapshot);
      return;
    }
    if (!initialized) return;
    if (event.type === "connection") {
      receiveConnection(event.connected);
      return;
    }
    if (event.type !== "invalidate") return;
    if (event.generation !== undefined && event.generation < latestGeneration)
      return;
    latestGeneration = Math.max(latestGeneration, event.generation ?? 0);
    if (event.scope === "workspace") {
      if (workspaceLoad) workspaceDirty = true;
      else void loadWorkspace().catch(reportError);
    } else {
      const id = event.sessionId;
      if (watched.has(id)) {
        if (sessionLoads.has(id)) dirtySessions.add(id);
        else void loadSession(id, true).catch(reportError);
      }
    }
  });

  return {
    runtime,
    async moveQueuedToSide(agentSessionId: string, promptId: string) {
      const queue = desktopQueue.prompts.queueRecord(agentSessionId);
      const prompt = queue?.prompts.find((item) => item.id === promptId);
      if (!queue || !prompt || queue.inFlight?.promptId === promptId)
        throw new Error("This queued message has already started");
      const sessionId = await backendSessionId(agentSessionId);
      const side = await client.hostApi.openSide({
        sessionId,
        requestId: `side:${sessionId}:${prompt.clientSubmitId || prompt.id}`,
        queued: true,
        queuedRequestId: prompt.clientSubmitId || prompt.id
      });
      await loadFreshSession(agentSessionId);
      return side;
    },
    async initialize() {
      if (initialized) return;
      const checkpoint = await client.storage.read({
        key: CONVERSATION_MIGRATION_KEY
      });
      migration.hydrate(
        checkpoint ? null : await client.storage.read({ key: STORAGE_KEY }),
        checkpoint
      );
      if (migration.needsCheckpoint) await persist();
      desktopQueue.prompts.migrateRestoredSessions((id) =>
        migration.canonicalId(id)
      );
      await composer.hydrated();
      initialized = true;
      await loadWorkspace();
      const active = init.activeSessionId
        ? workspace.sessions.find(
            (session) => session.id === init.activeSessionId
          )
        : undefined;
      if (active) {
        const saved = composer.permissionWithoutDefault(
          active.agent_id,
          active.id
        );
        // Retain an explicit choice made before provider defaults were remembered.
        if (saved !== undefined)
          await composer
            .setPermissionSelection(active.agent_id, null, saved)
            .catch(reportError);
      }
      await restorePromptQueues();
      receiveConnection(true);
    },
    getWorkspaceState: () => workspace,
    /**
     * Whether a local conversation can be continued from the empty composer:
     * its latest turn was stopped by the user or cut off by a restart, and
     * nothing is running or queued.
     */
    continuable(agentSessionId: string) {
      if (desktopQueue.prompts.queueRecord(agentSessionId)?.prompts.length)
        return false;
      return localConversation(agentSessionId)?.continuable() ?? false;
    },
    /** Asks the conversation's provider to continue where its turn stopped. */
    continueSession(agentSessionId: string) {
      desktopQueue.prompts.submitPrompt({
        agentSessionId,
        clientSubmitId: crypto.randomUUID(),
        content: [
          { type: "text", text: init.locale === "zh-CN" ? "继续" : "Continue" }
        ]
      });
    },
    subscribeTurns: (listener: () => void) =>
      reaction(
        () => [
          sessionState.snapshot(workspaceId),
          desktopQueue.prompts.revision
        ],
        () => listener()
      ),
    loadModelCatalog: (
      agentId: string,
      options?: { force?: boolean; signal?: AbortSignal }
    ) => composer.refreshCatalog(agentId, options),
    getReasoningSelection: (agentId: string, sessionId?: string | null) =>
      composer.selectedReasoning(agentId, sessionId),
    setReasoningSelection: (
      agentId: string,
      sessionId: string | null | undefined,
      effort: string | null
    ) => composer.setReasoningSelection(agentId, sessionId, effort),
    getModelSelection: (agentId: string, sessionId?: string | null) =>
      composer.selectedModel(agentId, sessionId),
    setModelSelection: (
      agentId: string,
      sessionId: string | null | undefined,
      model: string | null
    ) => composer.setModelSelection(agentId, sessionId, model),
    getPermissionSelection: (agentId: string, sessionId?: string | null) =>
      composer.selectedPermission(agentId, sessionId),
    setPermissionSelection: (
      agentId: string,
      sessionId: string | null | undefined,
      mode: LocalPermissionMode
    ) => composer.setPermissionSelection(agentId, sessionId, mode),
    getPermissionRevision: () => composer.permissionRevision,
    subscribePermissions: (listener: () => void) =>
      reaction(() => composer.permissionRevision, listener),
    isModelSelectionBusy,
    getModelRevision: () => composer.modelRevision,
    subscribeModels: (listener: () => void) =>
      reaction(() => composer.modelRevision, listener),
    subscribeWorkspace: (listener: () => void) => {
      workspaceListeners.add(listener);
      return () => {
        workspaceListeners.delete(listener);
      };
    },
    toServerSessionId: resolvedServerId,
    legacySessionAliases: () => migration.aliases(),
    toGuiSessionId: (id: string) => migration.canonicalId(id),
    registerAttachments,
    uploadAttachment,
    subscribeUploads: uploads.subscribe,
    activeUploadCount: uploads.activeCount,
    /** A React commit of this GUI, for the cost profile. */
    rendered: costProfile.rendered,
    localConversations: source,
    dispose() {
      if (disposed) return;
      disposed = true;
      commits.dispose();
      costProfile.dispose();
      unsubscribe();
      for (const pending of pendingIdentities.values())
        pending.reject(
          new MulticaAgentGuiError("This connection has closed.", "disposed")
        );
      pendingIdentities.clear();
      for (const release of openLocal.values()) release();
      openLocal.clear();
      composer.dispose();
      mutations.dispose();
      stopComposerReconcile();
      desktopQueue.dispose();
      workspaceListeners.clear();
      rowModels.clear();
      transcripts.clear();
    }
  };
}

function unconfirmed() {
  return new MulticaAgentGuiError(
    "The previous request was not confirmed. Refresh the conversation before submitting a new request.",
    "submission_unconfirmed"
  );
}

function definitiveRejection(error: unknown): boolean {
  const status =
    error && typeof error === "object" && "status" in error
      ? error.status
      : null;
  return (
    typeof status === "number" &&
    status >= 400 &&
    status < 500 &&
    status !== 408
  );
}
