import type { AgentSessionEngine, EngineQueuedPrompt, PromptQueueRecord } from "@tutti-os/agent-activity-core";
import { selectEngineInteraction, selectEngineSubmitWouldBeVisibleInQueue, selectEngineTurn, selectLatestStopTargetSubmitForSession } from "@tutti-os/agent-activity-core";
import { reaction } from "mobx";
import { PromptService, type PromptQueueAction, type PromptServiceOptions, type PromptStatus } from "@multica/conversation-client/services";
import type { LocalPromptQueue } from "@multica/core/local-conversation/protocol";
import type { DesktopPendingStore } from "./desktop-pending-store";
import { promptSubmissionError } from "./prompt-submission-policy";
const MAX_QUEUED_PROMPTS = 100;
type SubmitInput = Parameters<AgentSessionEngine["submitPrompt"]>[0];
/** Presents the prompt service to the engine, until views read it directly. */
export function createDesktopQueueEngine(base: AgentSessionEngine, options: {
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
      turnForSubmit: (id, clientSubmitId) => base.getSnapshot().sessionMessages.messagesBySessionId[id]
        ?.find((message) => message.turnId && message.payload?.clientSubmitId === clientSubmitId)?.turnId ?? null,
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
  const listeners = new Set<Parameters<AgentSessionEngine["subscribe"]>[0]>();
  let cached: ReturnType<AgentSessionEngine["getSnapshot"]> | undefined;
  let lastBase: typeof cached;
  let lastRevision = -1;
  let disposed = false;
  const snapshot = () => {
    const state = base.getSnapshot();
    if (cached && state === lastBase && service.revision === lastRevision) return cached;
    const records = { ...state.promptQueue.recordsBySessionId };
    const submits = { ...state.pendingIntents.submitsByClientSubmitId };
    const activations = { ...state.pendingIntents.activationsByRequestId };
    for (const [requestId, activation] of Object.entries(activations)) {
      if (activation.mode !== "new" || activation.status !== "confirmed") continue;
      // Admission can fail or remain paused without starting a turn. Keep its
      // optimistic prompt until history contains it or the user removes it.
      const id = activation.agentSessionId;
      const hasUserMessage = state.sessionMessages.messagesBySessionId[id]?.some((message) => message.role === "user");
      const hasInitialPrompt = service.hasPrompt(id, activation.clientSubmitId);
      activations[requestId] = { ...activation, initialTurnExpected: false, initialPromptRetracted: Boolean(hasUserMessage || !hasInitialPrompt) };
    }
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
    lastBase = state; lastRevision = service.revision;
    return cached = { ...state, promptQueue: { ...state.promptQueue, recordsBySessionId: records }, pendingIntents: { ...state.pendingIntents, submitsByClientSubmitId: submits, activationsByRequestId: activations } };
  };
  const notify = () => { for (const listener of listeners) listener(snapshot()); };
  const stopPresenting = reaction(() => service.revision, notify);
  const unsubscribe = base.subscribe(() => { service.lifecycleChanged(); notify(); });
  const engine: AgentSessionEngine = { ...base,
    getSnapshot: snapshot,
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
      if ("agentSessionId" in intent && typeof intent.agentSessionId === "string") {
        const id = intent.agentSessionId;
        if (intent.type === "queue/suspended") { service.update(id, "pause"); return; }
        if (intent.type === "queue/resumed") { service.update(id, "resume"); return; }
        if (intent.type === "queue/removed") { service.update(id, "remove", intent.promptId); return; }
        if (intent.type === "submit/canceled") { service.update(id, "remove", intent.clientSubmitId); return; }
        if (intent.type === "queue/sendNowRequested") { service.update(id, "sendNow", intent.promptId); return; }
      }
      base.dispatch(intent, dispatchOptions);
    },
    dispose() { service.dispose(); stopPresenting(); unsubscribe(); listeners.clear(); base.dispose(); },
  };
  return { engine, prompts: service, receive: (id: string, queue?: LocalPromptQueue) => service.receive(id, queue) };
}
