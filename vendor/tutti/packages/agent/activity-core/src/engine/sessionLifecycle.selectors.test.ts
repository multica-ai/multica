import assert from "node:assert/strict";
import test from "node:test";
import {
  createInitialAgentSessionEngineState,
  rootEngineReducer
} from "./rootReducer.ts";
import {
  selectEngineSessionOperationError,
  selectEngineSessionCanReload,
  selectEngineSubmitAvailability,
  selectFailedNewActivationResolution,
  selectEngineSessionDeleted,
  selectRootAgentSessionIdsWithPendingInteractions,
  selectWorkspaceAgentConsumerCounts,
  selectWorkspaceAgentConsumerSession,
  selectWorkspaceAgentConsumerSessions,
  selectWorkspaceAgentRootConversationSessions
} from "./sessionLifecycle.selectors.ts";

test("session operation errors do not fall back to canonical Turn errors", () => {
  let state = rootEngineReducer(createInitialAgentSessionEngineState(), {
    sessions: [
      {
        activeTurn: {
          agentSessionId: "session-1",
          error: { message: "Selected model is at capacity" },
          origin: "user_prompt",
          phase: "running",
          startedAtUnixMs: 10,
          turnId: "turn-1",
          updatedAtUnixMs: 20
        },
        activeTurnId: "turn-1",
        agentSessionId: "session-1",
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "codex",
        title: "Canonical session",
        workspaceId: "workspace-1"
      }
    ],
    type: "session/snapshotReceived"
  }).state;

  assert.equal(selectEngineSessionOperationError(state, "session-1"), null);

  state = rootEngineReducer(state, {
    agentSessionId: "session-1",
    errorMessage: "Settings update failed",
    type: "session/errorRecorded"
  }).state;
  assert.equal(
    selectEngineSessionOperationError(state, "session-1"),
    "Settings update failed"
  );
});

test("deleted session selector normalizes ids and hides tombstone storage", () => {
  const state = rootEngineReducer(createInitialAgentSessionEngineState(), {
    agentSessionId: "session-1",
    type: "session/removed"
  }).state;

  assert.equal(selectEngineSessionDeleted(state, " session-1 "), true);
  assert.equal(selectEngineSessionDeleted(state, "missing"), false);
  assert.equal(selectEngineSessionDeleted(state, null), false);
});

test("failed new activation preserves a canonical session after runtime failure", () => {
  let state = createInitialAgentSessionEngineState();
  assert.equal(
    selectFailedNewActivationResolution(state, "session-1"),
    "not-applicable"
  );

  state = rootEngineReducer(state, {
    agentSessionId: "session-1",
    agentTargetId: "target-1",
    clientSubmitId: "submit-1",
    content: [{ type: "text", text: "hello" }],
    cwd: "/workspace",
    expiresAtUnixMs: 45_001,
    mode: "new",
    requestedAtUnixMs: 1,
    requestId: "activation-1",
    type: "activation/requested",
    workspaceId: "workspace-1"
  }).state;
  state = rootEngineReducer(state, {
    commandId: "activate:activation-1",
    commandType: "session/activate",
    correlationId: "activation-1",
    errorMessage: "Initial goal failed.",
    outcome: "failed",
    type: "engine/commandResult"
  }).state;
  assert.equal(
    selectFailedNewActivationResolution(state, "session-1"),
    "rollback"
  );

  state = rootEngineReducer(state, {
    sessions: [
      {
        activeTurnId: null,
        agentSessionId: "session-1",
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "codex",
        title: "Persisted session",
        workspaceId: "workspace-1"
      }
    ],
    type: "session/snapshotReceived"
  }).state;

  assert.equal(
    selectFailedNewActivationResolution(state, " session-1 "),
    "preserve"
  );
  assert.equal(
    selectFailedNewActivationResolution(state, "session-1", {
      selectionSource: "user-selection"
    }),
    "not-applicable"
  );
  assert.equal(selectEngineSessionCanReload(state, "session-1"), true);
});

