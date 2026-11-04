import { normalizeAgentActivityCapabilityReferences } from "../capabilityReferences.ts";
import type { SendInputResultValidation } from "./commandResult.validation.ts";
import type { ScopedSessionResultValidation } from "./commandResult.validation.ts";
import {
  isPendingActivationViable,
  type PendingActivationIntentRecord,
  type PendingIntentsState,
  type SessionActivationRequestedIntent
} from "./pendingIntents.types.ts";
import { reconcilePendingIntentsFromAuthoritativeHistory } from "./pendingIntents.authoritativeHistory.ts";
import {
  pendingActivationRailSectionKeyFields
} from "./pendingIntents.activationExtras.ts";
import {
  attachPendingActivationSettings,
  settlePendingActivationSettings
} from "./pendingIntents.activationSettings.ts";
import {
  deleteActivation,
  replaceActivation
} from "./pendingIntents.activationRecords.ts";
import {
  confirmActivationsFromSessions,
  createActivationRecoveryIntent,
  receiveSessionSnapshot,
  settleActivationCommand
} from "./pendingIntents.activationSettlement.ts";
import {
  markSessionActive,
  markSessionInactive,
  removeInactiveSession,
  unchanged
} from "./pendingIntents.inactiveSessions.ts";
import {
  confirmFromMessages,
  confirmFromSessions,
  deleteSubmit,
  expireSubmit,
  removeSubmit,
  requestSubmit,
  settleSubmitCommand,
  submitExpiryId
} from "./pendingSubmit.reducer.ts";
import type {
  EngineCommand,
  EngineCommandResultIntent,
  EngineIntent,
  EngineReducerResult
} from "./types.ts";

const NO_COMMANDS: readonly EngineCommand[] = [];
const EXISTING_SESSION_ACTIVATION_COMMAND_TIMEOUT_MS = 30_000;
// A new activation includes process spawn and ACP initialize before session/new.
// Keep the outer command alive long enough for session/new to receive its own
// full 30-second protocol timeout instead of inheriting a partially spent UI
// deadline.
const NEW_SESSION_ACTIVATION_COMMAND_TIMEOUT_MS = 90_000;

export function createInitialPendingIntentsState(): PendingIntentsState {
  return {
    activationsByRequestId: {},
    inactiveSessionIds: {},
    submitsByClientSubmitId: {}
  };
}

export function pendingIntentsReducer(
  state: PendingIntentsState,
  intent: EngineIntent,
  context: {
    deletedSessionIds: Readonly<Record<string, true>>;
    turnsById: Readonly<
      Record<string, import("../types.ts").AgentActivityTurn>
    >;
    submitCancellationAccepted?: boolean;
    sendResultValidation?: SendInputResultValidation | null;
    settingsResultValidation?: ScopedSessionResultValidation | null;
    submitRequestAccepted?: boolean;
    submitDeliveryIsQueuePending?: boolean;
  } = {
    deletedSessionIds: {},
    turnsById: {}
  }
): EngineReducerResult<PendingIntentsState> {
  switch (intent.type) {
    case "activation/requested":
      if (context.deletedSessionIds[intent.agentSessionId.trim()]) {
        return unchanged(state);
      }
      return requestActivation(state, intent);
    case "activation/dismissed":
      return removeActivation(state, intent.requestId);
    case "activation/settingsPatched":
      return patchActivationSettings(state, intent);
    case "activation/failureRecorded":
      return recordActivationFailure(state, intent);
    case "activation/failureCleared":
      return clearActivationFailure(state, intent.agentSessionId);
    case "activation/unactivateRequested":
      return requestUnactivation(state, intent);
    case "session/stopRequested":
      return stopPendingActivation(state, intent);
    case "submit/requested":
      if (context.submitRequestAccepted === false) return unchanged(state);
      if (context.deletedSessionIds[intent.agentSessionId.trim()]) {
        return unchanged(state);
      }
      return requestSubmit(state, intent);
    case "submit/dismissed":
      return removeSubmit(state, intent.clientSubmitId);
    case "submit/canceled":
      return context.submitCancellationAccepted === true
        ? removeSubmit(state, intent.clientSubmitId)
        : unchanged(state);
    case "message/snapshotReceived":
      return confirmFromMessages(state, intent.messages);
    case "session/historyAuthoritativeSnapshotReceived": {
      const confirmed = confirmFromMessages(state, intent.messages);
      const reconciled = reconcilePendingIntentsFromAuthoritativeHistory(
        confirmed.state,
        intent
      );
      return {
        commands: [...confirmed.commands, ...reconciled.commands],
        state: reconciled.state
      };
    }
    case "session/snapshotReceived":
      return receiveSessionSnapshot(
        state,
        intent.sessions,
        context.turnsById,
        intent.observedAtUnixMs,
        intent.workspaceMismatchSessionIds
      );
    case "session/upserted":
      return confirmActivationsFromSessions(
        state,
        [intent.session],
        false,
        intent.observedAtUnixMs
      );
    case "turn/projectionReceived":
    case "turn/upserted":
      return confirmFromSessions(state, context.turnsById);
    case "engine/commandResult":
      if (intent.commandType === "queue/sendPrompt") {
        const settled = settleSubmitCommand(
          state,
          intent,
          context.sendResultValidation ?? null
        );
        if (intent.outcome !== "succeeded") return settled;
        const confirmed = confirmFromSessions(settled.state, context.turnsById);
        return {
          commands: [...settled.commands, ...confirmed.commands],
          ...(confirmed.followUpIntents?.length
            ? { followUpIntents: confirmed.followUpIntents }
            : {}),
          state: confirmed.state
        };
      }
      return intent.commandType === "session/activate"
        ? settleActivationCommand(state, intent)
        : intent.commandType === "session/updateSettings"
          ? settleActivationSettingsCommand(
              state,
              intent,
              context.settingsResultValidation ?? null
            )
          : unchanged(state);
    case "engine/intentExpired":
      return intent.expiryId.startsWith("activation:")
        ? expireActivation(state, intent.expiryId)
        : expireSubmit(state, intent.expiryId, {
            deliveryIsQueuePending:
              context.submitDeliveryIsQueuePending === true,
            dueAtUnixMs: intent.dueAtUnixMs
          });
    case "session/removed":
      return removeSessionIntents(state, intent.agentSessionId);
    default:
      return unchanged(state);
  }
}

