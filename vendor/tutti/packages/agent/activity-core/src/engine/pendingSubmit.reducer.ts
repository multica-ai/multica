import { normalizeAgentActivityCapabilityReferences } from "../capabilityReferences.ts";
import type { AgentActivityMessage } from "../types.ts";
import type { SendInputResultValidation } from "./commandResult.validation.ts";
import type {
  PendingIntentsState,
  PendingSubmitIntentRecord,
  SubmitRequestedIntent
} from "./pendingIntents.types.ts";
import { canonicalTurnKey } from "./sessionEntityKeys.ts";
import type {
  EngineCommand,
  EngineCommandResultIntent,
  EngineReducerResult
} from "./types.ts";

const NO_COMMANDS: readonly EngineCommand[] = [];

export function requestSubmit(
  state: PendingIntentsState,
  intent: SubmitRequestedIntent
): EngineReducerResult<PendingIntentsState> {
  const clientSubmitId = intent.clientSubmitId.trim();
  const agentSessionId = intent.agentSessionId.trim();
  if (
    !clientSubmitId ||
    !agentSessionId ||
    intent.content.length === 0 ||
    state.submitsByClientSubmitId[clientSubmitId]
  ) {
    return unchanged(state);
  }
  const capabilityRefs = normalizeAgentActivityCapabilityReferences(
    intent.capabilityRefs
  );
  const record: PendingSubmitIntentRecord = {
    acceptedSessionVersion: null,
    agentSessionId,
    clientSubmitId,
    ...(capabilityRefs.length > 0 ? { capabilityRefs } : {}),
    content: intent.content.map((block) => ({ ...block })),
    ...(intent.displayPrompt?.trim()
      ? { displayPrompt: intent.displayPrompt.trim() }
      : {}),
    errorCode: null,
    errorMessage: null,
    errorReason: null,
    expiresAtUnixMs: intent.expiresAtUnixMs,
    ...(intent.submitDiagnostics
      ? { submitDiagnostics: { ...intent.submitDiagnostics } }
      : {}),
    requestedAtUnixMs: intent.requestedAtUnixMs,
    status: "requested",
    turnId: null,
    workspaceId: intent.workspaceId
  };
  return {
    commands: [
      {
        dueAtUnixMs: intent.expiresAtUnixMs,
        expiryId: submitExpiryId(clientSubmitId),
        type: "engine/scheduleExpiry"
      }
    ],
    state: replaceSubmit(state, record)
  };
}

export function settleSubmitCommand(
  state: PendingIntentsState,
  intent: EngineCommandResultIntent,
  validation: SendInputResultValidation | null
): EngineReducerResult<PendingIntentsState> {
  const clientSubmitId = intent.correlationId?.trim() ?? "";
  const record = state.submitsByClientSubmitId[clientSubmitId];
  if (!record) {
    return unchanged(state);
  }
  if (intent.outcome === "succeeded") {
    if (!validation || validation.kind === "invalid") {
      return {
        commands: NO_COMMANDS,
        state: replaceSubmit(state, {
          ...record,
          errorCode: "invalid_command_result",
          errorMessage: null,
          errorReason: null,
          status: "uncertain"
        })
      };
    }
    const result = validation.result;
    return {
      commands: NO_COMMANDS,
      state: replaceSubmit(state, {
        ...record,
        acceptedSessionVersion: activitySessionVersion(result.session),
        status: "accepted",
        turnId: result.turnId.trim()
      })
    };
  }
  return {
    commands: NO_COMMANDS,
    state: replaceSubmit(state, {
      ...record,
      errorCode: intent.errorCode ?? null,
      errorMessage:
        intent.outcome === "timedOut"
          ? null
          : intent.errorMessage?.trim() || null,
      errorReason:
        intent.outcome === "timedOut"
          ? null
          : intent.errorReason?.trim() || null,
      status: intent.outcome === "timedOut" ? "uncertain" : "failed"
    })
  };
}