test("consumer collections hide presentation-invisible sessions without removing exact lookup", () => {
  const state = rootEngineReducer(createInitialAgentSessionEngineState(), {
    sessions: [
      {
        activeTurnId: null,
        agentSessionId: "visible-session",
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "codex",
        title: "Visible session",
        visible: true,
        workspaceId: "workspace-1"
      },
      {
        activeTurnId: null,
        agentSessionId: "hidden-session",
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "codex",
        title: "Hidden session",
        visible: false,
        workspaceId: "workspace-1"
      }
    ],
    type: "session/snapshotReceived"
  }).state;

  assert.deepEqual(
    selectWorkspaceAgentConsumerSessions(state).map(
      (item) => item.session.agentSessionId
    ),
    ["visible-session"]
  );
  assert.equal(
    selectWorkspaceAgentConsumerSession(state, "hidden-session")?.session
      .agentSessionId,
    "hidden-session"
  );
});

test("consumer status is derived from canonical Turn and Interaction entities", () => {
  let state = createInitialAgentSessionEngineState();
  state = rootEngineReducer(state, {
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
          startedAtUnixMs: 10,
          turnId: "turn-1",
          updatedAtUnixMs: 20
        },
        activeTurnId: "turn-1",
        agentSessionId: "session-1",
        cwd: "/workspace",
        pendingInteractions: [
          {
            agentSessionId: "session-1",
            createdAtUnixMs: 21,
            input: { question: "Choose an option" },
            kind: "question",
            requestId: "request-1",
            status: "pending",
            turnId: "turn-1",
            updatedAtUnixMs: 21
          }
        ],
        provider: "codex",
        title: "Canonical session",
        workspaceId: "workspace-1"
      }
    ],
    type: "session/snapshotReceived"
  }).state;

  const consumer = selectWorkspaceAgentConsumerSession(state, "session-1");
  assert.equal(consumer?.displayStatus, "waiting");
  assert.equal(consumer?.session.activeTurnId, "turn-1");
  assert.equal("status" in (consumer?.session ?? {}), false);
  assert.equal("pendingInteractions" in (consumer?.session ?? {}), false);
  assert.deepEqual(selectWorkspaceAgentConsumerCounts(state), {
    canceled: 0,
    completed: 0,
    failed: 0,
    idle: 0,
    waiting: 1,
    working: 0
  });
});

test("new activation remains idle until canonical execution appears", () => {
  let state = createInitialAgentSessionEngineState();
  state = rootEngineReducer(state, {
    agentSessionId: "session-1",
    agentTargetId: "local:codex",
    clientSubmitId: "submit-1",
    content: [{ type: "text", text: "test1" }],
    cwd: "/workspace",
    expiresAtUnixMs: 1_000,
    mode: "new",
    requestedAtUnixMs: 10,
    requestId: "activation-1",
    type: "activation/requested",
    workspaceId: "workspace-1"
  }).state;
  state = rootEngineReducer(state, {
    sessions: [
      {
        activeTurnId: null,
        agentSessionId: "session-1",
        createdAtUnixMs: 20,
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "codex",
        title: "test1",
        workspaceId: "workspace-1"
      }
    ],
    type: "session/snapshotReceived"
  }).state;

  assert.equal(
    selectWorkspaceAgentConsumerSession(state, "session-1")?.displayStatus,
    "idle"
  );

  state = rootEngineReducer(state, {
    sessions: [
      {
        activeTurn: {
          agentSessionId: "session-1",
          origin: "user_prompt",
          phase: "running",
          startedAtUnixMs: 20,
          turnId: "turn-1",
          updatedAtUnixMs: 20
        },
        activeTurnId: "turn-1",
        agentSessionId: "session-1",
        createdAtUnixMs: 20,
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "codex",
        title: "test1",
        workspaceId: "workspace-1"
      }
    ],
    type: "session/snapshotReceived"
  }).state;

  assert.equal(
    selectWorkspaceAgentConsumerSession(state, "session-1")?.displayStatus,
    "working"
  );

  state = rootEngineReducer(state, {
    sessions: [
      {
        activeTurnId: null,
        agentSessionId: "session-1",
        createdAtUnixMs: 20,
        cwd: "/workspace",
        latestTurn: {
          agentSessionId: "session-1",
          origin: "user_prompt",
          outcome: "completed",
          phase: "settled",
          startedAtUnixMs: 10,
          turnId: "turn-1",
          updatedAtUnixMs: 30
        },
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "codex",
        title: "test1",
        workspaceId: "workspace-1"
      }
    ],
    type: "session/snapshotReceived"
  }).state;

  assert.equal(
    selectWorkspaceAgentConsumerSession(state, "session-1")?.displayStatus,
    "completed"
  );
});

