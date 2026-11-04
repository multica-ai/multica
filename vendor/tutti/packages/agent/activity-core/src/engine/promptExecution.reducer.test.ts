import assert from "node:assert/strict";
import test from "node:test";
import type { AgentActivitySession } from "../types.ts";
import {
  createInitialPromptExecutionState,
  promptExecutionReducer
} from "./promptExecution.reducer.ts";
import type { PromptQueueSendCommand } from "./promptQueue.types.ts";

test("turns a prompt settings patch into a settings-lane precondition", () => {
  const command = promptCommand();
  const requested = promptExecutionReducer(
    createInitialPromptExecutionState(),
    {
      command,
      type: "prompt/executionRequested",
      workspaceId: command.workspaceId
    }
  );

  assert.equal(requested.followUpIntents, undefined);
  assert.deepEqual(requested.commands, [
    {
      agentSessionId: "session-1",
      commandId: "prompt:settings:send-1",
      correlationId: "session-1",
      settings: { browserUse: true },
      timeoutMs: 30_000,
      type: "session/updateSettings",
      workspaceId: "workspace-1"
    }
  ]);
  assert.equal(
    requested.state.recordsBySettingsCommandId["prompt:settings:send-1"]
      ?.promptCommand,
    command
  );
});

test("valid settings settlement starts the send", () => {
  const requested = requestPrompt();
  const settled = promptExecutionReducer(
    requested.state,
    {
      commandId: "prompt:settings:send-1",
      commandType: "session/updateSettings",
      correlationId: "session-1",
      outcome: "succeeded",
      type: "engine/commandResult",
      value: {}
    },
    {
      settingsResultValidation: {
        kind: "valid",
        session: {} as AgentActivitySession
      }
    }
  );

  assert.equal(settled.commands[0]?.type, "queue/sendPrompt");
  assert.equal(
    settled.commands[0]?.type === "queue/sendPrompt"
      ? settled.commands[0].requiredSettingsPatch
      : null,
    undefined
  );
  assert.equal(settled.followUpIntents, undefined);
  assert.deepEqual(settled.state.recordsBySettingsCommandId, {});
});

test("failed settings settlement fails the logical prompt without sending", () => {
  const requested = requestPrompt();
  const settled = promptExecutionReducer(requested.state, {
    commandId: "prompt:settings:send-1",
    commandType: "session/updateSettings",
    correlationId: "session-1",
    errorCode: "settings_rejected",
    errorMessage: "settings rejected",
    outcome: "failed",
    type: "engine/commandResult"
  });

  assert.deepEqual(settled.commands, []);
  assert.deepEqual(settled.followUpIntents, [
    {
      commandId: "send-1",
      commandType: "queue/sendPrompt",
      correlationId: "submit-1",
      errorCode: "settings_rejected",
      errorMessage: "settings rejected",
      outcome: "failed",
      type: "engine/commandResult"
    }
  ]);
});

test("a prompt without a settings patch sends directly", () => {
  const command = { ...promptCommand(), requiredSettingsPatch: undefined };
  const { requiredSettingsPatch: _settings, ...expected } = command;
  const requested = promptExecutionReducer(
    createInitialPromptExecutionState(),
    {
      command,
      type: "prompt/executionRequested",
      workspaceId: command.workspaceId
    }
  );

  assert.deepEqual(requested.commands, [expected]);
  assert.equal(requested.followUpIntents, undefined);
});

function requestPrompt() {
  const command = promptCommand();
  return promptExecutionReducer(createInitialPromptExecutionState(), {
    command,
    type: "prompt/executionRequested",
    workspaceId: command.workspaceId
  });
}

function promptCommand(): PromptQueueSendCommand {
  return {
    agentSessionId: "session-1",
    clientSubmitId: "submit-1",
    commandId: "send-1",
    content: [{ text: "hello", type: "text" }],
    correlationId: "submit-1",
    promptId: "prompt-1",
    requiredSettingsPatch: { browserUse: true },
    timeoutMs: 30_000,
    type: "queue/sendPrompt",
    workspaceId: "workspace-1"
  };
}