export function confirmFromMessages(
  state: PendingIntentsState,
  messages: readonly AgentActivityMessage[]
): EngineReducerResult<PendingIntentsState> {
  const matches = new Map<string, Set<string>>();
  for (const message of messages) {
    const clientSubmitId = messageClientSubmitId(message);
    if (!clientSubmitId) continue;
    const record = state.submitsByClientSubmitId[clientSubmitId];
    if (
      !record ||
      message.agentSessionId.trim() !== record.agentSessionId ||
      (message.workspaceId !== undefined &&
        message.workspaceId.trim() !== record.workspaceId)
    ) {
      continue;
    }
    const turnIds = matches.get(clientSubmitId) ?? new Set<string>();
    const turnId = message.turnId?.trim() ?? "";
    if (turnId) turnIds.add(turnId);
    matches.set(clientSubmitId, turnIds);
  }
  if (matches.size === 0) {
    return unchanged(state);
  }
  let next = state;
  for (const [clientSubmitId, turnIds] of matches) {
    const record = next.submitsByClientSubmitId[clientSubmitId];
    if (!record || turnIds.size > 1) continue;
    const [messageTurnId] = turnIds;
    if (record.turnId && messageTurnId && record.turnId !== messageTurnId) {
      continue;
    }
    next = replaceSubmit(next, {
      ...record,
      status: "confirmed",
      turnId: record.turnId ?? messageTurnId ?? null
    });
  }
  return next === state
    ? unchanged(state)
    : { commands: NO_COMMANDS, state: next };
}

export function confirmFromSessions(
  state: PendingIntentsState,
  turnsById: Readonly<Record<string, import("../types.ts").AgentActivityTurn>>
): EngineReducerResult<PendingIntentsState> {
  let next = state;
  for (const record of Object.values(state.submitsByClientSubmitId)) {
    if (record.status !== "accepted" || !record.turnId) {
      continue;
    }
    const turn =
      turnsById[canonicalTurnKey(record.agentSessionId, record.turnId)];
    if (
      turn?.agentSessionId === record.agentSessionId &&
      turn.phase === "settled"
    ) {
      next = replaceSubmit(next, { ...record, status: "confirmed" });
    }
  }
  return next === state
    ? unchanged(state)
    : { commands: NO_COMMANDS, state: next };
}

export function expireSubmit(
  state: PendingIntentsState,
  expiryId: string,
  options: {
    deliveryIsQueuePending?: boolean;
    dueAtUnixMs?: number;
  } = {}
): EngineReducerResult<PendingIntentsState> {
  if (!expiryId.startsWith("submit:")) {
    return unchanged(state);
  }
  const clientSubmitId = expiryId.slice("submit:".length);
  const record = state.submitsByClientSubmitId[clientSubmitId];
  if (!record) {
    return unchanged(state);
  }
  if (options.deliveryIsQueuePending) {
    const confirmationWindowMs = Math.max(
      1,
      record.expiresAtUnixMs - record.requestedAtUnixMs
    );
    return {
      commands: [
        {
          dueAtUnixMs:
            Math.max(
              record.expiresAtUnixMs,
              options.dueAtUnixMs ?? record.expiresAtUnixMs
            ) + confirmationWindowMs,
          expiryId,
          type: "engine/scheduleExpiry"
        }
      ],
      state
    };
  }
  if (record.status === "accepted" || record.status === "confirmed") {
    return {
      commands: NO_COMMANDS,
      state: deleteSubmit(state, clientSubmitId)
    };
  }
  return {
    commands: NO_COMMANDS,
    state: replaceSubmit(state, {
      ...record,
      errorMessage: record.errorMessage,
      status: "failed"
    })
  };
}

export function removeSubmit(
  state: PendingIntentsState,
  clientSubmitId: string
): EngineReducerResult<PendingIntentsState> {
  const id = clientSubmitId.trim();
  if (!state.submitsByClientSubmitId[id]) {
    return unchanged(state);
  }
  return {
    commands: [{ expiryId: submitExpiryId(id), type: "engine/cancelExpiry" }],
    state: deleteSubmit(state, id)
  };
}

function messageClientSubmitId(message: AgentActivityMessage): string | null {
  const value = message.payload?.clientSubmitId;
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

function activitySessionVersion(
  session: import("../types.ts").AgentActivitySession
): number | null {
  return (
    session.updatedAtUnixMs ??
    session.lastEventUnixMs ??
    session.messageVersion ??
    session.createdAtUnixMs ??
    session.startedAtUnixMs ??
    null
  );
}

export function submitExpiryId(clientSubmitId: string): string {
  return `submit:${clientSubmitId}`;
}

function replaceSubmit(
  state: PendingIntentsState,
  record: PendingSubmitIntentRecord
): PendingIntentsState {
  return {
    ...state,
    submitsByClientSubmitId: {
      ...state.submitsByClientSubmitId,
      [record.clientSubmitId]: record
    }
  };
}

export function deleteSubmit(
  state: PendingIntentsState,
  clientSubmitId: string
): PendingIntentsState {
  const submits = { ...state.submitsByClientSubmitId };
  delete submits[clientSubmitId];
  return { ...state, submitsByClientSubmitId: submits };
}

function unchanged(
  state: PendingIntentsState
): EngineReducerResult<PendingIntentsState> {
  return { commands: NO_COMMANDS, state };
}
