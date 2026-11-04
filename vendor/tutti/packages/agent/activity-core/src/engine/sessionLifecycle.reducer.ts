import type { EngineIntent } from "./types.ts";
import type {
  SessionLifecycleState,
  SessionRuntimeAvailability
} from "./sessionLifecycle.types.ts";
import {
  removeCanonicalSession,
  replaceCanonicalSessionHistory,
  upsertCanonicalSession,
  upsertCanonicalSessions
} from "./sessionEntities.reducer.ts";
import { setOperation } from "./sessionLifecycle.state.ts";

export function createInitialSessionLifecycleState(): SessionLifecycleState {
  return {
    deletedSessionIds: {},
    interactionsById: {},
    operationBySessionId: {},
    sessionsById: {},
    turnsById: {}
  };
}

export function sessionLifecycleReducer(
  state: SessionLifecycleState,
  intent: EngineIntent
): SessionLifecycleState {
  switch (intent.type) {
    case "session/snapshotReceived":
      return upsertCanonicalSessions(state, intent.sessions);
    case "session/upserted":
      return upsertCanonicalSession(state, intent.session);
    case "session/detailSnapshotReceived":
    case "session/historyAuthoritativeSnapshotReceived":
      return replaceCanonicalSessionHistory(state, intent.session, intent.turns);
    case "session/runtimeAvailabilityChanged":
      return changeRuntimeAvailability(
        state,
        intent.agentSessionId.trim(),
        intent.availability
      );
    case "session/removed": {
      const id = intent.agentSessionId.trim();
      return !id || state.deletedSessionIds[id]
        ? state
        : removeCanonicalSession(state, id);
    }
    case "session/restored": {
      const id = intent.agentSessionId.trim();
      if (!id || !state.deletedSessionIds[id]) return state;
      const { [id]: _restored, ...deletedSessionIds } = state.deletedSessionIds;
      return { ...state, deletedSessionIds };
    }
    default:
      return state;
  }
}

function changeRuntimeAvailability(
  state: SessionLifecycleState,
  id: string,
  availability: SessionRuntimeAvailability
): SessionLifecycleState {
  const operation = state.operationBySessionId[id];
  if (!operation) return state;
  const current = operation.runtimeAvailability;
  if (
    current.state === availability.state &&
    (current.state === "available" ||
      (availability.state === "blocked" &&
        current.reason === availability.reason))
  ) {
    return state;
  }
  return setOperation(state, id, {
    ...operation,
    runtimeAvailability: availability
  });
}
