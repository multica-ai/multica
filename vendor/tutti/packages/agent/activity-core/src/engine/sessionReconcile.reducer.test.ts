import assert from "node:assert/strict";
import test from "node:test";
import {
  createInitialSessionReconcileState,
  sessionReconcileReducer
} from "./sessionReconcile.reducer.ts";
import { selectEngineAuthoritativeHistoryRequirement } from "./sessionReconcile.selectors.ts";
import {
  createInitialAgentSessionEngineState,
  rootEngineReducer
} from "./rootReducer.ts";

test("an uncached initial read establishes a baseline without forcing full history", () => {
  const requirement = selectEngineAuthoritativeHistoryRequirement(
    createInitialAgentSessionEngineState(),
    {
      agentSessionId: "session-1",
      observedHistoryRevision: 0
    }
  );
  assert.deepEqual(requirement, {
    appliedHistoryRevision: null,
    minimumHistoryRevision: 0,
    needsAuthoritativeMessages: false
  });
});

test("historical detail turns preserve history provenance", () => {
  const turn = {
    agentSessionId: "session-1",
    origin: "user_prompt" as const,
    phase: "settled" as const,
    outcome: "completed" as const,
    settledAtUnixMs: 2,
    startedAtUnixMs: 1,
    turnId: "turn-1",
    updatedAtUnixMs: 2
  };
  const result = sessionReconcileReducer(createInitialSessionReconcileState(), {
    childSessions: [],
    session: {
      activeTurnId: null,
      agentSessionId: "session-1",
      cwd: "/workspace",
      latestTurnInteractions: [],
      pendingInteractions: [],
      provider: "codex",
      title: "Session",
      workspaceId: "workspace-1"
    },
    turns: [turn],
    type: "session/detailSnapshotReceived",
    workspaceId: "workspace-1"
  });

  assert.deepEqual(
    result.followUpIntents?.find((intent) => intent.type === "turn/upserted"),
    { live: false, turn, type: "turn/upserted" }
  );
});

test("live detail marks only the latest live turn as attention-capable", () => {
  const latestTurn = {
    agentSessionId: "session-1",
    origin: "user_prompt" as const,
    phase: "settled" as const,
    outcome: "completed" as const,
    settledAtUnixMs: 2,
    startedAtUnixMs: 1,
    turnId: "turn-2",
    updatedAtUnixMs: 2
  };
  const historicalTurn = { ...latestTurn, turnId: "turn-1" };
  const result = sessionReconcileReducer(createInitialSessionReconcileState(), {
    childSessions: [],
    live: true,
    session: {
      activeTurnId: null,
      agentSessionId: "session-1",
      cwd: "/workspace",
      latestTurn: latestTurn,
      latestTurnInteractions: [],
      pendingInteractions: [],
      provider: "codex",
      title: "Session",
      workspaceId: "workspace-1"
    },
    turns: [historicalTurn, latestTurn],
    type: "session/detailSnapshotReceived",
    workspaceId: "workspace-1"
  });

  assert.deepEqual(
    result.followUpIntents
      ?.filter((intent) => intent.type === "turn/upserted")
      .map((intent) => ({ live: intent.live, turnId: intent.turn.turnId })),
    [
      { live: true, turnId: "turn-2" },
      { live: false, turnId: "turn-1" },
      { live: false, turnId: "turn-2" }
    ]
  );
});

test("activity observation derives reconcile scope inside the engine", () => {
  const result = reduce(createInitialSessionReconcileState(), {
    type: "session/activityObserved",
    agentSessionId: "session-1",
    eventType: "message_update",
    hasCachedSession: true,
    hasInlineMessages: false,
    inlineApplied: false,
    workspaceId: "workspace-1"
  });
  assert.deepEqual(result.commands, [
    {
      agentSessionId: "session-1",
      commandId: "session:reconcile:session-1:1",
      live: false,
      scope: "state_and_messages",
      timeoutMs: 30_000,
      type: "session/reconcile",
      workspaceId: "workspace-1"
    }
  ]);
});

