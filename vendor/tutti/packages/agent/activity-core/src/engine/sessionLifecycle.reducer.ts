import type { SendInputResultValidation } from "./commandResult.validation.ts";
import type { ScopedSessionResultValidation } from "./commandResult.validation.ts";
import type {
  EngineCommand,
  EngineIntent,
  EngineReducerResult
} from "./types.ts";
import {
  type SessionLifecycleState,
  type SessionOperationState
} from "./sessionLifecycle.types.ts";
import {
  patchCanonicalSessionMetadata,
  removeCanonicalSession,
  replaceCanonicalSessionSnapshot,
  upsertCanonicalInteraction,
  upsertCanonicalSession,
  upsertCanonicalTurn,
  upsertCanonicalTurnProjection
} from "./sessionEntities.reducer.ts";
import { replaceAuthoritativeSessionHistory } from "./sessionLifecycle.authoritativeHistory.ts";
import { initialOperation, setOperation } from "./sessionLifecycle.state.ts";
const NO_COMMANDS: readonly EngineCommand[] = [];

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
  intent: EngineIntent,
  context: {
    settingsResultValidation?: ScopedSessionResultValidation | null;
  } = {}
): EngineReducerResult<SessionLifecycleState> {
  switch (intent.type) {
    case "session/snapshotReceived":
      return result(replaceCanonicalSessionSnapshot(
          state,
          intent.sessions,
          initialOperation
        ));
    case "session/upserted":
      return result(upsertCanonicalSession(state, intent.session, initialOperation));
    case "session/historyAuthoritativeSnapshotReceived": {
      const next = replaceAuthoritativeSessionHistory(
        state,
        intent,
        initialOperation
      );
      return result(next);
    }
    case "session/metadataPatched":
      return result(
        patchCanonicalSessionMetadata(
          state,
          intent.agentSessionId,
          intent.patch
        )
      );
    case "session/runtimeAvailabilityChanged":
      return changeRuntimeAvailability(
        state,
        intent.agentSessionId,
        intent.availability
      );
    case "session/runtimeActivityChanged":
      return changeRuntimeActivity(
        state,
        intent.agentSessionId,
        intent.state,
        intent.occurredAtUnixMs
      );
    case "turn/upserted":
      return result(upsertCanonicalTurn(state, intent.turn));
    case "turn/projectionReceived":
      return result(upsertCanonicalTurnProjection(state, intent));
    case "interaction/upserted":
      return result(upsertCanonicalInteraction(state, intent.interaction));
    case "session/removed":
      return removeSession(state, intent.agentSessionId);
    case "session/restored":
      return restoreSession(state, intent.agentSessionId);
    case "session/errorRecorded":
      return updateOperation(state, intent.agentSessionId, (operation) => ({
        ...operation,
        operationError: intent.errorMessage.trim() || operation.operationError
      }));
    case "session/errorCleared":
      return updateOperation(state, intent.agentSessionId, (operation) => ({
        ...operation,
        operationError: null
      }));
    case "engine/commandResult":
      if (
        intent.commandType === "session/updateSettings" &&
        context.settingsResultValidation?.kind === "valid"
      )
        return {
          commands: [],
          state: upsertCanonicalSession(
            state,
            context.settingsResultValidation.session,
            initialOperation
          )
        };
      return unchanged(state);
    default:
      return unchanged(state);
  }
}

function changeRuntimeActivity(
  state: SessionLifecycleState,
  rawAgentSessionId: string,
  runtimeActivity: SessionOperationState["runtimeActivity"],
  occurredAtUnixMs: number
): EngineReducerResult<SessionLifecycleState> {
  const agentSessionId = rawAgentSessionId.trim();
  if (!agentSessionId) return unchanged(state);
  const operation =
    state.operationBySessionId[agentSessionId] ?? initialOperation();
  if (occurredAtUnixMs <= 0) {
    if (
      operation.runtimeActivity === "idle" &&
      operation.runtimeActivityOccurredAtUnixMs === 0
    ) {
      return unchanged(state);
    }
    return result(
      setOperation(state, agentSessionId, {
        ...operation,
        runtimeActivity: "idle",
        runtimeActivityOccurredAtUnixMs: 0
      })
    );
  }
  if (
    occurredAtUnixMs < operation.runtimeActivityOccurredAtUnixMs ||
    (occurredAtUnixMs === operation.runtimeActivityOccurredAtUnixMs &&
      operation.runtimeActivity === "idle" &&
      runtimeActivity === "running") ||
    (occurredAtUnixMs === operation.runtimeActivityOccurredAtUnixMs &&
      operation.runtimeActivity === runtimeActivity)
  ) {
    return unchanged(state);
  }
  return result(
    setOperation(state, agentSessionId, {
      ...operation,
      runtimeActivity,
      runtimeActivityOccurredAtUnixMs: occurredAtUnixMs
    })
  );
}

function changeRuntimeAvailability(
  state: SessionLifecycleState,
  rawAgentSessionId: string,
  availability: SessionOperationState["runtimeAvailability"]
): EngineReducerResult<SessionLifecycleState> {
  const agentSessionId = rawAgentSessionId.trim();
  const operation = state.operationBySessionId[agentSessionId];
  if (!agentSessionId || !operation) return unchanged(state);
  const current = operation.runtimeAvailability;
  if (runtimeAvailabilityEquals(current, availability)) {
    return unchanged(state);
  }
  return updateOperation(state, agentSessionId, (value) => ({
    ...value,
    runtimeAvailability: availability
  }));
}

function runtimeAvailabilityEquals(
  left: SessionOperationState["runtimeAvailability"],
  right: SessionOperationState["runtimeAvailability"]
): boolean {
  if (left.state !== right.state) return false;
  if (left.state === "available" || right.state === "available") return true;
  if (left.reason !== right.reason) return false;
  if (
    left.reason === "agent_sharing_revoked" &&
    right.reason === "agent_sharing_revoked"
  ) {
    return left.ownerLabel === right.ownerLabel;
  }
  return true;
}

function removeSession(
  state: SessionLifecycleState,
  rawId: string
): EngineReducerResult<SessionLifecycleState> {
  const id = rawId.trim();
  if (!id || state.deletedSessionIds[id]) return unchanged(state);
  const removed = removeCanonicalSession(state, id);
  return result({
    ...removed,
    deletedSessionIds: { ...removed.deletedSessionIds, [id]: true }
  });
}

function restoreSession(
  state: SessionLifecycleState,
  rawId: string
): EngineReducerResult<SessionLifecycleState> {
  const id = rawId.trim();
  if (!id || !state.deletedSessionIds[id]) return unchanged(state);
  const deletedSessionIds = { ...state.deletedSessionIds };
  delete deletedSessionIds[id];
  return result({ ...state, deletedSessionIds });
}

function updateOperation(
  state: SessionLifecycleState,
  id: string,
  update: (value: SessionOperationState) => SessionOperationState
): EngineReducerResult<SessionLifecycleState> {
  const current = state.operationBySessionId[id.trim()];
  return current
    ? result(setOperation(state, id.trim(), update(current)))
    : unchanged(state);
}
function result(
  state: SessionLifecycleState
): EngineReducerResult<SessionLifecycleState> {
  return { commands: NO_COMMANDS, state };
}
function unchanged(
  state: SessionLifecycleState
): EngineReducerResult<SessionLifecycleState> {
  return { commands: NO_COMMANDS, state };
}
