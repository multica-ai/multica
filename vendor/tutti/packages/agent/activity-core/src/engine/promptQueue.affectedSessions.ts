import type {
  ScopedSessionResultValidation,
  SendInputResultValidation
} from "./commandResult.validation.ts";
import { queueOwnedReconcileSendCommandId } from "./promptQueue.ownedReconcile.ts";
import type { PromptQueueState } from "./promptQueue.types.ts";
import type { EngineIntent } from "./types.ts";

export interface PromptQueueAffectedSessionsContext {
  sendResultValidation?: SendInputResultValidation | null;
  settingsResultValidation?: ScopedSessionResultValidation | null;
}

export function affectedPromptQueueSessionIds(
  state: PromptQueueState,
  intent: EngineIntent,
  context: PromptQueueAffectedSessionsContext
): string[] {
  const ids: string[] = [];
  if ("agentSessionId" in intent && typeof intent.agentSessionId === "string") {
    ids.push(intent.agentSessionId.trim());
  }
  if (intent.type === "session/snapshotReceived") {
    ids.push(
      ...intent.sessions.map((session) => session.agentSessionId.trim())
    );
  }
  if (intent.type === "session/upserted") {
    ids.push(intent.session.agentSessionId.trim());
  }
  if (
    intent.type === "turn/projectionReceived" ||
    intent.type === "turn/upserted"
  ) {
    ids.push(intent.turn.agentSessionId.trim());
  }
  if (intent.type === "interaction/upserted") {
    ids.push(intent.interaction.agentSessionId.trim());
  }
  if (intent.type === "message/snapshotReceived") {
    ids.push(
      ...intent.messages.map((message) => message.agentSessionId.trim())
    );
  }
  if (intent.type === "engine/commandResult") {
    const queueEntry = Object.entries(state.recordsBySessionId).find(
      ([, record]) => record.inFlight?.commandId === intent.commandId
    );
    if (queueEntry) ids.push(queueEntry[0]);
    const ownedSendCommandId = queueOwnedReconcileSendCommandId(
      intent.commandId
    );
    if (ownedSendCommandId) {
      const uncertainEntry = Object.entries(state.recordsBySessionId).find(
        ([, record]) =>
          record.uncertainDelivery?.commandId === ownedSendCommandId
      );
      if (uncertainEntry) ids.push(uncertainEntry[0]);
    }
    // Settings results (success or settle-to-unknown/failed) must re-enter
    // drain so a queued send either proceeds after idle or stays gated.
    if (intent.commandType === "session/updateSettings") {
      const settingsSessionId = intent.correlationId?.trim();
      if (settingsSessionId) ids.push(settingsSessionId);
    }
    const validatedSessionIds = [
      context.sendResultValidation?.kind === "valid"
        ? context.sendResultValidation.result.session.agentSessionId
        : undefined,
      context.settingsResultValidation?.kind === "valid"
        ? context.settingsResultValidation.session.agentSessionId
        : undefined
    ];
    ids.push(
      ...validatedSessionIds
        .filter((id): id is string => typeof id === "string")
        .map((id) => id.trim())
    );
  }
  return ids.filter(Boolean);
}