test("pending new sessions defer reconcile until their canonical session is committed", () => {
  const pendingNewSessionIds = new Set(["session-1"]);
  const pending = sessionReconcileReducer(
    createInitialSessionReconcileState(),
    {
      type: "session/activityObserved",
      agentSessionId: "session-1",
      eventType: "session_reconcile_required",
      hasCachedSession: false,
      hasInlineMessages: false,
      inlineApplied: false,
      workspaceId: "workspace-1"
    },
    {
      deletedSessionIds: {},
      pendingNewSessionIds,
      sessionsById: {},
      workspaceReconcileCommandId: null
    }
  );

  assert.deepEqual(pending.commands, []);
  assert.equal(
    pending.state.recordsBySessionId["session-1"]?.pendingState,
    true
  );

  const committed = sessionReconcileReducer(
    pending.state,
    {
      session: {
        agentSessionId: "session-1",
        workspaceId: "workspace-1"
      } as never,
      type: "session/upserted"
    },
    {
      deletedSessionIds: {},
      pendingNewSessionIds: new Set(),
      sessionsById: { "session-1": {} as never },
      workspaceReconcileCommandId: null
    }
  );

  assert.equal(committed.commands[0]?.type, "session/reconcile");
  assert.equal(
    committed.commands[0]?.type === "session/reconcile"
      ? committed.commands[0].agentSessionId
      : null,
    "session-1"
  );
});

test("timed out activation admits a recovery reconcile for queued demand", () => {
  let state = createInitialAgentSessionEngineState();
  state = rootEngineReducer(state, {
    agentSessionId: "session-1",
    agentTargetId: "target-1",
    clientSubmitId: "submit-1",
    expiresAtUnixMs: 120_000,
    mode: "new",
    requestedAtUnixMs: 1,
    requestId: "activation-1",
    type: "activation/requested",
    workspaceId: "workspace-1"
  }).state;

  const observed = rootEngineReducer(state, {
    agentSessionId: "session-1",
    eventType: "session_reconcile_required",
    hasCachedSession: false,
    hasInlineMessages: false,
    inlineApplied: false,
    type: "session/activityObserved",
    workspaceId: "workspace-1"
  });
  assert.deepEqual(observed.commands, []);
  assert.equal(
    observed.state.sessionReconcile.recordsBySessionId["session-1"]
      ?.pendingState,
    true
  );

  const timedOut = rootEngineReducer(observed.state, {
    commandId: "activate:activation-1",
    commandType: "session/activate",
    correlationId: "activation-1",
    outcome: "timedOut",
    type: "engine/commandResult"
  });
  const recoveryIntent = timedOut.followUpIntents?.find(
    (intent) => intent.type === "session/reconcileRequested"
  );
  assert.ok(recoveryIntent);

  const recovery = rootEngineReducer(timedOut.state, recoveryIntent);
  assert.equal(recovery.commands[0]?.type, "session/reconcile");
  assert.equal(
    recovery.commands[0]?.type === "session/reconcile"
      ? recovery.commands[0].agentSessionId
      : null,
    "session-1"
  );
});

test("streaming message gaps coalesce into one delayed reconcile and one trailing read", () => {
  const observation = {
    agentSessionId: "session-1",
    eventType: "message_update",
    hasCachedSession: true,
    hasInlineMessages: true,
    inlineApplied: false,
    type: "session/activityObserved" as const,
    workspaceId: "workspace-1"
  };
  let result = reduce(createInitialSessionReconcileState(), observation);

  assert.deepEqual(result.commands, [
    {
      delayMs: 50,
      expiryId: "session:streaming-message-reconcile:session-1",
      type: "engine/scheduleExpiryAfter"
    }
  ]);
  result = reduce(result.state, observation);
  assert.equal(result.commands.length, 0);

  result = reduce(result.state, {
    dueAtUnixMs: 50,
    expiryId: "session:streaming-message-reconcile:session-1",
    type: "engine/intentExpired"
  });
  assert.equal(result.commands[0]?.type, "session/reconcile");

  result = reduce(result.state, observation);
  assert.equal(result.commands.length, 0);

  result = reduce(result.state, {
    commandId: "session:reconcile:session-1:1",
    commandType: "session/reconcile",
    outcome: "succeeded",
    type: "engine/commandResult"
  });
  assert.deepEqual(result.commands, [
    {
      delayMs: 50,
      expiryId: "session:streaming-message-reconcile:session-1",
      type: "engine/scheduleExpiryAfter"
    }
  ]);
});

