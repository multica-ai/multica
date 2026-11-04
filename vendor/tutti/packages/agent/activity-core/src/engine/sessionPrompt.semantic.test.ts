import assert from "node:assert/strict";
import { test } from "node:test";
import { normalizeAgentActivitySession } from "../sessionNormalization.ts";
import type {
  AgentActivityInteraction,
  AgentActivitySessionCapabilities,
  AgentActivityTurn
} from "../types.ts";
import { createAgentSessionEngine } from "./createAgentSessionEngine.ts";
import { createTestEngineCommandPort } from "./testEngineCommandPort.ts";
import type {
  EngineExternalCommand,
  EngineIntent,
  EngineScheduler
} from "./types.ts";

function createHarness(active: boolean) {
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
  engine.dispatch({
    sessions: [session(active ? activeTurn() : null)],
    type: "session/snapshotReceived"
  });
  return { commands, engine, observedIntents, scheduled };
}











test("semantic prompt submission rejects an invalid identity without side effects", () => {
  const harness = createHarness(false);

  const result = harness.engine.submitPrompt({
    agentSessionId: " ",
    clientSubmitId: "submit-1",
    content: [{ text: "continue", type: "text" }]
  });

  assert.deepEqual(result, { accepted: false, queued: false });
  assert.deepEqual(harness.commands, []);
  assert.deepEqual(harness.scheduled, []);
});

function activeTurn(): AgentActivityTurn {
  return {
    agentSessionId: "session-1",
    origin: "user_prompt",
    phase: "running",
    startedAtUnixMs: 1,
    turnId: "turn-1",
    updatedAtUnixMs: 2
  };
}

function session(turn: AgentActivityTurn | null) {
  return normalizeAgentActivitySession({
    activeTurn: turn,
    activeTurnId: turn?.turnId ?? null,
    agentSessionId: "session-1",
    cwd: "/workspace",
    latestTurn: turn,
    latestTurnInteractions: [],
    pendingInteractions: [],
    provider: "codex",
    title: "Session",
    workspaceId: "workspace-1"
  });
}

function sessionWithCapabilities(
  turn: AgentActivityTurn,
  sessionCapabilities: AgentActivitySessionCapabilities
) {
  const activeTurn = turn.phase === "settled" ? null : turn;
  return {
    ...session(activeTurn),
    activeTurn,
    activeTurnId: activeTurn?.turnId ?? null,
    capabilities: sessionCapabilities,
    latestTurn: turn,
    updatedAtUnixMs: turn.updatedAtUnixMs
  };
}

function capabilities(
  overrides: Partial<AgentActivitySessionCapabilities>
): AgentActivitySessionCapabilities {
  return {
    activeTurnGuidance: false,
    browserUse: false,
    compact: false,
    computerUse: false,
    goalPause: false,
    imageInput: false,
    interrupt: false,
    modelImageInputRequired: false,
    modelPlanBinding: false,
    modelSwitch: false,
    permissionModeChangeDeferred: false,
    permissionModeChangeDuringTurn: false,
    planImplementation: false,
    planMode: false,
    rateLimits: false,
    resumeRunningTurn: false,
    review: false,
    skills: false,
    tokenUsage: false,
    ...overrides
  };
}
