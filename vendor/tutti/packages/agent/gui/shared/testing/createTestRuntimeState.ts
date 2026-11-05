import {
  PromptService,
  SessionLoadService
} from "@multica/conversation-client/services";
import type {
  LocalPromptQueue,
  LocalQueuedPrompt
} from "@multica/core/local-conversation/protocol";
import type {
  AgentActivitySessionInput,
  AgentQueuedPrompt,
  AgentSessionSubmitPromptInput,
  PendingActivationIntentRecord,
  SessionRuntimeAvailability
} from "@tutti-os/agent-activity-core";
import {
  comparer,
  computed,
  observable,
  runInAction,
  type IComputedValue
} from "mobx";
import type {
  AgentActivationState,
  AgentConversationServices
} from "../../state/conversation-services";
import { TestAgentSessionsModel as AgentSessionsModel } from "./TestAgentSessionsModel";
type Activation = PendingActivationIntentRecord;

function requestedActivation(
  input: Record<string, unknown> & {
    agentSessionId: string;
    requestId: string;
  },
  workspaceId: string,
  content: unknown
): Activation {
  return {
    ...input,
    commandOutcome: "pending",
    commandSettledAtUnixMs: null,
    content: content ?? [],
    cwd: input.cwd ?? "",
    displayPrompt: input.initialDisplayPrompt,
    errorCode: null,
    errorMessage: null,
    expiresAtUnixMs: input.expiresAtUnixMs ?? Number.MAX_SAFE_INTEGER,
    initialPromptRetracted: false,
    initialTurnExpected:
      input.initialTurnExpected ??
      Boolean((content as unknown[] | undefined)?.length),
    lastObservedStage: "requested",
    requestedAtUnixMs: input.requestedAtUnixMs ?? Date.now(),
    snapshotObservedAtUnixMs: null,
    snapshotOutcome: "not_observed",
    status: "requested",
    title: input.title ?? null,
    workspaceId
  } as unknown as Activation;
}

type TestReport =
  | ({
      type: "activation/requested";
      agentSessionId: string;
      requestId: string;
      content?: unknown;
    } & Record<string, unknown>)
  | ({
      type: "engine/commandResult";
      correlationId?: string;
      outcome: string;
      errorCode?: string;
      errorMessage?: string;
    } & Record<string, unknown>)
  | { type: "session/upserted"; session: AgentActivitySessionInput }
  | {
      type: "session/snapshotReceived";
      sessions: readonly AgentActivitySessionInput[];
    }
  | {
      type: "session/runtimeAvailabilityChanged";
      agentSessionId: string;
      availability: SessionRuntimeAvailability;
    };

