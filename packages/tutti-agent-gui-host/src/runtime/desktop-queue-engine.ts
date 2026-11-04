import type { AgentActivityMessage, AgentSessionActivationInput, AgentSessionEngine, EngineQueuedPrompt, PendingActivationIntentRecord, PromptQueueRecord, SessionReconcileRecord } from "@tutti-os/agent-activity-core";
import { selectEngineInteraction, selectEngineSubmitWouldBeVisibleInQueue, selectEngineTurn, selectLatestStopTargetSubmitForSession } from "@tutti-os/agent-activity-core";
import { reaction } from "mobx";
import { PromptService, type PromptQueueAction, type PromptServiceOptions, type PromptStatus } from "@multica/conversation-client/services";
import { ActivationService, SessionLoadService, type ActivationServiceOptions } from "@multica/conversation-client/services";
import type { ActivationModel } from "@multica/conversation-client/models";
import type { LocalPromptQueue } from "@multica/core/local-conversation/protocol";
import type { DesktopPendingStore } from "./desktop-pending-store";
import { promptSubmissionError } from "./prompt-submission-policy";
const MAX_QUEUED_PROMPTS = 100;
type SubmitInput = Parameters<AgentSessionEngine["submitPrompt"]>[0];
type ActivationSettings = NonNullable<AgentSessionActivationInput["settings"]>;
type Activations<Result> = ActivationServiceOptions<AgentSessionActivationInput, ActivationSettings, Result>;

function normalizeActivation(input: AgentSessionActivationInput): AgentSessionActivationInput | null {
  const agentSessionId = input.agentSessionId.trim();
  const requestId = input.requestId.trim();
  const agentTargetId = input.agentTargetId?.trim() ?? "";
  const shared = { agentSessionId, requestId, title: input.title?.trim() || undefined, initialDisplayPrompt: input.initialDisplayPrompt?.trim() || undefined };
  if (input.mode === "existing") return { ...input, ...shared, agentTargetId: agentTargetId || undefined };
  const clientSubmitId = input.clientSubmitId.trim();
  if (!agentTargetId || !clientSubmitId) return null;
  return { ...input, ...shared, agentTargetId, clientSubmitId, optimisticTitle: input.optimisticTitle?.trim() || undefined };
}

function presentActivation(record: ActivationModel<AgentSessionActivationInput, ActivationSettings>, workspaceId: string): PendingActivationIntentRecord {
  const { request } = record;
  const content = request.initialContent ?? [];
  const shared = {
    agentSessionId: request.agentSessionId, requestId: request.requestId, workspaceId,
    content, cwd: request.cwd?.trim() ?? "", title: request.title ?? null,
    ...(request.initialDisplayPrompt ? { displayPrompt: request.initialDisplayPrompt } : {}),
    ...(request.railSectionKey?.trim() ? { railSectionKey: request.railSectionKey.trim() } : {}),
    ...(request.railPlacement ? { railPlacement: request.railPlacement } : {}),
    ...(record.settings ? { settings: record.settings } : {}),
    ...(record.settingsStatus ? { settingsUpdateStatus: record.settingsStatus } : {}),
    status: record.status, errorCode: record.errorCode, errorMessage: record.errorMessage,
    requestedAtUnixMs: record.requestedAtUnixMs, expiresAtUnixMs: record.expiresAtUnixMs,
    initialTurnExpected: request.initialTurnExpected ?? (request.runtimeContent ?? content).length > 0, initialPromptRetracted: false,
    commandOutcome: "pending" as const, commandSettledAtUnixMs: null, lastObservedStage: "requested" as const, snapshotObservedAtUnixMs: null, snapshotOutcome: "not_observed" as const,
  };
  return request.mode === "new"
    ? { ...shared, mode: "new", agentTargetId: request.agentTargetId, clientSubmitId: request.clientSubmitId, ...(request.optimisticTitle ? { optimisticTitle: request.optimisticTitle } : {}) }
    : { ...shared, mode: "existing", agentTargetId: request.agentTargetId ?? null, clientSubmitId: null };
}

