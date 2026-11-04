import {
  createInitialEngineRuntimeState,
  engineRuntimeReducer
} from "./engineRuntime.reducer.ts";
import {
  createInitialPromptQueueState,
  promptQueueReducer
} from "./promptQueue.reducer.ts";
import {
  createInitialPromptExecutionState,
  promptExecutionReducer,
  selectPromptSettingsExecution
} from "./promptExecution.reducer.ts";
import {
  resolvePromptSendNowStrategy,
  resolveQueuedPromptSendNowStrategy
} from "./promptQueue.sendNow.ts";
import {
  canCancelQueuedSubmit,
  isQueuedSubmitDeliveryPending
} from "./promptQueue.lookup.ts";
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
import {
  createInitialSessionCommandsState,
  sessionCommandsReducer
} from "./sessionCommands.reducer.ts";
import {
  createInitialSessionMessagesState,
  sessionMessagesReducer
} from "./sessionMessages.reducer.ts";
import { canonicalTurnKey } from "./sessionEntityKeys.ts";
import { deriveCanonicalSubmitAvailability } from "./sessionLifecycle.availability.ts";
import { activeTurnIdForSession } from "./promptQueue.drainDecision.ts";

// Root reducer: static composition of domain reducers, zero business logic.
// Cross-domain read-only context is passed explicitly; domains still own all
// decisions and state transitions in their own reducer.

export function createInitialAgentSessionEngineState(): RootAgentSessionEngineState {
  return {
    engineRuntime: createInitialEngineRuntimeState(),
    pendingIntents: createInitialPendingIntentsState(),
    promptExecutions: createInitialPromptExecutionState(),
    promptQueue: createInitialPromptQueueState(),
    sessionReconcile: createInitialSessionReconcileState(),
    sessionCommands: createInitialSessionCommandsState(),
    sessionLifecycle: createInitialSessionLifecycleState(),
    sessionMessages: createInitialSessionMessagesState()
  };
}