function patchActivationSettings(
  state: PendingIntentsState,
  intent: Extract<EngineIntent, { type: "activation/settingsPatched" }>
): EngineReducerResult<PendingIntentsState> {
  const agentSessionId = intent.agentSessionId.trim();
  const record = Object.values(state.activationsByRequestId)
    .filter(
      (candidate) =>
        candidate.agentSessionId === agentSessionId &&
        candidate.mode === "new" &&
        isPendingActivationViable(candidate)
    )
    .sort((left, right) => right.requestedAtUnixMs - left.requestedAtUnixMs)[0];
  if (!record) return unchanged(state);
  const patchedRecord: PendingActivationIntentRecord = {
    ...record,
    pendingSettingsPatch: {
      ...(record.pendingSettingsPatch ?? {}),
      ...intent.settings
    },
    settings: { ...(record.settings ?? {}), ...intent.settings },
    settingsUpdateStatus: undefined
  };
  if (record.status === "confirmed") {
    const attached = attachPendingActivationSettings(patchedRecord);
    return {
      commands: attached.commands,
      state: replaceActivation(state, attached.record)
    };
  }
  return {
    commands: NO_COMMANDS,
    state: replaceActivation(state, patchedRecord)
  };
}

function requestActivation(
  state: PendingIntentsState,
  intent: SessionActivationRequestedIntent
): EngineReducerResult<PendingIntentsState> {
  const requestId = intent.requestId.trim();
  const agentSessionId = intent.agentSessionId.trim();
  const workspaceId = intent.workspaceId.trim();
  const agentTargetId = intent.agentTargetId?.trim() || null;
  const clientSubmitId =
    intent.mode === "new" ? intent.clientSubmitId.trim() : null;
  if (
    !requestId ||
    !agentSessionId ||
    !workspaceId ||
    state.activationsByRequestId[requestId] ||
    (intent.mode === "new" && (!agentTargetId || !clientSubmitId))
  ) {
    return unchanged(state);
  }
  const content = (intent.content ?? []).map((block) => ({ ...block }));
  const displayPrompt = intent.initialDisplayPrompt?.trim() || undefined;
  const optimisticTitle =
    intent.mode === "new"
      ? intent.optimisticTitle?.trim() || undefined
      : undefined;
  const runtimeContent = (intent.runtimeContent ?? content).map((block) => ({
    ...block
  }));
  const capabilityRefs = normalizeAgentActivityCapabilityReferences(
    intent.capabilityRefs
  );
  const supersededRequestIds = Object.values(state.activationsByRequestId)
    .filter(
      (record) =>
        record.agentSessionId === agentSessionId && record.status !== "failed"
    )
    .map((record) => record.requestId);
  const baseState = supersededRequestIds.reduce(deleteActivation, state);
  const recordBase = {
    agentSessionId,
    ...(capabilityRefs.length > 0 ? { capabilityRefs } : {}),
    content,
    cwd: intent.cwd?.trim() ?? "",
    commandOutcome: "pending" as const,
    commandSettledAtUnixMs: null,
    ...(displayPrompt ? { displayPrompt } : {}),
    errorCode: null,
    errorMessage: null,
    expiresAtUnixMs: intent.expiresAtUnixMs,
    initialPromptRetracted: false,
    initialTurnExpected:
      intent.initialTurnExpected ?? runtimeContent.length > 0,
    lastObservedStage: "requested" as const,
    ...(intent.isolation ? { isolation: intent.isolation } : {}),
    ...(intent.modelExplicit !== undefined
      ? { modelExplicit: intent.modelExplicit }
      : {}),
    ...pendingActivationRailSectionKeyFields(intent),
    ...(intent.railPlacement
      ? { railPlacement: { ...intent.railPlacement } }
      : {}),
    ...(intent.reasoningEffortExplicit !== undefined
      ? { reasoningEffortExplicit: intent.reasoningEffortExplicit }
      : {}),
    ...(intent.submitDiagnostics
      ? { submitDiagnostics: { ...intent.submitDiagnostics } }
      : {}),
    requestedAtUnixMs: intent.requestedAtUnixMs,
    requestId,
    ...(intent.settings ? { settings: { ...intent.settings } } : {}),
    status: "requested" as const,
    snapshotObservedAtUnixMs: null,
    snapshotOutcome: "not_observed" as const,
    title: intent.title?.trim() || null,
    workspaceId
  };
  const record: PendingActivationIntentRecord =
    intent.mode === "new"
      ? {
          ...recordBase,
          agentTargetId: agentTargetId!,
          clientSubmitId: clientSubmitId!,
          mode: "new",
          ...(optimisticTitle ? { optimisticTitle } : {})
        }
      : {
          ...recordBase,
          agentTargetId,
          clientSubmitId: null,
          mode: "existing"
        };
  return {
    commands: [
      ...supersededRequestIds.map((id) => ({
        expiryId: activationExpiryId(id),
        type: "engine/cancelExpiry" as const
      })),
      {
        dueAtUnixMs: intent.expiresAtUnixMs,
        expiryId: activationExpiryId(requestId),
        type: "engine/scheduleExpiry"
      },
      intent.mode === "new"
        ? {
            agentSessionId,
            ...(capabilityRefs.length > 0 ? { capabilityRefs } : {}),
            agentTargetId: agentTargetId!,
            commandId: `activate:${requestId}`,
            clientSubmitId: clientSubmitId!,
            correlationId: requestId,
            ...(intent.cwd !== undefined ? { cwd: intent.cwd } : {}),
            ...(runtimeContent.length > 0
              ? { initialContent: runtimeContent }
              : {}),
            ...(displayPrompt ? { initialDisplayPrompt: displayPrompt } : {}),
            ...(intent.isolation ? { isolation: intent.isolation } : {}),
            ...(intent.modelExplicit !== undefined
              ? { modelExplicit: intent.modelExplicit }
              : {}),
            ...(intent.railPlacement
              ? { railPlacement: { ...intent.railPlacement } }
              : {}),
            ...(intent.reasoningEffortExplicit !== undefined
              ? { reasoningEffortExplicit: intent.reasoningEffortExplicit }
              : {}),
            ...(intent.submitDiagnostics
              ? { submitDiagnostics: { ...intent.submitDiagnostics } }
              : {}),
            mode: "new" as const,
            ...(intent.settings ? { settings: { ...intent.settings } } : {}),
            timeoutMs: NEW_SESSION_ACTIVATION_COMMAND_TIMEOUT_MS,
            ...(intent.title?.trim() ? { title: intent.title.trim() } : {}),
            type: "session/activate",
            ...(intent.visible !== undefined
              ? { visible: intent.visible }
              : {}),
            workspaceId
          }
        : {
            agentSessionId,
            ...(capabilityRefs.length > 0 ? { capabilityRefs } : {}),
            ...(agentTargetId ? { agentTargetId } : {}),
            commandId: `activate:${requestId}`,
            correlationId: requestId,
            ...(intent.cwd !== undefined ? { cwd: intent.cwd } : {}),
            ...(runtimeContent.length > 0
              ? { initialContent: runtimeContent }
              : {}),
            ...(displayPrompt ? { initialDisplayPrompt: displayPrompt } : {}),
            ...(intent.submitDiagnostics
              ? { submitDiagnostics: { ...intent.submitDiagnostics } }
              : {}),
            mode: "existing" as const,
            ...(intent.settings ? { settings: { ...intent.settings } } : {}),
            timeoutMs: EXISTING_SESSION_ACTIVATION_COMMAND_TIMEOUT_MS,
            ...(intent.title?.trim() ? { title: intent.title.trim() } : {}),
            type: "session/activate" as const,
            ...(intent.visible !== undefined
              ? { visible: intent.visible }
              : {}),
            workspaceId
          }
    ],
    state: replaceActivation(
      markSessionActive(baseState, agentSessionId),
      record
    )
  };
}

