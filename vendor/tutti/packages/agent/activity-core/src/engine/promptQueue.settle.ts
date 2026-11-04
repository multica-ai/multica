import type { SendInputResultValidation } from "./commandResult.validation.ts";
import { isPreTurnSendFailure } from "./promptSendFailure.ts";
import { queueOwnedReconcileCommand } from "./promptQueue.ownedReconcile.ts";
import { setPendingSendNowForPrompt } from "./promptQueue.pendingSendNow.ts";
import { compactQueueRecord } from "./promptQueue.record.ts";
import type {
  PromptQueueRecord,
  PromptQueueState
} from "./promptQueue.types.ts";
import type {
  EngineCommand,
  EngineCommandResultIntent,
  EngineReducerResult
} from "./types.ts";

const NO_COMMANDS: readonly EngineCommand[] = [];

export function settleQueueCommand(
  state: PromptQueueState,
  intent: EngineCommandResultIntent,
  validation: SendInputResultValidation | null
): EngineReducerResult<PromptQueueState> {
  const entry = Object.entries(state.recordsBySessionId).find(
    ([, record]) => record.inFlight?.commandId === intent.commandId
  );
  if (!entry) return unchanged(state);
  const [agentSessionId, current] = entry;
  const inFlight = current.inFlight!;
  if (intent.outcome === "succeeded" && validation?.kind === "valid") {
    const deliveryBarrierTurnId = validation.result.turnId;
    const record = compactQueueRecord({
      ...current,
      deliveryBarrierTurnId,
      failedPromptId: null,
      failureMessage: null,
      inFlight: null,
      prompts: current.prompts.filter(
        (prompt) => prompt.id !== inFlight.promptId
      ),
      pendingSendNowByPromptId: setPendingSendNowForPrompt(
        current.pendingSendNowByPromptId,
        inFlight.promptId,
        null
      ),
      sendNextPromptId:
        current.sendNextPromptId === inFlight.promptId
          ? null
          : current.sendNextPromptId
    });
    return result(
      record
        ? replaceRecord(state, agentSessionId, record)
        : deleteRecord(state, agentSessionId)
    );
  }
  if (intent.outcome === "timedOut" || intent.outcome === "succeeded") {
    const record = {
      ...current,
      failedPromptId: inFlight.promptId,
      failureMessage: null,
      inFlight: null,
      uncertainDelivery: inFlight
    };
    return {
      commands: [
        queueOwnedReconcileCommand(agentSessionId, current.workspaceId, intent)
      ],
      state: replaceRecord(state, agentSessionId, record)
    };
  }
  if (isPreTurnSendFailure(intent)) {
    const nextState =
      current.prompts.find((prompt) => prompt.id === inFlight.promptId)
        ?.visibleInQueue === false
        ? removeHiddenFailedPrompt(
            state,
            agentSessionId,
            current,
            inFlight.promptId
          )
        : replaceRecord(state, agentSessionId, {
            ...current,
            failedPromptId: null,
            failureMessage: null,
            inFlight: null
          });
    return {
      commands: [
        queueOwnedReconcileCommand(agentSessionId, current.workspaceId, intent)
      ],
      state: nextState
    };
  }
  if (
    current.prompts.find((prompt) => prompt.id === inFlight.promptId)
      ?.visibleInQueue === false
  ) {
    return result(
      removeHiddenFailedPrompt(
        state,
        agentSessionId,
        current,
        inFlight.promptId
      )
    );
  }
  return result(
    replaceRecord(state, agentSessionId, {
      ...current,
      failedPromptId: inFlight.promptId,
      failureMessage: intent.errorMessage?.trim() || null,
      inFlight: null
    })
  );
}

function removeHiddenFailedPrompt(
  state: PromptQueueState,
  agentSessionId: string,
  current: PromptQueueRecord,
  promptId: string
): PromptQueueState {
  const record = compactQueueRecord({
    ...current,
    failedPromptId:
      current.failedPromptId === promptId ? null : current.failedPromptId,
    failureMessage:
      current.failedPromptId === promptId ? null : current.failureMessage,
    inFlight: null,
    pendingSendNowByPromptId: setPendingSendNowForPrompt(
      current.pendingSendNowByPromptId,
      promptId,
      null
    ),
    prompts: current.prompts.filter((prompt) => prompt.id !== promptId),
    sendNextPromptId:
      current.sendNextPromptId === promptId ? null : current.sendNextPromptId
  });
  return record
    ? replaceRecord(state, agentSessionId, record)
    : deleteRecord(state, agentSessionId);
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
