import assert from "node:assert/strict";
import test from "node:test";
import { createAgentActivitySnapshotProjector } from "./agentActivitySnapshot.projector.ts";
import {
  createInitialAgentSessionEngineState,
  rootEngineReducer
} from "./rootReducer.ts";
import { canonicalTurnKey } from "./sessionEntityKeys.ts";
import type { AgentActivitySession } from "../types.ts";

test("projects canonical engine state and preserves the snapshot reference", () => {
  const project = createAgentActivitySnapshotProjector("workspace-1");
  let state = createInitialAgentSessionEngineState();
  const empty = project(state);
  assert.equal(project(state), empty);

  state = rootEngineReducer(state, {
    type: "session/snapshotReceived",
    sessions: [session()]
  }).state;
  const populated = project(state);
  assert.notEqual(populated, empty);
  assert.equal(project(state), populated);
  assert.equal(populated.sessions[0]?.activeTurn?.turnId, "turn-1");
  assert.equal(populated.sessions[0]?.latestTurn?.turnId, "turn-1");
  assert.equal(populated.sessions[0]?.latestTurnInteractions.length, 1);
  assert.equal(populated.sessions[0]?.pendingInteractions.length, 1);
});

test("root reducer projects a historical session from canonical turns", () => {
  const source = session();
  const settledTurn = {
    ...source.latestTurn!,
    outcome: "completed" as const,
    phase: "settled" as const,
    settledAtUnixMs: 30,
    updatedAtUnixMs: 30
  };
  let state = rootEngineReducer(createInitialAgentSessionEngineState(), {
    type: "session/snapshotReceived",
    sessions: [
      {
        ...source,
        activeTurn: null,
        activeTurnId: null,
        endedAtUnixMs: 30,
        lastEventUnixMs: 30,
        latestTurn: settledTurn,
        latestTurnInteractions: [],
        pendingInteractions: [],
        updatedAtUnixMs: 30
      }
    ]
  }).state;

  const canonicalSession = state.sessionLifecycle.sessionsById["session-1"];
  assert.ok(canonicalSession);
  assert.equal("latestTurn" in canonicalSession, false);
  assert.equal(
    state.sessionLifecycle.turnsById[canonicalTurnKey("session-1", "turn-1")]
      ?.phase,
    "settled"
  );

});

test("reuses expensive projections when unrelated engine slices change", () => {
  const project = createAgentActivitySnapshotProjector("workspace-1");
  const populatedState = rootEngineReducer(
    createInitialAgentSessionEngineState(),
    {
      type: "session/snapshotReceived",
      sessions: [session()]
    }
  ).state;
  const populated = project(populatedState);
  const reconcileOnlyState = {
    ...populatedState,
    sessionReconcile: { ...populatedState.sessionReconcile }
  };

  const reconciled = project(reconcileOnlyState);

  assert.notEqual(reconciled, populated);
  assert.equal(reconciled.sessions, populated.sessions);
  assert.equal(
    reconciled.composerOptionsByTargetKey,
    populated.composerOptionsByTargetKey
  );
  assert.equal(
    reconciled.composerOptionsLoadStatusByTargetKey,
    populated.composerOptionsLoadStatusByTargetKey
  );
});

test("reuses the legacy Session array when only runtime activity changes", () => {
  const project = createAgentActivitySnapshotProjector("workspace-1");
  const populatedState = rootEngineReducer(
    createInitialAgentSessionEngineState(),
    {
      type: "session/snapshotReceived",
      sessions: [session()]
    }
  ).state;
  const populated = project(populatedState);
  const runtimeState = rootEngineReducer(populatedState, {
    type: "session/runtimeActivityChanged",
    agentSessionId: "session-1",
    state: "running",
    occurredAtUnixMs: 30
  }).state;

  assert.equal(project(runtimeState).sessions, populated.sessions);
});

function session(): AgentActivitySession {
  const turn = {
    agentSessionId: "session-1",
    completedCommand: null,
    error: null,
    fileChanges: null,
    outcome: null,
    origin: "user_prompt" as const,
    phase: "waiting" as const,
    settledAtUnixMs: null,
    startedAtUnixMs: 10,
    turnId: "turn-1",
    updatedAtUnixMs: 20
  };
  const interaction = {
    agentSessionId: "session-1",
    createdAtUnixMs: 15,
    input: {},
    kind: "approval" as const,
    metadata: {},
    requestId: "request-1",
    status: "pending" as const,
    toolName: "Bash",
    turnId: "turn-1",
    updatedAtUnixMs: 20
  };
  return {
    activeTurn: turn,
    activeTurnId: turn.turnId,
    agentSessionId: "session-1",
    kind: "root",
    rootAgentSessionId: null,
    rootTurnId: null,
    parentAgentSessionId: null,
    parentTurnId: null,
    parentToolCallId: null,
    agentTargetId: "agent-1",
    capabilities: null,
    lifecycleCapabilities: { fork: false, forkThroughTurn: false },
    forkedFrom: null,
    createdAtUnixMs: 1,
    cwd: "/workspace",
    isolation: null,
    endedAtUnixMs: null,
    imported: false,
    lastEventUnixMs: 20,
    latestTurn: turn,
    latestTurnInteractions: [interaction],
    messageVersion: 0,
    pendingInteractions: [interaction],
    permissionConfig: { configurable: false, modes: [] },
    pinnedAtUnixMs: null,
    provider: "codex",
    providerSessionId: null,
    resumable: true,
    settings: {},
    startedAtUnixMs: 1,
    title: "Session",
    updatedAtUnixMs: 20,
    usage: null,
    visible: true,
    workspaceId: "workspace-1"
  };
}
