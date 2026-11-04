import {
  createInitialEngineRuntimeState,
  engineRuntimeReducer
} from "./engineRuntime.reducer.ts";
import { createInitialPromptQueueState } from "./promptQueue.initialState.ts";
import {
  createInitialPendingIntentsState,
  pendingIntentsReducer
} from "./pendingIntents.reducer.ts";
import {
  createInitialSessionLifecycleState,
  sessionLifecycleReducer
} from "./sessionLifecycle.reducer.ts";
import {
  createInitialSessionReconcileState,
  sessionReconcileReducer
} from "./sessionReconcile.reducer.ts";
import type {
  RootAgentSessionEngineState,
  RootEngineIntent,
  RootEngineReducerResult
} from "./rootReducer.types.ts";
import {
  validateScopedSessionResult,
  validateSendInputResult
} from "./commandResult.validation.ts";
import { canonicalTurnKey } from "./sessionEntityKeys.ts";
import { deriveCanonicalSubmitAvailability } from "./sessionLifecycle.availability.ts";

// Root reducer: static composition of domain reducers, zero business logic.
// Cross-domain read-only context is passed explicitly; domains still own all
// decisions and state transitions in their own reducer.

export function createInitialAgentSessionEngineState(): RootAgentSessionEngineState {
  return {
    engineRuntime: createInitialEngineRuntimeState(),
    pendingIntents: createInitialPendingIntentsState(),
    promptQueue: createInitialPromptQueueState(),
    sessionReconcile: createInitialSessionReconcileState(),
    sessionLifecycle: createInitialSessionLifecycleState(),
  };
}

export function rootEngineReducer(
  state: RootAgentSessionEngineState,
  intent: RootEngineIntent
): RootEngineReducerResult<RootAgentSessionEngineState> {
  const settingsSessionId =
    intent.type === "engine/commandResult" &&
    intent.commandType === "session/updateSettings"
      ? (intent.correlationId?.trim() ?? "")
      : "";
  const settingsResultValidation =
    intent.type === "engine/commandResult" &&
    intent.commandType === "session/updateSettings" &&
    intent.outcome === "succeeded"
      ? validateScopedSessionResult(
          intent.value,
          {
            agentSessionId: settingsSessionId,
            workspaceId:
              state.sessionLifecycle.sessionsById[settingsSessionId]
                ?.workspaceId ?? ""
          },
          true
        )
      : null;
  const engineRuntime = engineRuntimeReducer(state.engineRuntime, intent);
  const sessionLifecycle = sessionLifecycleReducer(
    state.sessionLifecycle,
    intent,
    {
      settingsResultValidation
    }
  );
  const pendingIntents = pendingIntentsReducer(state.pendingIntents, intent, {
    deletedSessionIds: sessionLifecycle.state.deletedSessionIds,
    turnsById: sessionLifecycle.state.turnsById,
    settingsResultValidation
  });
  const sessionReconcile = sessionReconcileReducer(
    state.sessionReconcile,
    intent,
    {
      deletedSessionIds: state.sessionLifecycle.deletedSessionIds,
      // Once activation is uncertain, the command is no longer the owner of
      // Session visibility. Its recovery follow-up must be allowed to issue
      // the authoritative read that can discover a committed Session.
      pendingNewSessionIds: new Set(
        Object.values(pendingIntents.state.activationsByRequestId)
          .filter(
            (activation) =>
              activation.mode === "new" && activation.status === "requested"
          )
          .map((activation) => activation.agentSessionId)
      ),
      sessionsById: sessionLifecycle.state.sessionsById,
      workspaceReconcileCommandId:
        state.engineRuntime.workspaceReconcile.commandId
    }
  );
  const unchanged =
    engineRuntime.state === state.engineRuntime &&
    pendingIntents.state === state.pendingIntents &&
    sessionReconcile.state === state.sessionReconcile &&
    sessionLifecycle.state === state.sessionLifecycle;
  const nextState = unchanged
    ? state
    : {
        engineRuntime: engineRuntime.state,
        pendingIntents: pendingIntents.state,
        promptQueue: state.promptQueue,
        sessionReconcile: sessionReconcile.state,
        sessionLifecycle: sessionLifecycle.state
      };
  const followUpIntents = [
    ...(sessionReconcile.followUpIntents ?? []),
    ...(sessionLifecycle.followUpIntents ?? []),
    ...(pendingIntents.followUpIntents ?? [])
  ];
  return {
    commands: [
      ...engineRuntime.commands,
      ...pendingIntents.commands,
      ...sessionReconcile.commands,
      ...sessionLifecycle.commands
    ],
    ...(followUpIntents.length > 0 ? { followUpIntents } : {}),
    state: nextState
  };
}
