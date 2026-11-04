import type {
  EngineCommand,
  EngineCommandResultIntent,
  EngineReducerResult
} from "./types.ts";
import type {
  PromptQueueRecord,
  PromptQueueState
} from "./promptQueue.types.ts";
import { compactQueueRecord } from "./promptQueue.record.ts";
import { setPendingSendNowForPrompt } from "./promptQueue.pendingSendNow.ts";

const NO_COMMANDS: readonly EngineCommand[] = [];

export const QUEUE_OWNED_RECONCILE_COMMAND_PREFIX = "queue:reconcile:";

export function queueOwnedReconcileCommand(
  agentSessionId: string,
  workspaceId: string,
  intent: EngineCommandResultIntent
): Extract<EngineCommand, { type: "session/reconcile" }> {
  return {
    agentSessionId,
    commandId: `${QUEUE_OWNED_RECONCILE_COMMAND_PREFIX}${intent.commandId}`,
    live: false,
    scope: "state_and_messages",
    timeoutMs: 30_000,
    type: "session/reconcile",
    workspaceId
  };
}

/**
 * After a send times out, the queue holds `uncertainDelivery` until exact turn
 * proof arrives. The follow-up owned reconcile may apply that proof via
 * message/snapshotReceived before this result lands. If uncertainty remains:
 * - succeeded reconcile → definitive non-delivery; drop the prompt so later
 *   queued work can drain
 * - failed/timedOut reconcile → retryable failed head (send-now / remove),
 *   matching Tutti Mode's inconclusive-owned-reconcile rule
 */
export function settleOwnedQueueReconcile(
  state: PromptQueueState,
  intent: EngineCommandResultIntent
): EngineReducerResult<PromptQueueState> {
  const sendCommandId = queueOwnedReconcileSendCommandId(intent.commandId);
  if (!sendCommandId) return unchanged(state);
  const entry = Object.entries(state.recordsBySessionId).find(
    ([, record]) => record.uncertainDelivery?.commandId === sendCommandId
  );
  if (!entry) return unchanged(state);
  const [agentSessionId, current] = entry;
  const uncertain = current.uncertainDelivery!;
  if (intent.outcome === "succeeded") {
    const record = compactQueueRecord({
      ...current,
      failedPromptId:
        current.failedPromptId === uncertain.promptId
          ? null
          : current.failedPromptId,
      failureMessage:
        current.failedPromptId === uncertain.promptId
          ? null
          : current.failureMessage,
      prompts: current.prompts.filter(
        (prompt) => prompt.id !== uncertain.promptId
      ),
      pendingSendNowByPromptId: setPendingSendNowForPrompt(
        current.pendingSendNowByPromptId,
        uncertain.promptId,
        null
      ),
      sendNextPromptId:
        current.sendNextPromptId === uncertain.promptId
          ? null
          : current.sendNextPromptId,
      uncertainDelivery: null
    });
    return result(
      record
        ? replaceRecord(state, agentSessionId, record)
        : deleteRecord(state, agentSessionId)
    );
  }
  return result(
    replaceRecord(state, agentSessionId, {
      ...current,
      failedPromptId: uncertain.promptId,
      failureMessage: intent.errorMessage?.trim() || current.failureMessage,
      uncertainDelivery: null
    })
  );
}

export function queueOwnedReconcileSendCommandId(
  commandId: string | null | undefined
): string | null {
  const id = commandId?.trim() ?? "";
  if (!id.startsWith(QUEUE_OWNED_RECONCILE_COMMAND_PREFIX)) {
    return null;
  }
  const sendCommandId = id.slice(QUEUE_OWNED_RECONCILE_COMMAND_PREFIX.length);
  return sendCommandId || null;
}

function replaceRecord(
  state: PromptQueueState,
  agentSessionId: string,
  record: PromptQueueRecord
): PromptQueueState {
  return {
    ...state,
    recordsBySessionId: {
      ...state.recordsBySessionId,
      [agentSessionId]: record
    }
  };
}

function deleteRecord(
  state: PromptQueueState,
  agentSessionId: string
): PromptQueueState {
  const records = { ...state.recordsBySessionId };
  delete records[agentSessionId];
  return { ...state, recordsBySessionId: records };
}

function result(
  state: PromptQueueState
): EngineReducerResult<PromptQueueState> {
  return { commands: NO_COMMANDS, state };
}

function unchanged(
  state: PromptQueueState
): EngineReducerResult<PromptQueueState> {
  return { commands: NO_COMMANDS, state };
}
