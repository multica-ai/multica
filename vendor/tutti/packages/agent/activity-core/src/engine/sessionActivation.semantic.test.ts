import assert from "node:assert/strict";
import { test } from "node:test";
import { createAgentSessionEngine } from "./createAgentSessionEngine.ts";
import { createTestEngineCommandPort } from "./testEngineCommandPort.ts";
import type {
  EngineExternalCommand,
  EngineIntent,
  EngineScheduler
} from "./types.ts";

function createHarness() {
  const commands: EngineExternalCommand[] = [];
  const observedIntents: EngineIntent[] = [];
  const scheduled: Array<{ canceled: boolean; delayMs: number }> = [];
  const commandPort = createTestEngineCommandPort((command) => {
    commands.push(command);
    return new Promise(() => undefined);
  });
  const scheduler: EngineScheduler = {
    schedule(delayMs) {
      const task = { canceled: false, delayMs };
      scheduled.push(task);
      return {
        cancel() {
          task.canceled = true;
        }
      };
    }
  };
  const engine = createAgentSessionEngine({
    clock: { nowUnixMs: () => 100 },
    commandPort,
    identity: { origin: "test", workspaceId: "workspace-1" },
    intentObserver: (intent) => {
      observedIntents.push(intent);
    },
    scheduler
  });
  return { commands, engine, observedIntents, scheduled };
}

test("semantic new-Session activation owns scope, confirmation window, and command projection", () => {
  const harness = createHarness();

  const accepted = harness.engine.activateSession({
    agentSessionId: " session-new ",
    agentTargetId: " target-1 ",
    capabilityRefs: [{ capability: "tutti", source: "slash_command" }],
    clientSubmitId: " submit-new ",
    cwd: " /workspace ",
    isolation: "worktree",
    modelExplicit: false,
    initialContent: [{ text: "display instructions", type: "text" }],
    initialDisplayPrompt: " /browser ",
    initialTurnExpected: false,
    mode: "new",
    optimisticTitle: " Review browser flow ",
    railPlacement: {
      kind: "project",
      projectPath: "/workspace",
      sectionKey: "project:/workspace",
      version: 1
    },
    railSectionKey: " project:/workspace ",
    reasoningEffortExplicit: true,
    requestId: " activation-1 ",
    runtimeContent: [{ text: "runtime instructions", type: "text" }],
    settings: { model: "model-1" },
    submitDiagnostics: { source: "test", submittedAtUnixMs: 100 },
    title: " New session ",
    visible: true
  });

  assert.equal(accepted, true);
  assert.deepEqual(harness.observedIntents.at(-1), {
    agentSessionId: "session-new",
    agentTargetId: "target-1",
    capabilityRefs: [{ capability: "tutti", source: "slash_command" }],
    clientSubmitId: "submit-new",
    content: [{ text: "display instructions", type: "text" }],
    cwd: "/workspace",
    isolation: "worktree",
    modelExplicit: false,
    expiresAtUnixMs: 120_100,
    initialDisplayPrompt: "/browser",
    initialTurnExpected: false,
    mode: "new",
    optimisticTitle: "Review browser flow",
    railPlacement: {
      kind: "project",
      projectPath: "/workspace",
      sectionKey: "project:/workspace",
      version: 1
    },
    railSectionKey: "project:/workspace",
    reasoningEffortExplicit: true,
    requestId: "activation-1",
    requestedAtUnixMs: 100,
    runtimeContent: [{ text: "runtime instructions", type: "text" }],
    settings: { model: "model-1" },
    submitDiagnostics: { source: "test", submittedAtUnixMs: 100 },
    title: "New session",
    type: "activation/requested",
    visible: true,
    workspaceId: "workspace-1"
  });
  assert.deepEqual(harness.commands, [
    {
      agentSessionId: "session-new",
      agentTargetId: "target-1",
      capabilityRefs: [{ capability: "tutti", source: "slash_command" }],
      clientSubmitId: "submit-new",
      commandId: "activate:activation-1",
      correlationId: "activation-1",
      cwd: "/workspace",
      isolation: "worktree",
      modelExplicit: false,
      initialContent: [{ text: "runtime instructions", type: "text" }],
      initialDisplayPrompt: "/browser",
      mode: "new",
      railPlacement: {
        kind: "project",
        projectPath: "/workspace",
        sectionKey: "project:/workspace",
        version: 1
      },
      reasoningEffortExplicit: true,
      settings: { model: "model-1" },
      submitDiagnostics: { source: "test", submittedAtUnixMs: 100 },
      timeoutMs: 90_000,
      title: "New session",
      type: "session/activate",
      visible: true,
      workspaceId: "workspace-1"
    }
  ]);
  assert.deepEqual(
    harness.scheduled.map((task) => task.delayMs),
    [120_000, 90_000]
  );
});