function recordActivationFailure(
  state: PendingIntentsState,
  intent: Extract<EngineIntent, { type: "activation/failureRecorded" }>
): EngineReducerResult<PendingIntentsState> {
  const agentSessionId = intent.agentSessionId.trim();
  const requestId = intent.requestId.trim();
  if (
    !agentSessionId ||
    !requestId ||
    state.activationsByRequestId[requestId]
  ) {
    return unchanged(state);
  }
  return {
    commands: NO_COMMANDS,
    state: replaceActivation(state, {
      agentSessionId,
      agentTargetId: null,
      clientSubmitId: null,
      content: [],
      cwd: "",
      commandOutcome: "failed",
      commandSettledAtUnixMs: intent.occurredAtUnixMs,
      errorCode: intent.errorCode?.trim() || null,
      errorMessage: intent.errorMessage.trim() || null,
      expiresAtUnixMs: Number.MAX_SAFE_INTEGER,
      initialPromptRetracted: false,
      initialTurnExpected: false,
      lastObservedStage: "command_settled",
      mode: "existing",
      requestedAtUnixMs: intent.occurredAtUnixMs,
      requestId,
      status: "failed",
      snapshotObservedAtUnixMs: null,
      snapshotOutcome: "not_observed",
      title: null,
      workspaceId: intent.workspaceId
    })
  };
}