test("new activation without initial content stays idle after session confirmation", () => {
  let state = createInitialAgentSessionEngineState();
  state = rootEngineReducer(state, {
    agentSessionId: "session-1",
    agentTargetId: "local:codex",
    clientSubmitId: "submit-1",
    cwd: "/workspace",
    expiresAtUnixMs: 1_000,
    mode: "new",
    requestedAtUnixMs: 10,
    requestId: "activation-1",
    type: "activation/requested",
    workspaceId: "workspace-1"
  }).state;
  state = rootEngineReducer(state, {
    sessions: [
      {
        activeTurnId: null,
        agentSessionId: "session-1",
        createdAtUnixMs: 20,
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "codex",
        title: "",
        workspaceId: "workspace-1"
      }
    ],
    type: "session/snapshotReceived"
  }).state;

  assert.equal(
    selectWorkspaceAgentConsumerSession(state, "session-1")?.displayStatus,
    "idle"
  );
});

test("completed Claude goal does not unlock a waiting root with a running child", () => {
  let state = createInitialAgentSessionEngineState();
  state = rootEngineReducer(state, {
    sessions: [
      {
        activeTurn: {
          agentSessionId: "root",
          origin: "user_prompt",
          phase: "waiting",
          startedAtUnixMs: 10,
          turnId: "root-turn",
          updatedAtUnixMs: 30
        },
        activeTurnId: "root-turn",
        agentSessionId: "root",
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "claude-code",
        title: "Root",
        workspaceId: "workspace-1"
      },
      {
        activeTurn: {
          agentSessionId: "child",
          origin: "provider_initiated",
          phase: "running",
          startedAtUnixMs: 20,
          turnId: "child-turn",
          updatedAtUnixMs: 30
        },
        activeTurnId: "child-turn",
        agentSessionId: "child",
        cwd: "/workspace",
        kind: "child",
        latestTurnInteractions: [],
        parentAgentSessionId: "root",
        parentToolCallId: "toolu-1",
        parentTurnId: "root-turn",
        pendingInteractions: [],
        provider: "claude-code",
        rootAgentSessionId: "root",
        rootTurnId: "root-turn",
        title: "Child",
        workspaceId: "workspace-1"
      }
    ],
    type: "session/snapshotReceived"
  }).state;

  assert.equal(
    selectWorkspaceAgentConsumerSession(state, "root")?.displayStatus,
    "waiting"
  );
  assert.deepEqual(selectEngineSubmitAvailability(state, "root"), {
    state: "blocked",
    reason: "active_turn"
  });
  assert.deepEqual(selectWorkspaceAgentConsumerCounts(state), {
    canceled: 0,
    completed: 0,
    failed: 0,
    idle: 0,
    waiting: 1,
    working: 0
  });
});

test("pending child interactions are attributed to their root session", () => {
  let state = createInitialAgentSessionEngineState();
  state = rootEngineReducer(state, {
    sessions: [
      {
        activeTurnId: null,
        agentSessionId: "root",
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "claude-code",
        title: "Root",
        workspaceId: "workspace-1"
      },
      {
        activeTurn: {
          agentSessionId: "child",
          origin: "provider_initiated",
          phase: "waiting",
          startedAtUnixMs: 20,
          turnId: "child-turn",
          updatedAtUnixMs: 21
        },
        activeTurnId: "child-turn",
        agentSessionId: "child",
        cwd: "/workspace",
        kind: "child",
        latestTurnInteractions: [],
        parentAgentSessionId: "root",
        parentToolCallId: "toolu-1",
        parentTurnId: "root-turn",
        pendingInteractions: [
          {
            agentSessionId: "child",
            createdAtUnixMs: 21,
            input: { question: "Choose an option" },
            kind: "question",
            requestId: "request-1",
            status: "pending",
            turnId: "child-turn",
            updatedAtUnixMs: 21
          }
        ],
        provider: "claude-code",
        rootAgentSessionId: "root",
        rootTurnId: "root-turn",
        title: "Child",
        workspaceId: "workspace-1"
      }
    ],
    type: "session/snapshotReceived"
  }).state;

  assert.deepEqual(selectRootAgentSessionIdsWithPendingInteractions(state), [
    "root"
  ]);
});

