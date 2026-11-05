import {
ActivationService,
PromptService,
SessionLoadService,
type ActivationServiceOptions,
type PromptQueueAction,
type PromptServiceOptions,
type PromptStatus
} from "@multica/conversation-client/services";
import type { LocalPromptQueue } from "@multica/core/local-conversation/protocol";
import type {
AgentActivityMessage,
AgentQueuedPrompt,
AgentSessionActivationInput,
AgentSessionSubmitPromptInput,
PendingActivationIntentRecord
} from "@tutti-os/agent-activity-core";
import { AgentSessionsModel } from "@tutti-os/agent-gui";
import { comparer,computed,observable,reaction,runInAction,type IComputedValue } from "mobx";
import type { AgentActivationState } from "../../../../vendor/tutti/packages/agent/gui/state/conversation-services";
import type { DesktopPendingStore } from "./desktop-pending-store";
import { normalizeActivation,presentActivation } from "./presented-state";
import { promptSubmissionError } from "./prompt-submission-policy";
type SubmitInput = AgentSessionSubmitPromptInput;
type Settings = NonNullable<AgentSessionActivationInput["settings"]>;
type Activations<Result> = ActivationServiceOptions<
  AgentSessionActivationInput,
  Settings,
  Result
>;

export function createPromptRuntime<Result>(
  sessions: AgentSessionsModel,
  workspaceId: string,
  options: {
    activate: Activations<Result>["activate"];
    applyActivationResult(
      request: AgentSessionActivationInput,
      result: Result
    ): void;
    updateActivationSettings: Activations<Result>["updateSettings"];
    describeActivationError: Activations<Result>["describeError"];
    loadSession(id: string): Promise<unknown>;
    locale?(): string;
    enqueue(input: SubmitInput): Promise<LocalPromptQueue>;
    checkpoint?(input: SubmitInput, display: AgentQueuedPrompt): Promise<void>;
    checkpointActivation?(input: AgentSessionActivationInput): Promise<void>;
    update(
      id: string,
      action: PromptQueueAction,
      requestId?: string,
      targetTurnId?: string,
    ): Promise<LocalPromptQueue>;
    statuses?(
      id: string,
      requestIds: string[]
    ): Promise<Record<string, PromptStatus>>;
    error(error: unknown): void;
    pendingStore?: DesktopPendingStore;
    respondToInteraction: PromptServiceOptions<
      SubmitInput,
      AgentQueuedPrompt
    >["respondToInteraction"];
    editRetry: PromptServiceOptions<
      SubmitInput,
      AgentQueuedPrompt
    >["editRetry"];
    recoverEditRetry: PromptServiceOptions<
      SubmitInput,
      AgentQueuedPrompt
    >["recoverEditRetry"];
    reconcileWorkspace(): void;
    messages(
      id: string
    ): readonly Pick<AgentActivityMessage, "role" | "turnId" | "payload">[];
    beforeTurnOperation?(): void;
  }
) {
  const service: PromptService<SubmitInput, AgentQueuedPrompt> =
    new PromptService<SubmitInput, AgentQueuedPrompt>({
      enqueue: options.enqueue,
      checkpoint: options.checkpoint,
      canSubmit: (id) => sessions.submitAvailability(id)?.state === "available",
      admit(input, queued) {
        if (!input.content.length || !input.clientSubmitId) return null;
        const error = promptSubmissionError(input, options.locale?.() ?? "en");
        if (error) {
          options.error(new Error(error));
          return null;
        }
        if (
          (service.queueRecord(input.agentSessionId)?.prompts.length ?? 0) >=
          100
        ) {
          options.error(
            new Error(
              options.locale?.() === "zh-CN"
                ? "每个会话最多排队 100 条消息，请等待或移除部分消息后再发送。"
                : "Each conversation can queue up to 100 messages. Wait or remove a queued message before sending."
            )
          );
          return null;
        }
        return {
          ...input,
          id: input.clientSubmitId,
          createdAtUnixMs: Date.now(),
          visibleInQueue: queued
        };
      },
      update: options.update,
      error: options.error,
      statuses: options.statuses,
      workspaceId,
      turns: {
        session(id) {
          const session = sessions.session(id);
          return session
            ? {
                workspaceId: session.workspaceId,
                activeTurnId: session.activeTurnId
              }
            : null;
        },
        turn: (id, turnId) => sessions.turn(id, turnId),
        deleted: (id) => sessions.isDeleted(id),
        runtimeBlocked: (id) =>
          sessions.runtimeAvailability(id)?.state === "blocked",
        interactionStatus: (id, turnId, requestId) =>
          sessions.interaction(id, turnId, requestId)?.status ?? null,
        // The presented state, because this client's messages on their way live in this service.
        stopTarget: (id) =>
          service.latestStopTarget(id)?.clientSubmitId ?? null,
        turnForSubmit: (id, clientSubmitId) => {
          const authority=service.submission(id,clientSubmitId);
          if(authority?.state==="queued"||authority?.state==="recovery_required")return null;
          return options.messages(id).find((message)=>message.turnId&&message.payload?.clientSubmitId===clientSubmitId&&(!authority?.attempt||authority.attempt.turn_ids.includes(message.turnId)))?.turnId??null;
        }
      },
      respondToInteraction: options.respondToInteraction,
      editRetry: options.editRetry,
      recoverEditRetry: options.recoverEditRetry,
      reconcile: (input) =>
        loads.request(input.agentSessionId, {
          state: input.needsState,
          messages: input.needsMessages
        }),
      reconcileWorkspace: options.reconcileWorkspace,
      beforeTurnOperation: options.beforeTurnOperation,
      restored: (options.pendingStore?.getState().queues ?? []).flatMap(
        (queue) =>
          queue.prompts.map((prompt) => ({
            agentSessionId: queue.agentSessionId,
            input: {
              ...prompt,
              agentSessionId: queue.agentSessionId,
              clientSubmitId: prompt.clientSubmitId || prompt.id
            } as SubmitInput,
            display: { ...prompt, visibleInQueue: true } as AgentQueuedPrompt
          }))
      ),
      recoveryWarning: () =>
        options.locale?.() === "zh-CN"
          ? "已恢复未确认的排队消息。它们可能已经发送，请先检查会话记录，再决定是否重新发送。"
          : "Unconfirmed queued messages were restored. They may already have been sent. Review the conversation before sending them again.",
      save(queues) {
        options.pendingStore?.getState().setQueues(
          queues.map(({ agentSessionId, items }) => ({
            agentSessionId,
            prompts: items.map((item) => ({
              ...item.display,
              runtimeContent: item.input.runtimeContent
            }))
          }))
        );
      }
    });
  const completedActivations = observable.set<string>();
  const activations = new ActivationService<
    AgentSessionActivationInput,
    Settings,
    Result
  >({
    workspaceId,
    checkpoint: options.checkpointActivation,
    nowUnixMs: () => Date.now(),
    isDeleted: (id) => sessions.isDeleted(id),
    activate: options.activate,
    applyResult: (request, result) =>
      runInAction(() => {
        options.applyActivationResult(request, result);
        completedActivations.add(request.requestId);
      }),
    updateSettings: options.updateActivationSettings,
    reconcile: (id) => loads.request(id, { state: true, messages: false }),
    describeError: options.describeActivationError
  });
  const loads = new SessionLoadService({
    canLoad: (id) => !activations.isCreating(id) && !sessions.isDeleted(id),
    load: options.loadSession,
    describeError: options.describeActivationError
  });

  const readings = new Map<string, IComputedValue<unknown>>();
  function read<T>(area: string, id: string, derive: () => T): T {
    const key = JSON.stringify([area, id]);
    let value = readings.get(key) as IComputedValue<T> | undefined;
    if (!value) {
      value = computed(derive, {
        equals: comparer.structural,
        keepAlive: true
      });
      readings.set(key, value);
    }
    return value.get();
  }
  function presented(
    record: ReturnType<typeof activations.activations>[number]
  ): PendingActivationIntentRecord {
    void service.revision;
    const value = presentActivation(record, workspaceId);
    if (value.mode === "new" && value.status === "confirmed")
      return {
        ...value,
        initialTurnExpected: false,
        initialPromptRetracted:
          options
            .messages(value.agentSessionId)
            .some((message) => message.role === "user") ||
          // Session metadata can confirm creation before the initial prompt reaches the queue.
          (completedActivations.has(value.requestId) &&
            !service.hasPrompt(value.agentSessionId, value.clientSubmitId))
      };
    return value;
  }
  const activationState: AgentActivationState = {
    latest(rawId) {
      const id = rawId?.trim() ?? "";
      return read("latest", id, () => {
        const records = activations
          .activations()
          .filter((record) => record.request.agentSessionId === id);
        const record = records.reduce<(typeof records)[number] | null>(
          (latest, record) =>
            !latest || record.requestedAtUnixMs >= latest.requestedAtUnixMs
              ? record
              : latest,
          null
        );
        return record ? presented(record) : null;
      });
    },
    pending() {
      return read("all", "", () =>
        activations
          .activations()
          .map(presented)
          .sort(
            (a, b) =>
              a.requestedAtUnixMs - b.requestedAtUnixMs ||
              a.requestId.localeCompare(b.requestId)
          )
      );
    },
    presentation(rawId) {
      const id = rawId?.trim() ?? "";
      return read("presentation", id, () => {
        const record = activationState.latest(id);
        if (activations.inactiveSessionIds().includes(id) || !record)
          return {
            errorCode: null,
            errorMessage: null,
            requestId: null,
            status: "inactive" as const
          };
        return {
          errorCode: record.errorCode,
          errorMessage: record.errorMessage,
          requestId: record.requestId,
          status:
            record.settingsUpdateStatus === "failed" ||
            record.status === "failed"
              ? ("failed" as const)
              : record.status === "confirmed"
                ? ("active" as const)
                : record.status === "canceled"
                  ? ("inactive" as const)
                  : ("activating" as const)
        };
      });
    },
    presentations() {
      return read("presentations", "", () =>
        Object.fromEntries(
          [
            ...new Set([
              ...activations
                .activations()
                .map((record) => record.request.agentSessionId),
              ...activations.inactiveSessionIds()
            ])
          ].map((id) => [id, activationState.presentation(id)])
        )
      );
    },
    failedResolution(id, sessionExists, opts) {
      const record = activationState.latest(id);
      return opts?.selectionSource === "user-selection" ||
        record?.mode !== "new" ||
        (record.status !== "failed" && record.status !== "canceled")
        ? "not-applicable"
        : sessionExists
          ? "preserve"
          : "rollback";
    },
    saved: (requestId) => activations.saved(requestId),
    request(input) {
      options.beforeTurnOperation?.();
      const normalized = normalizeActivation(input);
      return normalized ? activations.request(normalized) : false;
    },
    patchSettings: (id, settings) => activations.patchSettings(id, settings),
    unactivate: (id) => activations.unactivate(id),
    recordFailure: (input, failure) =>
      activations.recordFailure(input, failure),
    clearFailure: (id) => activations.clearFailure(id),
    removeSession: (id) => activations.removeSession(id)
  };
  const stopLifecycle = reaction(
    () => sessions.allConsumerSessions,
    (consumers) => {
      activations.observeSessions(consumers.map(({ session }) => session));
      service.lifecycleChanged();
    }
  );
  return {
    prompts: service,
    activations: activationState,
    loads: {
      request(id: string, needs: { state: boolean; messages: boolean }) {
        options.beforeTurnOperation?.();
        loads.request(id, needs);
      },
      record: (id: string | null | undefined) => loads.record(id),
      isDetailHydrated: (id: string | null | undefined) =>
        loads.isDetailHydrated(id),
      isDetailLoading: (id: string | null | undefined) =>
        loads.isDetailLoading(id),
      canReload: (id: string | null | undefined) => {
        const record = activationState.latest(id);
        return record?.status !== "requested" && record?.status !== "uncertain";
      },
      markHydrated: (id: string) => loads.markHydrated(id),
      remove: (id: string) => loads.remove(id)
    },
    transcriptCommitted: () => service.lifecycleChanged(),
    receive: (id: string, queue?: LocalPromptQueue) =>
      service.receive(id, queue),
    dispose() {
      stopLifecycle();
      activations.dispose();
      loads.dispose();
      service.dispose();
      readings.clear();
      runInAction(() => completedActivations.clear());
    }
  };
}