test("an explicit message reconcile bypasses a pending streaming delay", () => {
  const deferred = reduce(createInitialSessionReconcileState(), {
    agentSessionId: "session-1",
    eventType: "message_update",
    hasCachedSession: true,
    hasInlineMessages: true,
    inlineApplied: false,
    type: "session/activityObserved",
    workspaceId: "workspace-1"
  });

  const immediate = reduce(deferred.state, {
    agentSessionId: "session-1",
    needsMessages: true,
    needsState: false,
    type: "session/reconcileRequested",
    workspaceId: "workspace-1"
  });

  assert.equal(immediate.commands[0]?.type, "engine/cancelExpiry");
  assert.equal(immediate.commands[1]?.type, "session/reconcile");
});

test("inline-applied activity does not schedule redundant transport work", () => {
  const result = reduce(createInitialSessionReconcileState(), {
    type: "session/activityObserved",
    agentSessionId: "session-1",
    eventType: "turn_update",
    hasCachedSession: true,
    hasInlineMessages: false,
    inlineApplied: true,
    workspaceId: "workspace-1"
  });
  assert.equal(result.commands.length, 0);
});

test("terminal turn observation always verifies state and messages", () => {
  const result = reduce(createInitialSessionReconcileState(), {
    type: "session/activityObserved",
    agentSessionId: "session-1",
    eventType: "turn_update",
    hasCachedSession: true,
    hasInlineMessages: true,
    inlineApplied: true,
    terminalTurn: true,
    workspaceId: "workspace-1"
  });
  assert.deepEqual(result.commands, [
    {
      agentSessionId: "session-1",
      commandId: "session:reconcile:session-1:1",
      live: true,
      scope: "state_and_messages",
      timeoutMs: 30_000,
      type: "session/reconcile",
      workspaceId: "workspace-1"
    }
  ]);
});

test("reconcile requests merge while one command is in flight and rerun once", () => {
  let state = reduce(createInitialSessionReconcileState(), {
    type: "session/reconcileRequested",
    agentSessionId: "session-1",
    needsMessages: true,
    needsState: false,
    workspaceId: "workspace-1"
  }).state;
  const merged = reduce(state, {
    type: "session/reconcileRequested",
    agentSessionId: "session-1",
    needsMessages: false,
    needsState: true,
    workspaceId: "workspace-1"
  });
  assert.equal(merged.commands.length, 0);
  state = merged.state;
  const settled = reduce(state, {
    type: "engine/commandResult",
    commandId: "session:reconcile:session-1:1",
    commandType: "session/reconcile",
    outcome: "succeeded"
  });
  assert.deepEqual(settled.commands, [
    {
      agentSessionId: "session-1",
      commandId: "session:reconcile:session-1:2",
      live: false,
      scope: "state",
      timeoutMs: 30_000,
      type: "session/reconcile",
      workspaceId: "workspace-1"
    }
  ]);
});

test("session removal discards queued reconcile demand", () => {
  let state = reduce(createInitialSessionReconcileState(), {
    type: "session/reconcileRequested",
    agentSessionId: "session-1",
    needsMessages: true,
    needsState: true,
    workspaceId: "workspace-1"
  }).state;
  state = reduce(state, {
    type: "session/removed",
    agentSessionId: "session-1"
  }).state;
  assert.equal(state.recordsBySessionId["session-1"], undefined);
});

test("a timed-out reconcile releases merged demand into the next command", () => {
  let state = reduce(createInitialSessionReconcileState(), {
    type: "session/reconcileRequested",
    agentSessionId: "session-1",
    needsMessages: true,
    needsState: false,
    workspaceId: "workspace-1"
  }).state;
  state = reduce(state, {
    type: "session/reconcileRequested",
    agentSessionId: "session-1",
    needsMessages: false,
    needsState: true,
    workspaceId: "workspace-1"
  }).state;
  const timedOut = reduce(state, {
    type: "engine/commandResult",
    commandId: "session:reconcile:session-1:1",
    commandType: "session/reconcile",
    outcome: "timedOut"
  });
  assert.equal(timedOut.commands[0]?.type, "session/reconcile");
  assert.equal(
    timedOut.commands[0]?.type === "session/reconcile"
      ? timedOut.commands[0].scope
      : null,
    "state"
  );
});

