import assert from "node:assert/strict";
import { test } from "node:test";
import {
  createInitialEngineRuntimeState,
  engineRuntimeReducer
} from "./engineRuntime.reducer.ts";
import {
  createInitialAgentSessionEngineState,
  rootEngineReducer
} from "./rootReducer.ts";
import { selectSessionHasUnconfirmedSubmit } from "./pendingIntents.selectors.ts";
import type { EngineIntent, EngineRuntimeState } from "./types.ts";
import type { AgentActivitySessionCapabilities } from "../types.ts";

function reduceAll(intents: readonly EngineIntent[]): EngineRuntimeState {
  let state = createInitialEngineRuntimeState();
  for (const intent of intents) {
    state = engineRuntimeReducer(state, intent).state;
  }
  return state;
}

test("initial engine runtime state is idle and empty", () => {
  assert.deepEqual(createInitialEngineRuntimeState(), {
    connection: "unknown",
    lastCommandResult: null,
    lastExpiredIntentId: null,
    processedIntentCount: 0,
    workspaceReconcile: {
      commandId: null,
      errorCode: null,
      errorMessage: null,
      status: "idle"
    }
  });
});

test("every intent increments the processed intent counter", () => {
  const state = reduceAll([
    { status: "connected", type: "engine/connectionChanged" },
    { probeId: "p-1", type: "engine/probeRequested" },
    {
      commandId: "p-1",
      commandType: "engine/probe",
      outcome: "succeeded",
      type: "engine/commandResult"
    },
    { dueAtUnixMs: 10, expiryId: "e-1", type: "engine/intentExpired" }
  ]);
  assert.equal(state.processedIntentCount, 4);
  assert.equal(state.connection, "connected");
  assert.deepEqual(state.lastCommandResult, {
    commandId: "p-1",
    outcome: "succeeded"
  });
  assert.equal(state.lastExpiredIntentId, "e-1");
});

test("probe request emits an external probe command", () => {
  const result = engineRuntimeReducer(createInitialEngineRuntimeState(), {
    probeId: "p-9",
    timeoutMs: 250,
    type: "engine/probeRequested"
  });
  assert.deepEqual(result.commands, [
    { commandId: "p-9", timeoutMs: 250, type: "engine/probe" }
  ]);
});

test("connected transition requests one workspace reconcile", () => {
  const initial = createInitialEngineRuntimeState();
  const connected = engineRuntimeReducer(initial, {
    status: "connected",
    type: "engine/connectionChanged",
    workspaceId: "workspace-1"
  });
  assert.equal(connected.commands[0]?.type, "engine/reconcileWorkspace");

  const duplicate = engineRuntimeReducer(connected.state, {
    status: "connected",
    type: "engine/connectionChanged",
    workspaceId: "workspace-1"
  });
  assert.equal(duplicate.commands.length, 0);

  const completed = engineRuntimeReducer(duplicate.state, {
    commandId: connected.state.workspaceReconcile.commandId ?? "",
    commandType: "engine/reconcileWorkspace",
    outcome: "succeeded",
    type: "engine/commandResult"
  });

  const disconnected = engineRuntimeReducer(completed.state, {
    status: "disconnected",
    type: "engine/connectionChanged",
    workspaceId: "workspace-1"
  });
  const reconnected = engineRuntimeReducer(disconnected.state, {
    status: "connected",
    type: "engine/connectionChanged",
    workspaceId: "workspace-1"
  });
  assert.equal(reconnected.commands[0]?.type, "engine/reconcileWorkspace");
});

test("workspace start loads once and failed load requires explicit retry", () => {
  const initial = engineRuntimeReducer(createInitialEngineRuntimeState(), {
    type: "workspace/reconcileRequested",
    workspaceId: "workspace-1"
  });
  assert.equal(initial.commands.length, 1);
  const duplicate = engineRuntimeReducer(initial.state, {
    type: "workspace/reconcileRequested",
    workspaceId: "workspace-1"
  });
  assert.equal(duplicate.commands.length, 0);
  const failed = engineRuntimeReducer(duplicate.state, {
    commandId: initial.state.workspaceReconcile.commandId ?? "",
    commandType: "engine/reconcileWorkspace",
    errorCode: "load_failed",
    outcome: "failed",
    type: "engine/commandResult"
  });
  assert.equal(failed.state.workspaceReconcile.status, "failed");
  assert.equal(
    engineRuntimeReducer(failed.state, {
      type: "workspace/reconcileRequested",
      workspaceId: "workspace-1"
    }).commands.length,
    0
  );
  const retry = engineRuntimeReducer(failed.state, {
    retry: true,
    type: "workspace/reconcileRequested",
    workspaceId: "workspace-1"
  });
  assert.equal(retry.commands.length, 1);
});

