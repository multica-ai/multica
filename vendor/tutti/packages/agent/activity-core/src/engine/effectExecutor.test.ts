import assert from "node:assert/strict";
import test from "node:test";
import { createEngineEffectExecutor } from "./effectExecutor.ts";
import { createTestEngineCommandPort } from "./testEngineCommandPort.ts";
import type { AgentActivitySession } from "../types.ts";
import type {
  AgentSessionEffectPort,
  EngineCommandResultIntent,
  EngineExternalCommand,
  EngineTypedCommandPort
} from "./types.ts";

test("projects shared commands onto typed lifecycle effects without host switches", async () => {
  const calls: Array<{
    commandId: string;
    input: unknown;
    kind: string;
    origin?: "engine";
    signal?: AbortSignal;
  }> = [];
  const session = {} as AgentActivitySession;
  const effects: AgentSessionEffectPort = {
    async activateSession(input, options) {
      calls.push({
        commandId: options.commandId,
        input,
        kind: "activate",
        origin: options.origin,
        signal: options.signal
      });
      return {
        activation: { mode: "new", status: "attached" },
        session
      };
    },
    async sendInput(input, options) {
      calls.push({
        commandId: options.commandId,
        input,
        kind: "send",
        origin: options.origin,
        signal: options.signal
      });
    },
    async updateSessionSettings(input, options) {
      calls.push({
        commandId: options.commandId,
        input,
        kind: "settings",
        origin: options.origin,
        signal: options.signal
      });
    }
  };
  let extensionCalls = 0;
  const port: EngineTypedCommandPort = {
    effects,
    kind: "typed",
    async execute() {
      extensionCalls += 1;
    }
  };

  const activationResult = await executeAndWait(port, {
    agentSessionId: "session-1",
    agentTargetId: "target-1",
    capabilityRefs: [{ capability: "tutti", source: "slash_command" }],
    clientSubmitId: "submit-create",
    commandId: "activate-1",
    correlationId: "submit-create",
    cwd: "/repo",
    isolation: "worktree",
    modelExplicit: false,
    initialContent: [{ text: "build it", type: "text" }],
    initialDisplayPrompt: "Build it",
    mode: "new",
    railPlacement: {
      kind: "project",
      projectPath: "/repo",
      sectionKey: "project:/repo",
      version: 1
    },
    reasoningEffortExplicit: true,
    settings: { model: "model-1", planMode: true },
    submitDiagnostics: { blockCount: 1, source: "test" },
    title: "Session",
    type: "session/activate",
    visible: false,
    workspaceId: "workspace-1"
  });
  assert.equal(activationResult.resultContract, "activation-v1");
  const sendResult = await executeAndWait(port, {
    agentSessionId: "session-1",
    capabilityRefs: [{ capability: "tutti", source: "slash_command" }],
    clientSubmitId: "submit-send",
    commandId: "send-1",
    content: [{ text: "continue", type: "text" }],
    correlationId: "submit-send",
    displayPrompt: "Continue",
    promptId: "prompt-1",
    type: "queue/sendPrompt",
    workspaceId: "workspace-1"
  });
  assert.equal(sendResult.resultContract, "opaque");
  await executeAndWait(port, {
    agentSessionId: "session-1",
    commandId: "settings-1",
    correlationId: "settings-1",
    settings: { speed: "fast" },
    type: "session/updateSettings",
    workspaceId: "workspace-1"
  });

  assert.equal(extensionCalls, 0);
  assert.deepEqual(
    calls.map((call) => call.kind),
    [
      "activate",
      "send",
      "settings",
    ]
  );
  assert.deepEqual(
    calls.map((call) => call.commandId),
    [
      "activate-1",
      "send-1",
      "settings-1",
    ]
  );
  assert.deepEqual(calls[0]?.input, {
    activationId: "submit-create",
    agentSessionId: "session-1",
    agentTargetId: "target-1",
    capabilityRefs: [{ capability: "tutti", source: "slash_command" }],
    clientSubmitId: "submit-create",
    cwd: "/repo",
    isolation: "worktree",
    modelExplicit: false,
    initialContent: [{ text: "build it", type: "text" }],
    initialDisplayPrompt: "Build it",
    mode: "new",
    railPlacement: {
      kind: "project",
      projectPath: "/repo",
      sectionKey: "project:/repo",
      version: 1
    },
    reasoningEffortExplicit: true,
    settings: { model: "model-1", planMode: true },
    submitDiagnostics: { blockCount: 1, source: "test" },
    title: "Session",
    visible: false,
    workspaceId: "workspace-1"
  });
  assert.deepEqual(calls[1]?.input, {
    agentSessionId: "session-1",
    capabilityRefs: [{ capability: "tutti", source: "slash_command" }],
    clientSubmitId: "submit-send",
    content: [{ text: "continue", type: "text" }],
    displayPrompt: "Continue",
    workspaceId: "workspace-1"
  });
  assert.deepEqual(calls[2]?.input, {
    agentSessionId: "session-1",
    commandId: "settings-1",
    correlationId: "settings-1",
    settings: { speed: "fast" },
    workspaceId: "workspace-1"
  });
  assert.ok(calls.every((call) => call.signal instanceof AbortSignal));
  assert.deepEqual(
    calls.map((call) => call.origin),
    ["engine", "engine", "engine"]
  );
});

test("rejects prompt settings preconditions that bypass the Engine state machine", async () => {
  let executed = false;
  const result = await executeAndWait(
    createTestEngineCommandPort(async () => {
      executed = true;
    }),
    {
      agentSessionId: "session-1",
      clientSubmitId: "submit-1",
      commandId: "send-1",
      content: [{ text: "continue", type: "text" }],
      promptId: "prompt-1",
      requiredSettingsPatch: { browserUse: true },
      type: "queue/sendPrompt",
      workspaceId: "workspace-1"
    }
  );

  assert.equal(executed, false);
  assert.equal(result.outcome, "failed");
  assert.match(
    result.errorMessage ?? "",
    /settings preconditions must be resolved by the Engine/
  );
});

function executeAndWait(
  commandPort: EngineTypedCommandPort,
  command: EngineExternalCommand
): Promise<EngineCommandResultIntent> {
  return new Promise((resolve) => {
    createEngineEffectExecutor({
      clock: { nowUnixMs: () => 1234 },
      commandPort,
      onResult: resolve,
      scheduler: {
        schedule() {
          return { cancel() {} };
        }
      }
    }).execute(command);
  });
}
