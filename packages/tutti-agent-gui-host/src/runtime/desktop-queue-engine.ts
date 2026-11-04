import type { AgentSessionEngine, EngineQueuedPrompt, PromptQueueRecord } from "@tutti-os/agent-activity-core";
import { selectEngineSubmitWouldBeVisibleInQueue } from "@tutti-os/agent-activity-core";
import type { LocalPromptQueue } from "@multica/core/local-conversation/protocol";
import type { DesktopPendingStore } from "./desktop-pending-store";
import { promptSubmissionError } from "./prompt-submission-policy";
const MAX_QUEUED_PROMPTS = 100;
type SubmitInput = Parameters<AgentSessionEngine["submitPrompt"]>[0];
interface PendingPrompt {
  input: SubmitInput;
  display: EngineQueuedPrompt;
  admission?: Promise<LocalPromptQueue>;
  confirmed?: boolean;
  restored?: boolean;
}

/** Presentation adapter only: Electron owns admission, persistence and draining. */
export function createDesktopQueueEngine(base: AgentSessionEngine, options: {
  locale?(): string;
  enqueue(input: SubmitInput): Promise<LocalPromptQueue>;
  update(id: string, action: "pause" | "stop" | "resume" | "remove" | "sendNow", requestId?: string): Promise<LocalPromptQueue>;
  error(error: unknown): void;
  pendingStore?: DesktopPendingStore;
}) {
  const queues = new Map<string, LocalPromptQueue>();
  const pending = new Map<string, PendingPrompt[]>();
  for (const queue of options.pendingStore?.getState().queues ?? []) {
    pending.set(queue.agentSessionId, queue.prompts.map((prompt) => ({
      input: { ...prompt, agentSessionId: queue.agentSessionId, clientSubmitId: prompt.clientSubmitId || prompt.id },
      display: { ...prompt, visibleInQueue: true }, restored: true,
    })));
  }
  const errors = new Map<string, { message: string; promptId?: string; origin: "admission" | "operation" }>();
  const operations = new Map<string, Promise<void>>();
  const steering = new Map<string, { sessionId: string; requestId?: string; promptId: string; accepted: boolean }>();
  const listeners = new Set<Parameters<AgentSessionEngine["subscribe"]>[0]>();
  let revision = 0;
  let cached: ReturnType<AgentSessionEngine["getSnapshot"]> | undefined;
  let lastBase: typeof cached;
  let lastRevision = -1;
  let disposed = false;
  const snapshot = () => {
    const state = base.getSnapshot();
    if (cached && state === lastBase && revision === lastRevision) return cached;
    const records = { ...state.promptQueue.recordsBySessionId };
    const submits = { ...state.pendingIntents.submitsByClientSubmitId };
    const activations = { ...state.pendingIntents.activationsByRequestId };
    for (const [requestId, activation] of Object.entries(activations)) {
      if (activation.mode !== "new" || activation.status !== "confirmed") continue;
      // Admission can fail or remain paused without starting a turn. Keep its
      // optimistic prompt until history contains it or the user removes it.
      const id = activation.agentSessionId;
      const hasUserMessage = state.sessionMessages.messagesBySessionId[id]?.some((message) => message.role === "user");
      const hasInitialPrompt = pending.get(id)?.some((item) => item.input.clientSubmitId === activation.clientSubmitId)
        || queues.get(id)?.items.some((item) => item.requestId === activation.clientSubmitId);
      activations[requestId] = { ...activation, initialTurnExpected: false, initialPromptRetracted: Boolean(hasUserMessage || !hasInitialPrompt) };
    }
    for (const id of new Set([...queues.keys(), ...pending.keys(), ...errors.keys()])) {
      const queue = queues.get(id);
      const dispatching = queue?.paused || queue?.error ? undefined : queue?.dispatchingRequestId;
      const items: EngineQueuedPrompt[] = queue?.items.map((item) => item.requestId === dispatching ? { ...item.display, visibleInQueue: false } : item.display) ?? [];
      const prompts = [...items, ...(pending.get(id) ?? []).map((item) => item.display).filter((item) => !items.some((existing) => existing.clientSubmitId === item.clientSubmitId))];
      const error = errors.get(id);
      const sending = [...steering.entries()].find(([, value]) => value.sessionId === id);
      const failureMessage = error?.message ?? queue?.error ?? null;
      for (const prompt of prompts) if (prompt.visibleInQueue === false) {
        const clientSubmitId = prompt.clientSubmitId || prompt.id;
        submits[clientSubmitId] = { agentSessionId: id, clientSubmitId, workspaceId: base.identity.workspaceId,
          content: prompt.content, status: "requested", requestedAtUnixMs: prompt.createdAtUnixMs, expiresAtUnixMs: Number.MAX_SAFE_INTEGER,
          acceptedSessionVersion: null, errorCode: null, errorMessage: null, errorReason: null, turnId: null };
      }
      records[id] = { agentSessionId: id, workspaceId: base.identity.workspaceId, prompts,
        deliveryBarrierTurnId: null, failedPromptId: failureMessage ? error?.promptId ?? prompts[0]?.id ?? null : null,
        failureMessage, inFlight: sending ? { commandId: sending[0], kind: "send", promptId: sending[1].promptId } : null,
        sendNextPromptId: sending?.[1].promptId ?? null,
        suspendReason: queue?.paused || error || pending.get(id)?.some((item) => item.restored) ? "user_stop" : null, uncertainDelivery: null,
      } satisfies PromptQueueRecord;
    }
    lastBase = state; lastRevision = revision;
    return cached = { ...state, promptQueue: { ...state.promptQueue, recordsBySessionId: records }, pendingIntents: { ...state.pendingIntents, submitsByClientSubmitId: submits, activationsByRequestId: activations } };
  };
  const publish = () => {
    if (disposed) return;
    // Checkpoint before notifying the GUI that it may clear the composer. Even
    // an in-flight admission must survive destruction of this document.
    options.pendingStore?.getState().setQueues([...pending].flatMap(([agentSessionId, items]) => items.length
      ? [{ agentSessionId, prompts: items.map((item) => ({ ...item.display, runtimeContent: item.input.runtimeContent })) }]
      : []));
    revision++; for (const listener of listeners) listener(snapshot());
  };
  const clearError = (id: string, promptId: string | undefined, origin: "admission" | "operation") => {
    const error = errors.get(id);
    if (error && error.promptId === promptId && error.origin === origin) errors.delete(id);
  };
  const fail = (id: string, error: unknown, promptId?: string, origin: "admission" | "operation" = "operation") => {
    errors.set(id, { message: error instanceof Error ? error.message : String(error), promptId, origin });
    publish(); options.error(error);
  };
  const receive = (id: string, queue?: LocalPromptQueue) => {
    if (disposed) return;
    if (!queue) return;
    // Reconcile accepted operations against the newest canonical state, even
    // when their RPC acknowledgement arrives after an invalidation snapshot.
    if (queues.has(id) && queue.revision < queues.get(id)!.revision) queue = queues.get(id)!;
    queues.set(id, queue);
    for (const [key, request] of steering) if (request.sessionId === id && (
      !queue.items.some((item) => item.requestId === request.requestId) && !(pending.get(id) ?? []).some((item) => item.input.clientSubmitId === request.requestId && !item.confirmed)
      || queue.dispatchingRequestId === request.requestId
      || request.accepted && (queue.paused || queue.error)
    )) steering.delete(key);
    pending.set(id, (pending.get(id) ?? []).filter((item) => {
      if (!queue.items.some((prompt) => prompt.requestId === item.input.clientSubmitId)) return true;
      item.confirmed = true;
      clearError(id, item.display.id, "admission");
      return false;
    }));
    publish();
  };
  const admit = (id: string, item: PendingPrompt): Promise<LocalPromptQueue> => {
    if (item.admission) return item.admission;
    item.admission = options.enqueue(item.input).then((queue) => {
      if (disposed) return queue;
      item.confirmed = true;
      pending.set(id, (pending.get(id) ?? []).filter((value) => value !== item));
      clearError(id, item.display.id, "admission");
      receive(id, queue); publish();
      return queues.get(id)!;
    }, (error: unknown) => {
      if (disposed) throw error;
      // A canonical queue snapshot can confirm admission before its RPC reply.
      if (item.confirmed) return queues.get(id)!;
      item.display = { ...item.display, visibleInQueue: true };
      fail(id, error, item.display.id, "admission");
      throw error;
    }).finally(() => { delete item.admission; });
    return item.admission;
  };
  const update = (id: string, action: "pause" | "stop" | "resume" | "remove" | "sendNow", promptId?: string) => {
    const prompt = snapshot().promptQueue.recordsBySessionId[id]?.prompts.find((item) => item.id === promptId);
    const requestId = prompt?.clientSubmitId || promptId;
    const key = JSON.stringify([id, action, requestId]);
    if (operations.has(key) || steering.has(key)) return;
    const request = action === "sendNow" && promptId ? { sessionId: id, requestId, promptId, accepted: false } : undefined;
    if (request) { steering.set(key, request); publish(); }
    const operation = (async () => {
      const item = (pending.get(id) ?? []).find((value) => value.input.clientSubmitId === requestId);
      if (action === "sendNow" && item) {
        try { await admit(id, item); } catch { return; }
      }
      if (action === "remove" && item?.admission) {
        // Wait for the original admission before removing its canonical item.
        // A rejected admission can still be discarded explicitly.
        try { await item.admission; } catch { /* Removal also handles rejection. */ }
      }
      if (disposed) return;
      // Re-admission uses the original ID. A missing or dispatching item was
      // already accepted for execution; never interrupt it or send it again.
      const current = queues.get(id);
      if (action === "sendNow" && current && (!current.items.some((value) => value.requestId === requestId) || current.dispatchingRequestId === requestId)) return;
      try {
        const queue = await options.update(id, action, requestId);
        if (disposed) return;
        if (action === "remove" && item) {
          pending.set(id, (pending.get(id) ?? []).filter((value) => value !== item));
          clearError(id, item.display.id, "admission");
        }
        clearError(id, promptId, "operation");
        if (request) request.accepted = true;
        receive(id, queue); publish();
      } catch (error) { steering.delete(key); fail(id, error, promptId); }
    })();
    operations.set(key, operation);
    void operation.finally(() => {
      operations.delete(key);
      // An acknowledgement only accepts the steer. Keep presenting it until
      // Electron reports dispatch, removal, failure or an explicit stop.
      if (request && !request.accepted) { steering.delete(key); publish(); }
    });
  };
  const unsubscribe = base.subscribe(() => { for (const listener of listeners) listener(snapshot()); });
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
      const item: PendingPrompt = { input: saved, display: { ...saved, id: saved.clientSubmitId, createdAtUnixMs: Date.now(), visibleInQueue: queued } };
      pending.set(id, [...(pending.get(id) ?? []), item]); publish();
      void admit(id, item).catch(() => {});
      return { accepted: true, queued };
    },
    dispatch(intent, dispatchOptions) {
      if ("agentSessionId" in intent && typeof intent.agentSessionId === "string") {
        const id = intent.agentSessionId;
        if (intent.type === "queue/suspended") { update(id, "pause"); return; }
        if (intent.type === "queue/resumed") { update(id, "resume"); return; }
        if (intent.type === "queue/removed") { update(id, "remove", intent.promptId); return; }
        if (intent.type === "submit/canceled") { update(id, "remove", intent.clientSubmitId); return; }
        if (intent.type === "queue/sendNowRequested") { update(id, "sendNow", intent.promptId); return; }
      }
      base.dispatch(intent, dispatchOptions);
    },
    stopSession(input) {
      // Persist the pause before the cancellation can release the next turn.
      update(input.agentSessionId, "stop");
    },
    dispose() { disposed = true; unsubscribe(); listeners.clear(); base.dispose(); },
  };
  return { engine, receive };
}