test("a timed-out focused reconcile remains an observable synchronization failure", () => {
  let state = reduce(createInitialSessionReconcileState(), {
    type: "session/reconcileRequested",
    agentSessionId: "session-1",
    needsMessages: true,
    needsState: true,
    workspaceId: "workspace-1"
  }).state;

  state = reduce(state, {
    type: "engine/commandResult",
    commandId: "session:reconcile:session-1:1",
    commandType: "session/reconcile",
    outcome: "timedOut"
  }).state;

  assert.equal(state.recordsBySessionId["session-1"]?.errorCode, "timeout");
  assert.equal(
    state.recordsBySessionId["session-1"]?.errorMessage,
    "Session synchronization timed out."
  );
});

test("failed reconcile preserves typed error details for exact recovery", () => {
  let state = reduce(createInitialSessionReconcileState(), {
    type: "session/reconcileRequested",
    agentSessionId: "session-1",
    needsMessages: true,
    needsState: true,
    workspaceId: "workspace-1"
  }).state;
  state = reduce(state, {
    type: "engine/commandResult",
    commandId: "session:reconcile:session-1:1",
    commandType: "session/reconcile",
    errorCode: "session.not_found",
    errorMessage: "Session not found.",
    outcome: "failed"
  }).state;

  assert.deepEqual(state.recordsBySessionId["session-1"], {
    agentSessionId: "session-1",
    appliedHistoryRevision: null,
    authoritativeMessagesRequired: false,
    errorCode: "session.not_found",
    errorMessage: "Session not found.",
    inFlightCommandId: null,
    inFlightLive: false,
    inFlightScope: null,
    messageRefreshScheduled: false,
    messagesHydrated: false,
    pendingLive: false,
    pendingMessages: false,
    pendingMessagesImmediate: false,
    pendingState: false,
    requiredHistoryRevision: null,
    workspaceId: "workspace-1"
  });
});

test("a later reconcile request clears stale typed error details", () => {
  let state = reduce(createInitialSessionReconcileState(), {
    type: "session/reconcileRequested",
    agentSessionId: "session-1",
    needsMessages: false,
    needsState: true,
    workspaceId: "workspace-1"
  }).state;
  state = reduce(state, {
    type: "engine/commandResult",
    commandId: "session:reconcile:session-1:1",
    commandType: "session/reconcile",
    errorCode: "session.not_found",
    errorMessage: "Session not found.",
    outcome: "failed"
  }).state;

  const retried = reduce(state, {
    type: "session/reconcileRequested",
    agentSessionId: "session-1",
    needsMessages: false,
    needsState: true,
    workspaceId: "workspace-1"
  });

  assert.equal(retried.state.recordsBySessionId["session-1"]?.errorCode, null);
});

test("a merged retry does not expose the previous attempt's typed error", () => {
  let state = reduce(createInitialSessionReconcileState(), {
    type: "session/reconcileRequested",
    agentSessionId: "session-1",
    needsMessages: true,
    needsState: false,
    workspaceId: "workspace-1"
  }).state;
  state = reduce(state, {
    type: "session/reconcileRequested",
    agentSessionId: "session-1",
    needsMessages: false,
    needsState: true,
    workspaceId: "workspace-1"
  }).state;

  const retrying = reduce(state, {
    type: "engine/commandResult",
    commandId: "session:reconcile:session-1:1",
    commandType: "session/reconcile",
    errorCode: "session.not_found",
    errorMessage: "Session not found.",
    outcome: "failed"
  });

  assert.equal(retrying.commands[0]?.type, "session/reconcile");
  assert.equal(retrying.state.recordsBySessionId["session-1"]?.errorCode, null);
  assert.equal(
    retrying.state.recordsBySessionId["session-1"]?.errorMessage,
    null
  );
});

test("live Turn provenance forces state hydration after a failed reconcile", () => {
  let state = reduce(createInitialSessionReconcileState(), {
    type: "session/activityObserved",
    agentSessionId: "session-1",
    eventType: "turn_update",
    hasCachedSession: false,
    hasInlineMessages: false,
    inlineApplied: false,
    workspaceId: "workspace-1"
  }).state;
  assert.equal(state.recordsBySessionId["session-1"]?.inFlightLive, true);

  state = reduce(state, {
    type: "engine/commandResult",
    commandId: "session:reconcile:session-1:1",
    commandType: "session/reconcile",
    outcome: "failed"
  }).state;
  assert.equal(state.recordsBySessionId["session-1"]?.pendingLive, true);

  const retried = reduce(state, {
    type: "session/reconcileRequested",
    agentSessionId: "session-1",
    needsMessages: true,
    needsState: false,
    workspaceId: "workspace-1"
  });
  const command = retried.commands[0];
  assert.equal(command?.type, "session/reconcile");
  if (command?.type !== "session/reconcile") return;
  assert.equal(command.live, true);
  assert.equal(command.scope, "state_and_messages");
});