function clearActivationFailure(
  state: PendingIntentsState,
  agentSessionId: string
): EngineReducerResult<PendingIntentsState> {
  const ids = Object.values(state.activationsByRequestId)
    .filter(
      (record) =>
        record.agentSessionId === agentSessionId.trim() &&
        record.status === "failed"
    )
    .map((record) => record.requestId);
  if (ids.length === 0) {
    return unchanged(state);
  }
  return {
    commands: ids
      .filter(
        (id) =>
          state.activationsByRequestId[id]?.expiresAtUnixMs !==
          Number.MAX_SAFE_INTEGER
      )
      .map((id) => ({
        expiryId: activationExpiryId(id),
        type: "engine/cancelExpiry" as const
      })),
    state: ids.reduce(deleteActivation, state)
  };
}

function requestUnactivation(
  state: PendingIntentsState,
  intent: Extract<EngineIntent, { type: "activation/unactivateRequested" }>
): EngineReducerResult<PendingIntentsState> {
  const agentSessionId = intent.agentSessionId.trim();
  if (!agentSessionId || !intent.workspaceId.trim()) {
    return unchanged(state);
  }
  const activationIds = Object.values(state.activationsByRequestId)
    .filter((record) => record.agentSessionId === agentSessionId)
    .map((record) => record.requestId);
  return {
    commands: [
      ...activationIds
        .filter(
          (id) =>
            state.activationsByRequestId[id]?.expiresAtUnixMs !==
            Number.MAX_SAFE_INTEGER
        )
        .map((id) => ({
          expiryId: activationExpiryId(id),
          type: "engine/cancelExpiry" as const
        })),
      {
        agentSessionId,
        commandId: intent.commandId,
        type: "session/unactivate" as const,
        workspaceId: intent.workspaceId
      }
    ],
    state: markSessionInactive(
      activationIds.reduce(deleteActivation, state),
      agentSessionId
    )
  };
}

