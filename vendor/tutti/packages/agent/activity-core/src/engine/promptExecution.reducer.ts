import type { ScopedSessionResultValidation } from "./commandResult.validation.ts";
import type {
  PromptExecutionState,
  PromptSettingsExecutionRecord
} from "./promptExecution.types.ts";
import type { PromptQueueSendCommand } from "./promptQueue.types.ts";
import type {
  EngineCommand,
  EngineCommandResultIntent,
  EngineReducerResult
} from "./types.ts";
import type {
  RootEngineIntent,
  RootEngineReducerResult
} from "./rootReducer.types.ts";

const NO_COMMANDS: readonly EngineCommand[] = [];

export function createInitialPromptExecutionState(): PromptExecutionState {
  return { recordsBySettingsCommandId: {} };
}

export function promptExecutionReducer(
  state: PromptExecutionState,
  intent: RootEngineIntent,
  context: {
    settingsResultValidation?: ScopedSessionResultValidation | null;
  } = {}
): RootEngineReducerResult<PromptExecutionState> {
  if (intent.type === "prompt/executionRequested") {
    return requestPromptExecution(state, intent.command);
  }
  if (
    intent.type === "engine/commandResult" &&
    intent.commandType === "session/updateSettings"
  ) {
    return settlePromptSettings(
      state,
      intent,
      context.settingsResultValidation ?? null
    );
  }
  if (intent.type === "session/removed") {
    return removeSessionExecutions(state, intent.agentSessionId);
  }
  return unchanged(state);
}

export function selectPromptSettingsExecution(
  state: PromptExecutionState,
  settingsCommandId: string
): PromptSettingsExecutionRecord | null {
  return state.recordsBySettingsCommandId[settingsCommandId.trim()] ?? null;
}

function requestPromptExecution(
  state: PromptExecutionState,
  command: PromptQueueSendCommand
): EngineReducerResult<PromptExecutionState> {
  const settings = command.requiredSettingsPatch;
  if (!settings || Object.keys(settings).length === 0) {
    return { commands: [sendCommand(command)], state };
  }
  const settingsCommandId = settingsCommandIdForPrompt(command.commandId);
  if (
    !settingsCommandId ||
    !command.agentSessionId.trim() ||
    !command.workspaceId.trim() ||
    state.recordsBySettingsCommandId[settingsCommandId]
  ) {
    return unchanged(state);
  }
  return {
    commands: [
      {
        agentSessionId: command.agentSessionId,
        commandId: settingsCommandId,
        correlationId: command.agentSessionId,
        settings: { ...settings },
        ...(command.timeoutMs !== undefined
          ? { timeoutMs: command.timeoutMs }
          : {}),
        type: "session/updateSettings",
        workspaceId: command.workspaceId
      }
    ],
    state: {
      recordsBySettingsCommandId: {
        ...state.recordsBySettingsCommandId,
        [settingsCommandId]: {
          promptCommand: command,
          settingsCommandId
        }
      }
    }
  };
}

function settlePromptSettings(
  state: PromptExecutionState,
  intent: EngineCommandResultIntent,
  validation: ScopedSessionResultValidation | null
): EngineReducerResult<PromptExecutionState> {
  const record = selectPromptSettingsExecution(state, intent.commandId);
  if (!record) return unchanged(state);
  const next = deleteRecord(state, record.settingsCommandId);
  if (intent.outcome === "succeeded" && validation?.kind === "valid") {
    return {
      commands: [sendCommand(record.promptCommand)],
      state: next
    };
  }
  return {
    commands: NO_COMMANDS,
    followUpIntents: [
      promptFailureIntent(record.promptCommand, intent, validation)
    ],
    state: next
  };
}

function promptFailureIntent(
  command: PromptQueueSendCommand,
  intent: EngineCommandResultIntent,
  validation: ScopedSessionResultValidation | null
): EngineCommandResultIntent {
  const invalidResult =
    intent.outcome === "succeeded" && validation?.kind !== "valid";
  return {
    commandId: command.commandId,
    commandType: "queue/sendPrompt",
    ...(command.correlationId ? { correlationId: command.correlationId } : {}),
    errorCode: invalidResult
      ? "invalid_command_result"
      : intent.outcome === "timedOut"
        ? "settings_precondition_timeout"
        : (intent.errorCode ?? "settings_precondition_failed"),
    ...(intent.errorMessage ? { errorMessage: intent.errorMessage } : {}),
    ...(intent.errorReason ? { errorReason: intent.errorReason } : {}),
    outcome: "failed",
    type: "engine/commandResult"
  };
}

function sendCommand(command: PromptQueueSendCommand): PromptQueueSendCommand {
  const { requiredSettingsPatch: _settings, ...send } = command;
  return send;
}

function removeSessionExecutions(
  state: PromptExecutionState,
  rawAgentSessionId: string
): EngineReducerResult<PromptExecutionState> {
  const agentSessionId = rawAgentSessionId.trim();
  const removed = Object.values(state.recordsBySettingsCommandId).filter(
    (record) => record.promptCommand.agentSessionId === agentSessionId
  );
  if (removed.length === 0) return unchanged(state);
  const next = { ...state.recordsBySettingsCommandId };
  for (const record of removed) {
    delete next[record.settingsCommandId];
  }
  return {
    commands: removed.map((record) => ({
      reason: "prompt session removed",
      targetCommandId: record.settingsCommandId,
      type: "engine/abortExternalCommand"
    })),
    state: { recordsBySettingsCommandId: next }
  };
}

function deleteRecord(
  state: PromptExecutionState,
  settingsCommandId: string
): PromptExecutionState {
  const records = { ...state.recordsBySettingsCommandId };
  delete records[settingsCommandId];
  return { recordsBySettingsCommandId: records };
}

function settingsCommandIdForPrompt(promptCommandId: string): string {
  const id = promptCommandId.trim();
  return id ? `prompt:settings:${id}` : "";
}

function unchanged(
  state: PromptExecutionState
): EngineReducerResult<PromptExecutionState> {
  return { commands: NO_COMMANDS, state };
}