test("required history revision stays pending until a composite authoritative snapshot applies", () => {
  let result = reduce(createInitialSessionReconcileState(), {
    type: "session/reconcileRequested",
    agentSessionId: "session-1",
    needsMessages: false,
    needsState: false,
    requiredHistoryRevision: 4,
    workspaceId: "workspace-1"
  });
  assert.deepEqual(result.commands, [
    {
      agentSessionId: "session-1",
      authoritativeMessages: true,
      commandId: "session:reconcile:session-1:1",
      live: false,
      requiredHistoryRevision: 4,
      scope: "state_and_messages",
      timeoutMs: 30_000,
      type: "session/reconcile",
      workspaceId: "workspace-1"
    }
  ]);

  result = reduce(result.state, {
    agentSessionId: "session-1",
    childSessions: [],
    historyRevision: 4,
    messages: [],
    session: {
      agentSessionId: "session-1",
      workspaceId: "workspace-1"
    } as never,
    turns: [],
    type: "session/historyAuthoritativeSnapshotReceived",
    workspaceId: "workspace-1"
  });
  assert.equal(
    result.state.recordsBySessionId["session-1"]?.appliedHistoryRevision,
    4
  );
  assert.equal(
    result.state.recordsBySessionId["session-1"]?.authoritativeMessagesRequired,
    false
  );

  result = reduce(result.state, {
    type: "engine/commandResult",
    commandId: "session:reconcile:session-1:1",
    commandType: "session/reconcile",
    outcome: "succeeded"
  });
  assert.equal(result.commands.length, 0);
});

test("a history checkpoint initializes a complete inactive reconcile record", () => {
  const result = reduce(createInitialSessionReconcileState(), {
    agentSessionId: "session-1",
    historyRevision: 4,
    type: "session/historyRevisionObserved",
    workspaceId: "workspace-1"
  });

  assert.deepEqual(result.state.recordsBySessionId["session-1"], {
    agentSessionId: "session-1",
    appliedHistoryRevision: 4,
    authoritativeMessagesRequired: false,
    errorCode: null,
    errorMessage: null,
    inFlightCommandId: null,
    inFlightLive: false,
    inFlightScope: null,
    messageRefreshScheduled: false,
    messagesHydrated: false,
    pendingLive: false,
    pendingMessages: false,
    pendingMessagesImmediate: false,
    pendingState: false,
    requiredHistoryRevision: null,
    workspaceId: "workspace-1"
  });
});

test("authoritative demand arriving during an ordinary read retries exactly once after failure", () => {
  let result = reduce(createInitialSessionReconcileState(), {
    type: "session/reconcileRequested",
    agentSessionId: "session-1",
    needsMessages: true,
    needsState: true,
    workspaceId: "workspace-1"
  });
  result = reduce(result.state, {
    type: "session/reconcileRequested",
    agentSessionId: "session-1",
    authoritativeMessages: true,
    needsMessages: false,
    needsState: false,
    requiredHistoryRevision: 2,
    workspaceId: "workspace-1"
  });
  assert.equal(result.commands.length, 0);

  result = reduce(result.state, {
    type: "engine/commandResult",
    commandId: "session:reconcile:session-1:1",
    commandType: "session/reconcile",
    errorMessage: "revision changed",
    outcome: "failed"
  });
  assert.equal(result.commands[0]?.type, "session/reconcile");
  assert.equal(
    result.commands[0]?.type === "session/reconcile"
      ? result.commands[0].authoritativeMessages
      : false,
    true
  );

  result = reduce(result.state, {
    type: "engine/commandResult",
    commandId: "session:reconcile:session-1:2",
    commandType: "session/reconcile",
    errorMessage: "still unavailable",
    outcome: "failed"
  });
  assert.equal(result.commands.length, 0);
});

function reduce(
  state: ReturnType<typeof createInitialSessionReconcileState>,
  intent: Parameters<typeof sessionReconcileReducer>[1]
) {
  return sessionReconcileReducer(state, intent);
}