/** Real observable session state and prompt operations, with controlled activation reports. */
export function createTestRuntimeState(workspaceId = "test-workspace") {
  const sessions = new AgentSessionsModel();
  const records = observable.map<string, Activation>([], { deep: false });
  const inactive = observable.set<string>();
  const cached = new Map<string, IComputedValue<unknown>>();
  const queues = new Map<string, LocalPromptQueue>();
  let revision = 0;
  function read<T>(key: string, fn: () => T): T {
    let value = cached.get(key) as IComputedValue<T> | undefined;
    if (!value) {
      value = computed(fn, { keepAlive: true, equals: comparer.structural });
      cached.set(key, value);
    }
    return value.get();
  }
  const pending = () =>
    read("activations", () =>
      [...records.values()].map((record) =>
        sessions.session(record.agentSessionId) &&
        (record.status === "requested" || record.status === "uncertain")
          ? { ...record, status: "confirmed" as const }
          : record
      )
    );
  const activations: AgentActivationState = {
    pending,
    latest(id) {
      return read(`latest:${id}`, () =>
        pending()
          .filter((record) => record.agentSessionId === id)
          .reduce<Activation | null>(
            (latest, record) =>
              !latest || record.requestedAtUnixMs >= latest.requestedAtUnixMs
                ? record
                : latest,
            null
          )
      );
    },
    presentation(id) {
      return read(`presentation:${id}`, () => {
        const record = activations.latest(id);
        return {
          errorCode: record?.errorCode ?? null,
          errorMessage: record?.errorMessage ?? null,
          requestId: record?.requestId ?? null,
          status:
            inactive.has(id ?? "") || !record || record.status === "canceled"
              ? ("inactive" as const)
              : record.status === "confirmed"
                ? ("active" as const)
                : record.status === "failed" ||
                    record.settingsUpdateStatus === "failed"
                  ? ("failed" as const)
                  : ("activating" as const)
        };
      });
    },
    presentations() {
      return read("presentations", () =>
        Object.fromEntries(
          [
            ...new Set([
              ...pending().map((record) => record.agentSessionId),
              ...inactive
            ])
          ].map((id) => [id, activations.presentation(id)])
        )
      );
    },
    failedResolution(id, exists, options) {
      const activation = activations.latest(id);
      return options?.selectionSource === "user-selection" ||
        activation?.mode !== "new" ||
        (activation.status !== "failed" && activation.status !== "canceled")
        ? "not-applicable"
        : exists
          ? "preserve"
          : "rollback";
    },
    request(input) {
      if (!input.agentSessionId.trim() || records.has(input.requestId))
        return false;
      runInAction(() => {
        for (const record of records.values())
          if (
            record.agentSessionId === input.agentSessionId &&
            record.status !== "failed"
          )
            records.delete(record.requestId);
        records.set(
          input.requestId,
          requestedActivation({ ...input }, workspaceId, input.initialContent)
        );
        inactive.delete(input.agentSessionId);
      });
      return true;
    },
    patchSettings(id, settings) {
      runInAction(() => {
        const record = activations.latest(id);
        if (record)
          records.set(record.requestId, {
            ...record,
            settings: { ...record.settings, ...settings }
          });
      });
    },
    unactivate(id) {
      activations.removeSession(id);
      runInAction(() => inactive.add(id));
    },
    recordFailure(input, failure) {
      runInAction(() =>
        records.set(input.requestId, {
          ...requestedActivation(
            { ...input },
            workspaceId,
            input.initialContent
          ),
          status: "failed",
          errorCode: failure.code,
          errorMessage: failure.message
        })
      );
    },
    clearFailure(id) {
      runInAction(() => {
        for (const record of records.values())
          if (record.agentSessionId === id && record.status === "failed")
            records.delete(record.requestId);
      });
    },
    removeSession(id) {
      runInAction(() => {
        for (const record of records.values())
          if (record.agentSessionId === id) records.delete(record.requestId);
      });
    }
  };
  const loads = new SessionLoadService({
    canLoad: (id) => !sessions.isDeleted(id),
    load: async () => {},
    describeError: () => ({ code: null, message: null })
  });
  const prompts: PromptService<
    AgentSessionSubmitPromptInput,
    AgentQueuedPrompt
  > = new PromptService({
    workspaceId,
    turns: {
      session: (id) => sessions.session(id),
      turn: (id, turnId) => sessions.turn(id, turnId),
      deleted: (id) => sessions.isDeleted(id),
      runtimeBlocked: (id) =>
        sessions.runtimeAvailability(id)?.state === "blocked",
      interactionStatus: (id, turnId, requestId) =>
        sessions.interaction(id, turnId, requestId)?.status ?? null,
      stopTarget: (id) => prompts.latestStopTarget(id)?.clientSubmitId ?? null,
      turnForSubmit: () => null
    },
    canSubmit: (id) => sessions.submitAvailability(id)?.state === "available",
    admit: (input, queued) => ({
      ...input,
      id: input.clientSubmitId,
      createdAtUnixMs: Date.now(),
      visibleInQueue: queued
    }),
    enqueue: () => new Promise(() => {}),
    update: async (id, action, requestId) => {
      const current = queues.get(id) ?? {
        revision: 0,
        paused: false,
        items: []
      };
      const next = {
        ...current,
        revision: ++revision,
        items:
          action === "remove"
            ? current.items.filter((item) => item.requestId !== requestId)
            : current.items
      };
      queues.set(id, next);
      prompts.receive(id, next);
      return next;
    },
    error: () => {},
    respondToInteraction: async () => ({}),
    editRetry: async () => ({}),
    recoverEditRetry: async () => ({}),
    reconcile: () => {},
    reconcileWorkspace: () => {}
  });
  const state: AgentConversationServices = {
    sessions,
    prompts,
    activations,
    sessionLoads: {
      request: (id, needs) => loads.request(id, needs),
      record: (id) => loads.record(id),
      isDetailHydrated: (id) => loads.isDetailHydrated(id),
      isDetailLoading: (id) => loads.isDetailLoading(id),
      canReload: (id) => {
        const record = activations.latest(id);
        return record?.status !== "requested" && record?.status !== "uncertain";
      },
      markHydrated: (id) => loads.markHydrated(id),
      remove: (id) => loads.remove(id)
    }
  };
  return {
    ...state,
    seedQueue(id: string, displays: readonly AgentQueuedPrompt[]) {
      const queue: LocalPromptQueue = {
        revision: ++revision,
        paused: false,
        items: displays.map(
          (display): LocalQueuedPrompt => ({
            requestId: display.clientSubmitId || display.id,
            content: "",
            display
          })
        )
      };
      queues.set(id, queue);
      prompts.receive(id, queue);
    },
    report(input: TestReport) {
      runInAction(() => {
        switch (input.type) {
          case "activation/requested":
            records.set(
              input.requestId,
              requestedActivation(input, workspaceId, input.content)
            );
            break;
          case "engine/commandResult": {
            const record = records.get(input.correlationId ?? "");
            if (record && input.outcome === "failed")
              records.set(record.requestId, {
                ...record,
                status: "failed",
                errorCode: input.errorCode ?? null,
                errorMessage: input.errorMessage ?? null
              });
            break;
          }
          case "session/upserted":
            sessions.upsertSession(input.session);
            break;
          case "session/snapshotReceived":
            sessions.upsertSessions(input.sessions);
            break;
          case "session/runtimeAvailabilityChanged":
            sessions.setRuntimeAvailability(
              input.agentSessionId,
              input.availability
            );
            break;
        }
      });
    },
    dispose() {
      prompts.dispose();
      loads.dispose();
      cached.clear();
    }
  };
}
