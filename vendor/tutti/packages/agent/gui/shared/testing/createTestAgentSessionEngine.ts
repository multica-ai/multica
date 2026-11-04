import {
  AGENT_SESSION_ENGINE_LOCAL_ORIGIN,
  createAgentSessionEngine,
  type AgentSessionEngine,
  type EngineIntent,
  type PendingActivationIntentRecord
} from "@tutti-os/agent-activity-core";

type Activation = PendingActivationIntentRecord;

function requestedActivation(
  input: Record<string, unknown> & { agentSessionId: string; requestId: string },
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
    initialTurnExpected: input.initialTurnExpected ?? Boolean((content as unknown[] | undefined)?.length),
    lastObservedStage: "requested",
    requestedAtUnixMs: input.requestedAtUnixMs ?? Date.now(),
    snapshotObservedAtUnixMs: null,
    snapshotOutcome: "not_observed",
    status: "requested",
    title: input.title ?? null,
    workspaceId
  } as unknown as Activation;
}

/**
 * An engine presenting activations the way a host's activation service does, with
 * the outcomes a spec reports: a failed command, or the session turning up.
 */
export function createTestAgentSessionEngine(
  workspaceId = "test-workspace"
): AgentSessionEngine {
  const base = createAgentSessionEngine({
    identity: { origin: AGENT_SESSION_ENGINE_LOCAL_ORIGIN, workspaceId }
  });
  const listeners = new Set<Parameters<AgentSessionEngine["subscribe"]>[0]>();
  let activations: Readonly<Record<string, Activation>> = {};
  let cachedBase: ReturnType<AgentSessionEngine["getSnapshot"]> | null = null;
  let cached: ReturnType<AgentSessionEngine["getSnapshot"]> | null = null;
  const getSnapshot = () => {
    const state = base.getSnapshot();
    if (cached && cachedBase === state) return cached;
    cachedBase = state;
    cached = {
      ...state,
      pendingIntents: { ...state.pendingIntents, activationsByRequestId: activations }
    };
    return cached;
  };
  const notify = () => {
    for (const listener of listeners) listener(getSnapshot());
  };
  const write = (next: Readonly<Record<string, Activation>>) => {
    activations = next;
    cached = null;
    notify();
  };
  const request = (record: Activation) => {
    const kept = Object.values(activations).filter(
      (item) => item.agentSessionId !== record.agentSessionId || item.status === "failed"
    );
    write(Object.fromEntries([...kept, record].map((item) => [item.requestId, item])));
  };
  const confirm = (sessionIds: readonly string[]) => {
    const updated = Object.values(activations).map((item) =>
      sessionIds.includes(item.agentSessionId) &&
      (item.status === "requested" || item.status === "uncertain")
        ? { ...item, status: "confirmed" as const }
        : item
    );
    if (updated.some((item) => item !== activations[item.requestId])) {
      write(Object.fromEntries(updated.map((item) => [item.requestId, item])));
    }
  };
  base.subscribe(notify);
  return {
    ...base,
    getSnapshot,
    subscribe(listener) {
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    },
    activateSession(input) {
      if (!input.agentSessionId.trim() || activations[input.requestId]) return false;
      request(requestedActivation({ ...input }, workspaceId, input.initialContent));
      return true;
    },
    dispatch(intent: EngineIntent) {
      switch (intent.type) {
        case "activation/requested":
          request(requestedActivation({ ...intent }, workspaceId, intent.content));
          return;
        case "engine/commandResult": {
          const record = activations[intent.correlationId ?? ""];
          if (intent.commandType === "session/activate" && record && intent.outcome === "failed") {
            write({ ...activations, [record.requestId]: { ...record, status: "failed", errorCode: intent.errorCode ?? null, errorMessage: intent.errorMessage ?? null } });
          }
          return;
        }
      }
      base.dispatch(intent);
      if (intent.type === "session/upserted" || intent.type === "session/detailSnapshotReceived" || intent.type === "session/historyAuthoritativeSnapshotReceived") {
        confirm([intent.session.agentSessionId]);
      } else if (intent.type === "session/snapshotReceived") {
        confirm(intent.sessions.map((session) => session.agentSessionId));
      }
    }
  };
}