test("successful workspace reconcile hydrates state for active root sessions", () => {
  const requested = rootEngineReducer(createInitialAgentSessionEngineState(), {
    type: "workspace/reconcileRequested",
    workspaceId: "workspace-1"
  });
  const snapshotted = rootEngineReducer(requested.state, {
    sessions: [
      runningSession(capabilities({})),
      {
        activeTurnId: null,
        agentSessionId: "session-settled",
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "claude-code",
        title: "Settled",
        workspaceId: "workspace-1"
      }
    ],
    type: "session/snapshotReceived"
  });

  const reconciled = rootEngineReducer(snapshotted.state, {
    commandId: requested.state.engineRuntime.workspaceReconcile.commandId ?? "",
    commandType: "engine/reconcileWorkspace",
    outcome: "succeeded",
    type: "engine/commandResult"
  });

  assert.deepEqual(reconciled.commands, [
    {
      agentSessionId: "session-1",
      commandId: "session:reconcile:session-1:1",
      live: false,
      scope: "state",
      timeoutMs: 30_000,
      type: "session/reconcile",
      workspaceId: "workspace-1"
    }
  ]);
});

test("expiry request and cancellation emit internal clock commands", () => {
  const requested = engineRuntimeReducer(createInitialEngineRuntimeState(), {
    dueAtUnixMs: 500,
    expiryId: "e-7",
    type: "engine/expiryRequested"
  });
  assert.deepEqual(requested.commands, [
    { dueAtUnixMs: 500, expiryId: "e-7", type: "engine/scheduleExpiry" }
  ]);

  const canceled = engineRuntimeReducer(requested.state, {
    expiryId: "e-7",
    type: "engine/expiryCancelRequested"
  });
  assert.deepEqual(canceled.commands, [
    { expiryId: "e-7", type: "engine/cancelExpiry" }
  ]);
});

test("failed command results keep the error message", () => {
  const result = engineRuntimeReducer(createInitialEngineRuntimeState(), {
    commandId: "p-2",
    commandType: "engine/probe",
    errorMessage: "boom",
    outcome: "failed",
    type: "engine/commandResult"
  });
  assert.deepEqual(result.state.lastCommandResult, {
    commandId: "p-2",
    errorMessage: "boom",
    outcome: "failed"
  });
});

test("interleaving: a late command result does not clobber a newer expiry", () => {
  // submit probe -> expiry fires -> stale probe result arrives afterwards
  const state = reduceAll([
    { probeId: "p-1", type: "engine/probeRequested" },
    { dueAtUnixMs: 30, expiryId: "e-1", type: "engine/intentExpired" },
    {
      commandId: "p-1",
      commandType: "engine/probe",
      outcome: "timedOut",
      type: "engine/commandResult"
    }
  ]);
  assert.equal(state.lastExpiredIntentId, "e-1");
  assert.deepEqual(state.lastCommandResult, {
    commandId: "p-1",
    outcome: "timedOut"
  });
});

test("root reducer composes domain slices and commands", () => {
  const initial = createInitialAgentSessionEngineState();
  const result = rootEngineReducer(initial, {
    status: "disconnected",
    type: "engine/connectionChanged"
  });
  assert.equal(result.state.engineRuntime.connection, "disconnected");
  assert.notEqual(result.state, initial);
  assert.deepEqual(result.commands, []);

  const withCommand = rootEngineReducer(result.state, {
    probeId: "p-1",
    type: "engine/probeRequested"
  });
  assert.deepEqual(withCommand.commands, [
    { commandId: "p-1", type: "engine/probe" }
  ]);
});


test("immediate-routing submit still requires a canonical session", () => {
  const result = rootEngineReducer(createInitialAgentSessionEngineState(), {
    agentSessionId: "session-1",
    clientSubmitId: "submit-1",
    content: [{ type: "text", text: "now" }],
    expiresAtUnixMs: 120_000,
    requestedAtUnixMs: 1,
    routing: "immediate",
    type: "submit/requested",
    workspaceId: "workspace-1"
  });
  assert.deepEqual(result.commands, []);
  assert.equal(
    result.state.pendingIntents.submitsByClientSubmitId["submit-1"],
    undefined
  );
});



test("submit acceptance rejects cross-workspace canonical sessions atomically", () => {
  const submit = {
    agentSessionId: "session-1",
    clientSubmitId: "submit-1",
    content: [{ type: "text" as const, text: "hello" }],
    expiresAtUnixMs: 120_000,
    requestedAtUnixMs: 2,
    type: "submit/requested" as const,
    workspaceId: "workspace-1"
  };
  let state = rootEngineReducer(createInitialAgentSessionEngineState(), {
    sessions: [
      {
        ...{
          activeTurnId: null,
          latestTurnInteractions: [],
          pendingInteractions: []
        },
        agentSessionId: "session-1",
        cwd: "/workspace",
        provider: "codex",
        title: "Session",
        updatedAtUnixMs: 1,
        workspaceId: "workspace-2"
      }
    ],
    type: "session/snapshotReceived"
  }).state;
  const crossWorkspace = rootEngineReducer(state, submit);
  assert.equal(
    crossWorkspace.state.pendingIntents.submitsByClientSubmitId["submit-1"],
    undefined
  );
  assert.equal(
    crossWorkspace.state.promptQueue.recordsBySessionId["session-1"],
    undefined
  );
  assert.deepEqual(crossWorkspace.commands, []);
});



