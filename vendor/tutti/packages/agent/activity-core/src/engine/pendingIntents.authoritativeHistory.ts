import type { AgentActivityMessage } from "../types.ts";
import type {
  PendingActivationIntentRecord,
  PendingIntentsState
} from "./pendingIntents.types.ts";
import { deleteSubmit, submitExpiryId } from "./pendingSubmit.reducer.ts";
import type { SessionHistoryAuthoritativeSnapshotReceivedIntent } from "./sessionLifecycle.types.ts";
import type { EngineCommand, EngineReducerResult } from "./types.ts";

const NO_COMMANDS: readonly EngineCommand[] = [];

export function reconcilePendingIntentsFromAuthoritativeHistory(
  state: PendingIntentsState,
  intent: SessionHistoryAuthoritativeSnapshotReceivedIntent
): EngineReducerResult<PendingIntentsState> {
  const agentSessionId = intent.agentSessionId.trim();
  const workspaceId = intent.workspaceId.trim();
  if (
    !agentSessionId ||
    !workspaceId ||
    intent.session.agentSessionId.trim() !== agentSessionId ||
    intent.session.workspaceId.trim() !== workspaceId
  ) {
    return unchanged(state);
  }

  const effectiveTurnIds = new Set(
    intent.turns
      .filter((turn) => turn.agentSessionId.trim() === agentSessionId)
      .map((turn) => turn.turnId.trim())
      .filter(Boolean)
  );
  const effectiveClientSubmitIds = new Set(
    intent.messages
      .filter(
        (message) =>
          message.agentSessionId.trim() === agentSessionId &&
          (!message.workspaceId || message.workspaceId.trim() === workspaceId)
      )
      .map(messageClientSubmitId)
      .filter((value): value is string => value !== null)
  );

  const commands: EngineCommand[] = [];
  let next = state;
  for (const record of Object.values(state.submitsByClientSubmitId)) {
    const turnId = record.turnId?.trim() ?? "";
    if (
      record.agentSessionId !== agentSessionId ||
      record.workspaceId !== workspaceId ||
      (record.status !== "accepted" && record.status !== "confirmed") ||
      !turnId ||
      effectiveTurnIds.has(turnId) ||
      effectiveClientSubmitIds.has(record.clientSubmitId)
    ) {
      continue;
    }
    next = deleteSubmit(next, record.clientSubmitId);
    commands.push({
      expiryId: submitExpiryId(record.clientSubmitId),
      type: "engine/cancelExpiry"
    });
  }

  for (const record of Object.values(state.activationsByRequestId)) {
    if (
      record.agentSessionId !== agentSessionId ||
      record.workspaceId !== workspaceId ||
      record.mode !== "new" ||
      record.status !== "confirmed" ||
      record.initialPromptRetracted ||
      !record.initialTurnExpected ||
      effectiveClientSubmitIds.has(record.clientSubmitId)
    ) {
      continue;
    }
    next = replaceActivation(next, {
      ...record,
      initialPromptRetracted: true
    });
  }

  return next === state ? unchanged(state) : { commands, state: next };
}

function messageClientSubmitId(message: AgentActivityMessage): string | null {
  const value = message.payload?.clientSubmitId;
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

function replaceActivation(
  state: PendingIntentsState,
  record: PendingActivationIntentRecord
): PendingIntentsState {
  return {
    ...state,
    activationsByRequestId: {
      ...state.activationsByRequestId,
      [record.requestId]: record
    }
  };
}

function unchanged(
  state: PendingIntentsState
): EngineReducerResult<PendingIntentsState> {
  return { commands: NO_COMMANDS, state };
}