function stopPendingActivation(
  state: PendingIntentsState,
  intent: Extract<EngineIntent, { type: "session/stopRequested" }>
): EngineReducerResult<PendingIntentsState> {
  const agentSessionId = intent.agentSessionId.trim();
  const workspaceId = intent.workspaceId.trim();
  const activation = Object.values(state.activationsByRequestId)
    .filter(
      (record) =>
        record.agentSessionId === agentSessionId &&
        record.workspaceId === workspaceId &&
        (record.status === "requested" || record.status === "uncertain")
    )
    .sort((left, right) => right.requestedAtUnixMs - left.requestedAtUnixMs)[0];
  if (!activation) return unchanged(state);
  return {
    commands: [
      {
        type: "engine/abortExternalCommand",
        reason: "agent session activation canceled by user",
        targetCommandId: `activate:${activation.requestId}`
      }
    ],
    state: replaceActivation(state, {
      ...activation,
      errorCode: null,
      errorMessage: null,
      commandOutcome:
        activation.commandOutcome === "pending"
          ? "canceled"
          : activation.commandOutcome,
      lastObservedStage: "canceled",
      status: "canceled"
    })
  };
}

function settleActivationSettingsCommand(
  state: PendingIntentsState,
  intent: EngineCommandResultIntent,
  validation: ScopedSessionResultValidation | null
): EngineReducerResult<PendingIntentsState> {
  const record = settlePendingActivationSettings(
    state.activationsByRequestId,
    intent,
    validation
  );
  return record
    ? { commands: NO_COMMANDS, state: replaceActivation(state, record) }
    : unchanged(state);
}

function expireActivation(
  state: PendingIntentsState,
  expiryId: string
): EngineReducerResult<PendingIntentsState> {
  const requestId = expiryId.slice("activation:".length);
  const record = state.activationsByRequestId[requestId];
  if (!record) {
    return unchanged(state);
  }
  if (record.status === "confirmed") {
    return unchanged(state);
  }
  if (record.status === "canceled") {
    return { commands: NO_COMMANDS, state: deleteActivation(state, requestId) };
  }
  const recoveryIntent =
    record.status === "requested" || record.status === "uncertain"
      ? createActivationRecoveryIntent(record)
      : null;
  return {
    commands: NO_COMMANDS,
    ...(recoveryIntent ? { followUpIntents: [recoveryIntent] } : {}),
    state: replaceActivation(state, {
      ...record,
      errorCode: record.errorCode ?? "activation_confirmation_expired",
      errorMessage: record.errorMessage,
      lastObservedStage: "expired",
      status: "failed"
    })
  };
}

function removeActivation(
  state: PendingIntentsState,
  requestId: string
): EngineReducerResult<PendingIntentsState> {
  const id = requestId.trim();
  if (!state.activationsByRequestId[id]) {
    return unchanged(state);
  }
  return {
    commands: [
      { expiryId: activationExpiryId(id), type: "engine/cancelExpiry" }
    ],
    state: deleteActivation(state, id)
  };
}

function removeSessionIntents(
  state: PendingIntentsState,
  agentSessionId: string
): EngineReducerResult<PendingIntentsState> {
  const submitIds = Object.values(state.submitsByClientSubmitId)
    .filter((record) => record.agentSessionId === agentSessionId.trim())
    .map((record) => record.clientSubmitId);
  const activationIds = Object.values(state.activationsByRequestId)
    .filter((record) => record.agentSessionId === agentSessionId.trim())
    .map((record) => record.requestId);
  const wasInactive = state.inactiveSessionIds[agentSessionId.trim()] === true;
  if (submitIds.length === 0 && activationIds.length === 0 && !wasInactive) {
    return unchanged(state);
  }
  return {
    commands: [
      ...submitIds.map((id) => ({
        expiryId: submitExpiryId(id),
        type: "engine/cancelExpiry" as const
      })),
      ...activationIds.map((id) => ({
        expiryId: activationExpiryId(id),
        type: "engine/cancelExpiry" as const
      }))
    ],
    state: removeInactiveSession(
      activationIds.reduce(
        deleteActivation,
        submitIds.reduce(deleteSubmit, state)
      ),
      agentSessionId
    )
  };
}

function activationExpiryId(requestId: string): string {
  return `activation:${requestId}`;
}