test("semantic existing-Session activation uses the shared confirmation window", () => {
  const harness = createHarness();

  const accepted = harness.engine.activateSession({
    agentSessionId: " session-1 ",
    agentTargetId: " target-1 ",
    mode: "existing",
    requestId: " activation-existing "
  });

  assert.equal(accepted, true);
  assert.deepEqual(harness.commands, [
    {
      agentSessionId: "session-1",
      agentTargetId: "target-1",
      commandId: "activate:activation-existing",
      correlationId: "activation-existing",
      mode: "existing",
      timeoutMs: 30_000,
      type: "session/activate",
      workspaceId: "workspace-1"
    }
  ]);
  assert.deepEqual(
    harness.scheduled.map((task) => task.delayMs),
    [120_000, 30_000]
  );
});

test("semantic activation does not admit changed input under a reused request identity", () => {
  const harness = createHarness();

  const firstAccepted = harness.engine.activateSession({
    agentSessionId: "session-new",
    agentTargetId: "target-1",
    clientSubmitId: "submit-1",
    initialContent: [{ text: "first prompt", type: "text" }],
    mode: "new",
    requestId: "activation-1"
  });
  const reusedAccepted = harness.engine.activateSession({
    agentSessionId: "session-new",
    agentTargetId: "target-2",
    clientSubmitId: "submit-2",
    initialContent: [{ text: "changed prompt", type: "text" }],
    mode: "new",
    requestId: "activation-1"
  });

  assert.equal(firstAccepted, true);
  assert.equal(reusedAccepted, false);
  assert.equal(harness.commands.length, 1);
  assert.deepEqual(
    harness.engine.getSnapshot().pendingIntents.activationsByRequestId[
      "activation-1"
    ],
    {
      agentSessionId: "session-new",
      agentTargetId: "target-1",
      clientSubmitId: "submit-1",
      commandOutcome: "pending",
      commandSettledAtUnixMs: null,
      content: [{ text: "first prompt", type: "text" }],
      cwd: "",
      errorCode: null,
      errorMessage: null,
      expiresAtUnixMs: 120_100,
      initialPromptRetracted: false,
      initialTurnExpected: true,
      lastObservedStage: "requested",
      mode: "new",
      requestedAtUnixMs: 100,
      requestId: "activation-1",
      snapshotObservedAtUnixMs: null,
      snapshotOutcome: "not_observed",
      status: "requested",
      title: null,
      workspaceId: "workspace-1"
    }
  );
  assert.deepEqual(
    harness.scheduled.map((task) => task.delayMs),
    [120_000, 90_000]
  );
});

test("semantic activation rejects invalid identity without side effects", () => {
  const harness = createHarness();

  const accepted = harness.engine.activateSession({
    agentSessionId: "session-new",
    agentTargetId: " ",
    clientSubmitId: "submit-new",
    mode: "new",
    requestId: "activation-1"
  });

  assert.equal(accepted, false);
  assert.deepEqual(harness.observedIntents, []);
  assert.deepEqual(harness.commands, []);
  assert.deepEqual(harness.scheduled, []);
});