test("session tombstone blocks late queue and snapshot resurrection across domains", () => {
  let state = createInitialAgentSessionEngineState();
  state = rootEngineReducer(state, {
    type: "session/snapshotReceived",
    sessions: [
      {
        ...{
          activeTurnId: null,
          latestTurnInteractions: [],
          pendingInteractions: []
        },
        agentSessionId: "session-1",
        cwd: "/workspace",
        provider: "codex",
        title: "Session",
        updatedAtUnixMs: 1,
        workspaceId: "workspace-1"
      }
    ]
  }).state;
  state = rootEngineReducer(state, {
    type: "session/removed",
    agentSessionId: "session-1"
  }).state;
  const lateEnqueue = rootEngineReducer(state, {
    type: "queue/enqueued",
    agentSessionId: "session-1",
    prompt: {
      content: [{ type: "text", text: "late" }],
      createdAtUnixMs: 2,
      id: "prompt-1"
    },
    workspaceId: "workspace-1"
  });
  assert.equal(
    lateEnqueue.state.promptQueue.recordsBySessionId["session-1"],
    undefined
  );
  const lateSnapshot = rootEngineReducer(lateEnqueue.state, {
    type: "session/snapshotReceived",
    sessions: [
      {
        ...{
          activeTurnId: null,
          latestTurnInteractions: [],
          pendingInteractions: []
        },
        agentSessionId: "session-1",
        cwd: "/workspace",
        provider: "codex",
        title: "Session",
        updatedAtUnixMs: 3,
        workspaceId: "workspace-1"
      }
    ]
  });
  assert.equal(lateSnapshot.commands.length, 0);
  assert.equal(
    lateSnapshot.state.promptQueue.recordsBySessionId["session-1"],
    undefined
  );
  assert.equal(
    lateSnapshot.state.sessionLifecycle.sessionsById["session-1"],
    undefined
  );
  const lateActivity = rootEngineReducer(lateSnapshot.state, {
    type: "session/activityObserved",
    agentSessionId: "session-1",
    eventType: "session_reconcile_required",
    hasCachedSession: false,
    hasInlineMessages: false,
    inlineApplied: false,
    workspaceId: "workspace-1"
  });
  assert.equal(lateActivity.commands.length, 0);
  assert.equal(
    lateActivity.state.sessionReconcile.recordsBySessionId["session-1"],
    undefined
  );
});

test("an invalid send-now request cannot cancel an unrelated active turn", () => {
  let state = createInitialAgentSessionEngineState();
  state = rootEngineReducer(state, {
    type: "session/snapshotReceived",
    sessions: [
      {
        ...{
          activeTurnId: null,
          latestTurnInteractions: [],
          pendingInteractions: []
        },
        activeTurn: {
          agentSessionId: "session-1",
          origin: "user_prompt",
          phase: "running",
          startedAtUnixMs: 1,
          turnId: "turn-1",
          updatedAtUnixMs: 1
        },
        activeTurnId: "turn-1",
        agentSessionId: "session-1",
        cwd: "/workspace",
        provider: "codex",
        title: "Session",
        updatedAtUnixMs: 1,
        workspaceId: "workspace-1"
      }
    ]
  }).state;
  const invalid = rootEngineReducer(state, {
    type: "queue/sendNowRequested",
    agentSessionId: "session-1",
    awaitingTurnExpiresAtUnixMs: 30_000,
    cancelCommandId: "cancel-1",
    promptId: "missing-prompt",
    timeoutMs: 30_000
  });
  assert.equal(invalid.commands.length, 0);
});










function queuedSubmit(clientSubmitId: string, requestedAtUnixMs: number) {
  return {
    agentSessionId: "session-1",
    clientSubmitId,
    content: [{ type: "text" as const, text: clientSubmitId }],
    expiresAtUnixMs: requestedAtUnixMs + 120_000,
    requestedAtUnixMs,
    type: "submit/requested" as const,
    workspaceId: "workspace-1"
  };
}

function runningSession(capabilityList: AgentActivitySessionCapabilities) {
  return {
    activeTurn: {
      agentSessionId: "session-1",
      origin: "user_prompt" as const,
      phase: "running" as const,
      startedAtUnixMs: 1,
      turnId: "turn-1",
      updatedAtUnixMs: 1
    },
    activeTurnId: "turn-1",
    agentSessionId: "session-1",
    capabilities: capabilityList,
    cwd: "/workspace",
    latestTurnInteractions: [],
    pendingInteractions: [],
    provider: "opencode",
    title: "Session",
    updatedAtUnixMs: 1,
    workspaceId: "workspace-1"
  };
}

function sendNowSubmit(clientSubmitId: string) {
  return {
    agentSessionId: "session-1",
    clientSubmitId,
    content: [{ type: "text" as const, text: "inserted" }],
    expiresAtUnixMs: 120_000,
    requestedAtUnixMs: 2,
    routing: "send_now" as const,
    type: "submit/requested" as const,
    workspaceId: "workspace-1"
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