test("new goal control activation stays idle without a provider Turn", () => {
  let state = createInitialAgentSessionEngineState();
  state = rootEngineReducer(state, {
    agentSessionId: "session-goal",
    agentTargetId: "local:claude-code",
    clientSubmitId: "submit-goal",
    content: [{ type: "text", text: "/goal ship it" }],
    cwd: "/workspace",
    expiresAtUnixMs: 1_000,
    initialTurnExpected: false,
    mode: "new",
    requestedAtUnixMs: 10,
    requestId: "activation-goal",
    runtimeContent: [{ type: "text", text: "/goal ship it" }],
    type: "activation/requested",
    workspaceId: "workspace-1"
  }).state;
  state = rootEngineReducer(state, {
    sessions: [
      {
        activeTurnId: null,
        agentSessionId: "session-goal",
        createdAtUnixMs: 20,
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "claude-code",
        title: "/goal ship it",
        workspaceId: "workspace-1"
      }
    ],
    type: "session/snapshotReceived"
  }).state;

  assert.equal(
    selectWorkspaceAgentConsumerSession(state, "session-goal")?.displayStatus,
    "idle"
  );
});

test("root conversation selector aggregates descendant pending interactions", () => {
  const state = rootEngineReducer(createInitialAgentSessionEngineState(), {
    sessions: [
      {
        activeTurn: {
          agentSessionId: "root",
          origin: "user_prompt",
          phase: "running",
          startedAtUnixMs: 10,
          turnId: "root-turn",
          updatedAtUnixMs: 20
        },
        activeTurnId: "root-turn",
        agentSessionId: "root",
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "claude-code",
        title: "Root",
        workspaceId: "workspace-1"
      },
      {
        activeTurn: {
          agentSessionId: "child",
          origin: "provider_initiated",
          phase: "waiting",
          startedAtUnixMs: 15,
          turnId: "child-turn",
          updatedAtUnixMs: 25
        },
        activeTurnId: "child-turn",
        agentSessionId: "child",
        cwd: "/workspace",
        kind: "child",
        latestTurnInteractions: [],
        parentAgentSessionId: "root",
        parentToolCallId: "toolu-1",
        parentTurnId: "root-turn",
        pendingInteractions: [
          {
            agentSessionId: "child",
            createdAtUnixMs: 26,
            input: { question: "Allow Bash?" },
            kind: "approval",
            requestId: "approval-1",
            status: "pending",
            toolName: "Bash",
            turnId: "child-turn",
            updatedAtUnixMs: 26
          }
        ],
        provider: "claude-code",
        rootAgentSessionId: "root",
        rootTurnId: "root-turn",
        title: "Child",
        workspaceId: "workspace-1"
      }
    ],
    type: "session/snapshotReceived"
  }).state;

  const conversations = selectWorkspaceAgentRootConversationSessions(state);
  assert.equal(conversations.length, 1);
  assert.equal(conversations[0]?.session.agentSessionId, "root");
  assert.equal(conversations[0]?.displayStatus, "waiting");
  assert.deepEqual(
    conversations[0]?.pendingInteractions.map((interaction) => [
      interaction.agentSessionId,
      interaction.turnId,
      interaction.requestId
    ]),
    [["child", "child-turn", "approval-1"]]
  );
});

test("root conversation stays working while a descendant turn is running", () => {
  const state = rootEngineReducer(createInitialAgentSessionEngineState(), {
    sessions: [
      {
        activeTurnId: null,
        agentSessionId: "root",
        cwd: "/workspace",
        latestTurn: {
          agentSessionId: "root",
          origin: "user_prompt",
          outcome: "completed",
          phase: "settled",
          settledAtUnixMs: 20,
          startedAtUnixMs: 10,
          turnId: "root-turn",
          updatedAtUnixMs: 20
        },
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "claude-code",
        title: "Root",
        workspaceId: "workspace-1"
      },
      {
        activeTurn: {
          agentSessionId: "child",
          origin: "provider_initiated",
          phase: "running",
          startedAtUnixMs: 15,
          turnId: "child-turn",
          updatedAtUnixMs: 25
        },
        activeTurnId: "child-turn",
        agentSessionId: "child",
        cwd: "/workspace",
        kind: "child",
        latestTurnInteractions: [],
        parentAgentSessionId: "root",
        parentToolCallId: "toolu-1",
        parentTurnId: "root-turn",
        pendingInteractions: [],
        provider: "claude-code",
        rootAgentSessionId: "root",
        rootTurnId: "root-turn",
        title: "Child",
        workspaceId: "workspace-1"
      }
    ],
    type: "session/snapshotReceived"
  }).state;

  assert.equal(
    selectWorkspaceAgentRootConversationSessions(state)[0]?.displayStatus,
    "working"
  );
});