/** Presents the prompt service to the engine, until views read it directly. */
export function createDesktopQueueEngine<ActivationResult>(base: AgentSessionEngine, options: {
  activate: Activations<ActivationResult>["activate"];
  applyActivationResult(engine: AgentSessionEngine, request: AgentSessionActivationInput, result: ActivationResult): void;
  updateActivationSettings: Activations<ActivationResult>["updateSettings"];
  describeActivationError: Activations<ActivationResult>["describeError"];
  loadSession(agentSessionId: string): Promise<unknown>;
  locale?(): string;
  enqueue(input: SubmitInput): Promise<LocalPromptQueue>;
  update(id: string, action: PromptQueueAction, requestId?: string): Promise<LocalPromptQueue>;
  statuses?(id: string, requestIds: string[]): Promise<Record<string, PromptStatus>>;
  error(error: unknown): void;
  pendingStore?: DesktopPendingStore;
  respondToInteraction: PromptServiceOptions<SubmitInput, EngineQueuedPrompt>["respondToInteraction"];
  editRetry: PromptServiceOptions<SubmitInput, EngineQueuedPrompt>["editRetry"];
  recoverEditRetry: PromptServiceOptions<SubmitInput, EngineQueuedPrompt>["recoverEditRetry"];
  reconcileWorkspace(): void;
  messages(agentSessionId: string): readonly Pick<AgentActivityMessage, "role" | "turnId" | "payload">[];
  beforeTurnOperation?(): void;
}) {
  const workspaceId = base.identity.workspaceId;
  const service = new PromptService<SubmitInput, EngineQueuedPrompt>({
    enqueue: options.enqueue,
    update: options.update,
    error: options.error,
    statuses: options.statuses,
    workspaceId,
    turns: {
      session(id) {
        const session = base.getSnapshot().sessionLifecycle.sessionsById[id];
        return session ? { workspaceId: session.workspaceId, activeTurnId: session.activeTurnId } : null;
      },
      turn: (id, turnId) => selectEngineTurn(base.getSnapshot(), id, turnId),
      deleted: (id) => base.getSnapshot().sessionLifecycle.deletedSessionIds[id] === true,
      runtimeBlocked: (id) => base.getSnapshot().sessionLifecycle.operationBySessionId[id]?.runtimeAvailability.state === "blocked",
      interactionStatus: (id, turnId, requestId) => selectEngineInteraction(base.getSnapshot(), id, turnId, requestId)?.status ?? null,
      // The presented state, because this client's messages on their way live in this service.
      stopTarget: (id) => selectLatestStopTargetSubmitForSession(snapshot(), id)?.clientSubmitId ?? null,
      turnForSubmit: (id, clientSubmitId) => options.messages(id)
        .find((message) => message.turnId && message.payload?.clientSubmitId === clientSubmitId)?.turnId ?? null,
    },
    respondToInteraction: options.respondToInteraction,
    editRetry: options.editRetry,
    recoverEditRetry: options.recoverEditRetry,
    reconcile: (input) => base.dispatch({ ...input, type: "session/reconcileRequested", workspaceId }),
    reconcileWorkspace: options.reconcileWorkspace,
    beforeTurnOperation: options.beforeTurnOperation,
    restored: (options.pendingStore?.getState().queues ?? []).flatMap((queue) => queue.prompts.map((prompt) => ({
      agentSessionId: queue.agentSessionId,
      input: { ...prompt, agentSessionId: queue.agentSessionId, clientSubmitId: prompt.clientSubmitId || prompt.id } as SubmitInput,
      display: { ...prompt, visibleInQueue: true } as EngineQueuedPrompt,
    }))),
    save(queues) {
      options.pendingStore?.getState().setQueues(queues.map(({ agentSessionId, items }) => ({
        agentSessionId,
        prompts: items.map((item) => ({ ...item.display, runtimeContent: item.input.runtimeContent })),
      })));
    },
  });
  const activations = new ActivationService<AgentSessionActivationInput, ActivationSettings, ActivationResult>({
    workspaceId,
    nowUnixMs: () => Date.now(),
    isDeleted: (id) => base.getSnapshot().sessionLifecycle.deletedSessionIds[id] === true,
    activate: options.activate,
    applyResult: (request, result) => options.applyActivationResult(engine, request, result),
    updateSettings: options.updateActivationSettings,
    reconcile: (id) => loads.request(id, { state: true, messages: false }),
    describeError: options.describeActivationError,
  });
  const loads = new SessionLoadService({
    canLoad: (id) => !activations.isCreating(id) && base.getSnapshot().sessionLifecycle.deletedSessionIds[id] !== true,
    load: options.loadSession,
    describeError: options.describeActivationError,
  });
  const listeners = new Set<Parameters<AgentSessionEngine["subscribe"]>[0]>();
  let cached: ReturnType<AgentSessionEngine["getSnapshot"]> | undefined;
  let lastBase: typeof cached;
  let lastRevision = -1;
  let lastActivationRevision = -1;
  let lastLoadRevision = -1;
  let disposed = false;
  const snapshot = () => {
    const state = base.getSnapshot();
    if (cached && state === lastBase && service.revision === lastRevision && activations.revision === lastActivationRevision && loads.revision === lastLoadRevision) return cached;
    const records = { ...state.promptQueue.recordsBySessionId };
    const submits = { ...state.pendingIntents.submitsByClientSubmitId };
    const activationsByRequestId: Record<string, PendingActivationIntentRecord> = {};
    for (const record of activations.activations()) {
      const activation = presentActivation(record, workspaceId);
      if (activation.mode === "new" && activation.status === "confirmed") {
        // Admission can fail or remain paused without starting a turn. Keep its
        // optimistic prompt until history contains it or the user removes it.
        const id = activation.agentSessionId;
        const hasUserMessage = options.messages(id).some((message) => message.role === "user");
        const hasInitialPrompt = service.hasPrompt(id, activation.clientSubmitId);
        activationsByRequestId[activation.requestId] = { ...activation, initialTurnExpected: false, initialPromptRetracted: Boolean(hasUserMessage || !hasInitialPrompt) };
      } else activationsByRequestId[activation.requestId] = activation;
    }
    const inactiveSessionIds: Record<string, true> = {};
    for (const id of activations.inactiveSessionIds()) inactiveSessionIds[id] = true;
    for (const id of service.sessionIds()) {
      const queue = service.queue(id);
      const prompts = service.prompts(id);
      const error = service.error(id);
      const sending = service.sending(id);
      const failureMessage = error?.message ?? queue?.error ?? null;
      for (const prompt of prompts) if (prompt.visibleInQueue === false) {
        const clientSubmitId = prompt.clientSubmitId || prompt.id;
        submits[clientSubmitId] = { agentSessionId: id, clientSubmitId, workspaceId: base.identity.workspaceId,
          content: prompt.content, status: "requested", requestedAtUnixMs: prompt.createdAtUnixMs, expiresAtUnixMs: Number.MAX_SAFE_INTEGER,
          acceptedSessionVersion: null, errorCode: null, errorMessage: null, errorReason: null, turnId: null };
      }
      records[id] = { agentSessionId: id, workspaceId: base.identity.workspaceId, prompts,
        deliveryBarrierTurnId: null, failedPromptId: failureMessage ? error?.promptId ?? prompts[0]?.id ?? null : null,
        failureMessage, inFlight: sending ? { commandId: sending.key, kind: "send", promptId: sending.promptId } : null,
        sendNextPromptId: sending?.promptId ?? null,
        suspendReason: queue?.paused || error || service.pending(id).some((item) => item.restored) ? "user_stop" : null, uncertainDelivery: null,
      } satisfies PromptQueueRecord;
    }
    lastBase = state; lastRevision = service.revision; lastActivationRevision = activations.revision; lastLoadRevision = loads.revision;
    const recordsBySessionId = Object.fromEntries(loads.all().map((record) => [record.agentSessionId, {
      agentSessionId: record.agentSessionId, workspaceId, appliedHistoryRevision: null, authoritativeMessagesRequired: false,
      errorCode: record.errorCode, errorMessage: record.errorMessage, inFlightCommandId: record.inFlight ? `load:${record.agentSessionId}` : null,
      inFlightLive: false, inFlightScope: record.inFlight, messageRefreshScheduled: false, messagesHydrated: record.messagesHydrated, pendingLive: false,
      pendingMessages: record.pendingMessages, pendingMessagesImmediate: record.pendingMessages, pendingState: record.pendingState, requiredHistoryRevision: null,
    } satisfies SessionReconcileRecord]));
    return cached = { ...state, sessionReconcile: { ...state.sessionReconcile, recordsBySessionId }, promptQueue: { ...state.promptQueue, recordsBySessionId: records }, pendingIntents: { ...state.pendingIntents, submitsByClientSubmitId: submits, activationsByRequestId, inactiveSessionIds } };
  };
  const notify = () => { for (const listener of listeners) listener(snapshot()); };
  const stopPresenting = reaction(() => service.revision, notify);
  const stopPresentingActivations = reaction(() => activations.revision, notify);
  const stopPresentingLoads = reaction(() => loads.revision, notify);
  const unsubscribe = base.subscribe(() => { service.lifecycleChanged(); notify(); });
  const engine: AgentSessionEngine = { ...base,
    getSnapshot: snapshot,
    activateSession(input) {
      const request = normalizeActivation(input);
      return request ? activations.request(request) : false;
    },
    subscribe(listener) { listeners.add(listener); return () => { listeners.delete(listener); }; },
    submitPrompt(input) {
      if (!input.content.length || !input.clientSubmitId) return { accepted: false, queued: false };
      const submissionError = promptSubmissionError(input, options.locale?.() ?? "en");
      if (submissionError) {
        options.error(new Error(submissionError));
        return { accepted: false, queued: false };
      }
      const id = input.agentSessionId;
      const state = snapshot();
      // Include unanswered admissions and dispatching items, which still occupy
      // canonical capacity. Reject before the GUI clears or checkpoints the draft.
      if ((state.promptQueue.recordsBySessionId[id]?.prompts.length ?? 0) >= MAX_QUEUED_PROMPTS) {
        options.error(new Error(options.locale?.() === "zh-CN"
          ? `每个会话最多排队 ${MAX_QUEUED_PROMPTS} 条消息，请等待或移除部分消息后再发送。`
          : `Each conversation can queue up to ${MAX_QUEUED_PROMPTS} messages. Wait or remove a queued message before sending.`));
        return { accepted: false, queued: false };
      }
      const queued = selectEngineSubmitWouldBeVisibleInQueue(state, id);
      const saved = structuredClone(input);
      service.submit(saved, { ...saved, id: saved.clientSubmitId, createdAtUnixMs: Date.now(), visibleInQueue: queued });
      return { accepted: true, queued };
    },
    dispatch(intent, dispatchOptions) {
      switch (intent.type) {
        case "activation/settingsPatched": activations.patchSettings(intent.agentSessionId.trim(), intent.settings); return;
        case "activation/failureRecorded":
          activations.recordFailure({ mode: "existing", agentSessionId: intent.agentSessionId, requestId: intent.requestId }, { code: intent.errorCode?.trim() || null, message: intent.errorMessage.trim() || null });
          return;
        case "activation/failureCleared": activations.clearFailure(intent.agentSessionId.trim()); return;
        case "activation/unactivateRequested": activations.unactivate(intent.agentSessionId); return;
        case "session/reconcileRequested": loads.request(intent.agentSessionId, { state: intent.needsState, messages: intent.needsMessages }); return;
      }
      if ("agentSessionId" in intent && typeof intent.agentSessionId === "string") {
        const id = intent.agentSessionId;
        if (intent.type === "queue/suspended") { service.update(id, "pause"); return; }
        if (intent.type === "queue/resumed") { service.update(id, "resume"); return; }
        if (intent.type === "queue/removed") { service.update(id, "remove", intent.promptId); return; }
        if (intent.type === "submit/canceled") { service.update(id, "remove", intent.clientSubmitId); return; }
        if (intent.type === "queue/sendNowRequested") { service.update(id, "sendNow", intent.promptId); return; }
      }
      base.dispatch(intent, dispatchOptions);
      switch (intent.type) {
        case "session/upserted": case "session/detailSnapshotReceived":
          activations.observeSessions([intent.session]); break;
        case "session/historyAuthoritativeSnapshotReceived":
          loads.markHydrated(intent.agentSessionId);
          activations.observeSessions([intent.session]); break;
        case "session/snapshotReceived": activations.observeSessions(intent.sessions); break;
        case "session/removed": activations.removeSession(intent.agentSessionId); loads.remove(intent.agentSessionId); break;
      }
    },
    dispose() { activations.dispose(); loads.dispose(); service.dispose(); stopPresenting(); stopPresentingActivations(); stopPresentingLoads(); unsubscribe(); listeners.clear(); base.dispose(); },
  };
  /** What is presented also reads the transcript, which changes without the engine's state changing. */
  const transcriptCommitted = () => { cached = undefined; service.lifecycleChanged(); notify(); };
  return { engine, activations, transcriptCommitted, prompts: service, receive: (id: string, queue?: LocalPromptQueue) => service.receive(id, queue) };
}