export function rootEngineReducer(
  state: RootAgentSessionEngineState,
  intent: RootEngineIntent
): RootEngineReducerResult<RootAgentSessionEngineState> {
  if (intent.type === "prompt/executionRequested") {
    const promptExecutions = promptExecutionReducer(
      state.promptExecutions,
      intent
    );
    return {
      commands: promptExecutions.commands,
      ...(promptExecutions.followUpIntents
        ? { followUpIntents: promptExecutions.followUpIntents }
        : {}),
      state:
        promptExecutions.state === state.promptExecutions
          ? state
          : { ...state, promptExecutions: promptExecutions.state }
    };
  }
  const sendResultValidation =
    intent.type === "engine/commandResult" &&
    intent.commandType === "queue/sendPrompt" &&
    intent.outcome === "succeeded"
      ? validateSendInputResult(
          intent.value,
          state.pendingIntents.submitsByClientSubmitId[
            intent.correlationId?.trim() ?? ""
          ]
        )
      : null;
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
  const promptSettingsExecution =
    intent.type === "engine/commandResult" &&
    intent.commandType === "session/updateSettings"
      ? selectPromptSettingsExecution(state.promptExecutions, intent.commandId)
      : null;
  const engineRuntime = engineRuntimeReducer(state.engineRuntime, intent);
  const submitIntent = intent.type === "submit/requested" ? intent : null;
  const submitId = submitIntent?.clientSubmitId.trim() ?? "";
  const submitSessionId = submitIntent?.agentSessionId.trim() ?? "";
  const submitWorkspaceId = submitIntent?.workspaceId.trim() ?? "";
  const submitSession = submitSessionId
    ? state.sessionLifecycle.sessionsById[submitSessionId]
    : undefined;
  const submitSendNowStrategy =
    submitIntent?.routing === "send_now"
      ? resolvePromptSendNowStrategy(
          deriveCanonicalSubmitAvailability(
            state.sessionLifecycle,
            submitSessionId
          ),
          submitSession?.capabilities
        )
      : null;
  const queueRecord = submitIntent
    ? state.promptQueue.recordsBySessionId[submitSessionId]
    : undefined;
  // A session whose activation is still in flight is not in sessionsById yet.
  // Plain submits for it are accepted into the queue (they drain once the
  // session upserts) instead of being silently dropped; routings that dispatch
  // straight to the daemon still require the canonical session record.
  const submitSessionScopeValid =
    submitSession !== undefined
      ? submitSession.workspaceId === submitWorkspaceId
      : submitIntent !== null && submitIntent.routing !== "immediate";
  const submitRequestAccepted = Boolean(
    submitIntent &&
    submitId &&
    submitSessionId &&
    submitWorkspaceId &&
    submitSessionScopeValid &&
    submitIntent.content.length > 0 &&
    (submitIntent.routing !== "send_now" || submitSendNowStrategy !== null) &&
    !state.sessionLifecycle.deletedSessionIds[submitSessionId] &&
    !state.pendingIntents.submitsByClientSubmitId[submitId] &&
    !queueRecord?.prompts.some(
      (prompt) => prompt.id === submitId || prompt.clientSubmitId === submitId
    ) &&
    queueRecord?.inFlight?.promptId !== submitId &&
    queueRecord?.uncertainDelivery?.promptId !== submitId
  );
  const queuedSendNowTargetTurnId =
    intent.type === "queue/sendNowRequested"
      ? (intent.targetTurnId?.trim() ?? "")
      : "";
  const queuedSendNowCurrentTurnId =
    intent.type === "queue/sendNowRequested"
      ? (activeTurnIdForSession(
          state.sessionLifecycle,
          intent.agentSessionId
        ) ?? "")
      : "";
  const sendNowStrategy =
    intent.type === "submit/requested" && intent.routing === "send_now"
      ? submitSendNowStrategy
      : intent.type === "queue/sendNowRequested"
        ? queuedSendNowTargetTurnId &&
          queuedSendNowTargetTurnId !== queuedSendNowCurrentTurnId
          ? "send_available"
          : resolveQueuedPromptSendNowStrategy(
              state.promptQueue,
              intent.agentSessionId,
              intent.promptId,
              deriveCanonicalSubmitAvailability(
                state.sessionLifecycle,
                intent.agentSessionId
              ),
              state.sessionLifecycle.sessionsById[intent.agentSessionId.trim()]
                ?.capabilities
            )
        : null;
  const expiringSubmitId =
    intent.type === "engine/intentExpired" &&
    intent.expiryId.startsWith("submit:")
      ? intent.expiryId.slice("submit:".length)
      : "";
  const expiringSubmit =
    state.pendingIntents.submitsByClientSubmitId[expiringSubmitId];
  const sessionCommands = sessionCommandsReducer(
    state.sessionCommands,
    intent,
    {
      deletedSessionIds: state.sessionLifecycle.deletedSessionIds
    }
  );
  const sessionLifecycle = sessionLifecycleReducer(
    state.sessionLifecycle,
    intent,
    {
      sendResultValidation,
      settingsResultValidation
    }
  );
  const promptExecutions = promptExecutionReducer(
    state.promptExecutions,
    intent,
    { settingsResultValidation }
  );
  const promptQueue = promptQueueReducer(state.promptQueue, intent, {
    deletedSessionIds: sessionLifecycle.state.deletedSessionIds,
    lifecycle: sessionLifecycle.state,
    sendResultValidation,
    submitRequestAccepted,
    sendNowStrategy,
    settingsPreconditionPromptCommandId:
      promptSettingsExecution?.promptCommand.commandId ?? null,
    settingsResultValidation
  });
  const pendingIntents = pendingIntentsReducer(state.pendingIntents, intent, {
    deletedSessionIds: sessionLifecycle.state.deletedSessionIds,
    turnsById: sessionLifecycle.state.turnsById,
    submitCancellationAccepted:
      intent.type === "submit/canceled" &&
      canCancelQueuedSubmit(
        state.promptQueue,
        intent.agentSessionId,
        intent.clientSubmitId
      ),
    submitRequestAccepted,
    submitDeliveryIsQueuePending: Boolean(
      expiringSubmit &&
      isQueuedSubmitDeliveryPending(
        state.promptQueue,
        expiringSubmit.agentSessionId,
        expiringSubmitId
      )
    ),
    sendResultValidation,
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
  const sessionMessages = sessionMessagesReducer(
    state.sessionMessages,
    intent,
    {
      previousSessionsById: state.sessionLifecycle.sessionsById,
      sessionsById: sessionLifecycle.state.sessionsById
    }
  );
  const unchanged =
    engineRuntime.state === state.engineRuntime &&
    pendingIntents.state === state.pendingIntents &&
    promptExecutions.state === state.promptExecutions &&
    promptQueue.state === state.promptQueue &&
    sessionReconcile.state === state.sessionReconcile &&
    sessionCommands.state === state.sessionCommands &&
    sessionLifecycle.state === state.sessionLifecycle &&
    sessionMessages.state === state.sessionMessages;
  const nextState = unchanged
    ? state
    : {
        engineRuntime: engineRuntime.state,
        pendingIntents: pendingIntents.state,
        promptExecutions: promptExecutions.state,
        promptQueue: promptQueue.state,
        sessionReconcile: sessionReconcile.state,
        sessionCommands: sessionCommands.state,
        sessionLifecycle: sessionLifecycle.state,
        sessionMessages: sessionMessages.state
      };
  const followUpIntents = [
    ...(sessionReconcile.followUpIntents ?? []),
    ...(sessionLifecycle.followUpIntents ?? []),
    ...(pendingIntents.followUpIntents ?? []),
    ...(promptExecutions.followUpIntents ?? []),
    ...(promptQueue.followUpIntents ?? [])
  ];
  return {
    commands: [
      ...engineRuntime.commands,
      ...pendingIntents.commands,
      ...promptExecutions.commands,
      ...sessionReconcile.commands,
      ...sessionCommands.commands,
      ...sessionLifecycle.commands,
      ...promptQueue.commands
    ],
    ...(followUpIntents.length > 0 ? { followUpIntents } : {}),
    state: nextState
  };
}
