import assert from "node:assert/strict";
import test from "node:test";
import type {
  AgentActivityInteraction,
  AgentActivitySession,
  AgentActivityTurn
} from "../types.ts";
import { normalizeAgentActivitySession } from "../sessionNormalization.ts";
import {
  createInitialSessionLifecycleState,
  sessionLifecycleReducer
} from "./sessionLifecycle.reducer.ts";
import {
  canonicalInteractionKey,
  canonicalTurnKey
} from "./sessionEntityKeys.ts";
import {
  selectEngineInteraction,
  selectEngineTurn
} from "./sessionLifecycle.selectors.ts";
import { createInitialAgentSessionEngineState } from "./rootReducer.ts";
import { validateSendInputResult } from "./commandResult.validation.ts";
import { deriveCanonicalSubmitAvailability } from "./sessionLifecycle.availability.ts";

test("snapshot decomposes protocol v2 session and turn entities", () => {
  const result = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [session(activeTurn(2), 2)]
  });
  assert.equal(result.state.sessionsById["session-1"]?.activeTurnId, "turn-1");
  assert.equal(
    result.state.turnsById[canonicalTurnKey("session-1", "turn-1")]?.phase,
    "running"
  );
  assert.equal("recordsBySessionId" in result.state, false);
  assert.equal(
    "activeTurn" in (result.state.sessionsById["session-1"] ?? {}),
    false
  );
  assert.equal(
    "turnLifecycle" in (result.state.sessionsById["session-1"] ?? {}),
    false
  );
});

test("authoritative turn history removes a retracted turn from canonical state", () => {
  const source = session(activeTurn(2), 2);
  let state = reduce(createInitialSessionLifecycleState(), {
    type: "session/upserted",
    session: source
  }).state;
  assert.ok(state.turnsById[canonicalTurnKey("session-1", "turn-1")]);

  state = reduce(state, {
    agentSessionId: "session-1",
    childSessions: [],
    historyRevision: 1,
    messages: [],
    session: {
      ...session(null, 3),
      latestTurn: source.latestTurn
    },
    turns: [],
    type: "session/historyAuthoritativeSnapshotReceived",
    workspaceId: "workspace-1"
  }).state;

  assert.equal(
    state.turnsById[canonicalTurnKey("session-1", "turn-1")],
    undefined
  );
});

test("identical session upserts preserve canonical state references", () => {
  const source = session(activeTurn(2), 2);
  const first = reduce(createInitialSessionLifecycleState(), {
    type: "session/upserted",
    session: source
  }).state;
  const second = reduce(first, {
    type: "session/upserted",
    session: source
  }).state;

  assert.equal(second, first);
  assert.equal(second.sessionsById, first.sessionsById);
  assert.equal(second.turnsById, first.turnsById);
});

test("identical session snapshots preserve canonical state references", () => {
  const source = session(activeTurn(2), 2);
  const first = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [source]
  }).state;
  const second = reduce(first, {
    type: "session/snapshotReceived",
    sessions: [source]
  }).state;

  assert.equal(second, first);
});

test("lightweight session snapshots cannot downgrade projected lifecycle capabilities", () => {
  const authoritative = {
    ...session(null, 2),
    lifecycleCapabilities: { fork: false, forkThroughTurn: true },
    lifecycleCapabilitiesProjected: true
  };
  const first = reduce(createInitialSessionLifecycleState(), {
    type: "session/upserted",
    session: authoritative
  }).state;
  const lightweight = {
    ...authoritative,
    lifecycleCapabilities: { fork: false, forkThroughTurn: false },
    lifecycleCapabilitiesProjected: undefined,
    title: "Updated rail title",
    updatedAtUnixMs: 3
  };
  const second = reduce(first, {
    type: "session/snapshotReceived",
    sessions: [lightweight]
  }).state;

  assert.equal(second.sessionsById["session-1"]?.title, "Updated rail title");
  assert.deepEqual(
    second.sessionsById["session-1"]?.lifecycleCapabilities,
    authoritative.lifecycleCapabilities
  );
  assert.equal(
    second.sessionsById["session-1"]?.lifecycleCapabilitiesProjected,
    true
  );
});

test("lightweight session snapshots cannot downgrade a bound provider Turn", () => {
  const boundTurn: AgentActivityTurn = {
    ...activeTurn(2),
    outcome: "completed",
    phase: "settled",
    providerForkBindingAvailable: true,
    providerForkBindingState: "bound"
  };
  const authoritative = {
    ...session(boundTurn, 2),
    lifecycleCapabilitiesProjected: true
  };
  let state = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [authoritative]
  }).state;

  const lightweight = {
    ...authoritative,
    lifecycleCapabilitiesProjected: undefined,
    latestTurn: {
      ...boundTurn,
      providerForkBindingAvailable: false,
      providerForkBindingState: "recovery_required" as const
    }
  };
  state = reduce(state, {
    type: "session/snapshotReceived",
    sessions: [lightweight]
  }).state;

  assert.deepEqual(
    state.turnsById[canonicalTurnKey("session-1", "turn-1")],
    boundTurn
  );
});

test("full session snapshots may authoritatively downgrade a provider Turn binding", () => {
  const boundTurn: AgentActivityTurn = {
    ...activeTurn(2),
    outcome: "completed",
    phase: "settled",
    providerForkBindingAvailable: true,
    providerForkBindingState: "bound"
  };
  const authoritative = {
    ...session(boundTurn, 2),
    lifecycleCapabilitiesProjected: true
  };
  let state = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [authoritative]
  }).state;

  state = reduce(state, {
    type: "session/snapshotReceived",
    sessions: [
      {
        ...authoritative,
        latestTurn: {
          ...boundTurn,
          providerForkBindingAvailable: false,
          providerForkBindingState: "recovery_required"
        }
      }
    ]
  }).state;

  const turn = state.turnsById[canonicalTurnKey("session-1", "turn-1")];
  assert.equal(turn?.providerForkBindingAvailable, false);
  assert.equal(turn?.providerForkBindingState, "recovery_required");
});

test("session snapshots preserve monotonic message revisions", () => {
  const current = {
    ...session(null, 2),
    messageVersion: 5
  };
  let state = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [current]
  }).state;

  state = reduce(state, {
    type: "session/snapshotReceived",
    sessions: [
      {
        ...current,
        messageVersion: 4
      }
    ]
  }).state;

  const projected = state.sessionsById["session-1"];
  assert.equal(projected?.messageVersion, 5);
});

test("equal-version sessions still merge changed pending interactions", () => {
  const source = session(activeTurn(2), 2);
  source.latestTurnInteractions = [interaction("pending", 2)];
  source.pendingInteractions = source.latestTurnInteractions;
  const first = reduce(createInitialSessionLifecycleState(), {
    type: "session/upserted",
    session: source
  }).state;
  const changed = {
    ...source,
    latestTurnInteractions: [interaction("pending", 3)],
    pendingInteractions: [interaction("pending", 3)]
  };
  const second = reduce(first, {
    type: "session/upserted",
    session: changed
  }).state;

  assert.equal(second.sessionsById, first.sessionsById);
  assert.notEqual(second.interactionsById, first.interactionsById);
  assert.equal(
    second.interactionsById[
      canonicalInteractionKey("session-1", "turn-1", "request-1")
    ]?.updatedAtUnixMs,
    3
  );
});

test("snapshot restores a settled latest turn without an active turn", () => {
  const latestTurn: AgentActivityTurn = {
    ...activeTurn(7),
    phase: "settled",
    outcome: "failed",
    settledAtUnixMs: 7
  };
  const source = session(null, 7);
  source.latestTurn = latestTurn;
  const result = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [source]
  });
  assert.equal(result.state.sessionsById["session-1"]?.activeTurnId, null);
  assert.deepEqual(
    result.state.turnsById[canonicalTurnKey("session-1", "turn-1")],
    latestTurn
  );
});


test("revoked sharing availability updates owner presentation without changing its reason", () => {
  let state = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [session(null, 1)]
  }).state;
  state = reduce(state, {
    type: "session/runtimeAvailabilityChanged",
    agentSessionId: "session-1",
    availability: {
      state: "blocked",
      reason: "agent_sharing_revoked",
      ownerLabel: "Old owner"
    }
  }).state;

  const renamed = reduce(state, {
    type: "session/runtimeAvailabilityChanged",
    agentSessionId: "session-1",
    availability: {
      state: "blocked",
      reason: "agent_sharing_revoked",
      ownerLabel: "Current owner"
    }
  });

  assert.notEqual(renamed.state, state);
  assert.deepEqual(
    renamed.state.operationBySessionId["session-1"]?.runtimeAvailability,
    {
      state: "blocked",
      reason: "agent_sharing_revoked",
      ownerLabel: "Current owner"
    }
  );
});

test("Turn provenance survives lifecycle upserts, reconcile snapshots, and selectors", () => {
  const initialTurn: AgentActivityTurn = {
    ...activeTurn(2),
    origin: "goal_continuation",
    sourceGoalOperationId: "goal-operation-1",
    sourceGoalRepairEpoch: 4,
    sourceGoalRevision: 7
  };
  let state = reduce(createInitialSessionLifecycleState(), {
    sessions: [session(initialTurn, 2)],
    type: "session/snapshotReceived"
  }).state;

  state = reduce(state, {
    turn: {
      ...initialTurn,
      origin: "goal_arm",
      phase: "waiting",
      sourceGoalOperationId: "conflicting-operation",
      sourceGoalRepairEpoch: 99,
      sourceGoalRevision: 99,
      updatedAtUnixMs: 3
    },
    live: true,
    type: "turn/upserted"
  }).state;

  const reconciled = session(null, 4);
  reconciled.activeTurn = {
    ...initialTurn,
    origin: "goal_arm",
    phase: "running",
    sourceGoalOperationId: undefined,
    sourceGoalRepairEpoch: undefined,
    sourceGoalRevision: undefined,
    updatedAtUnixMs: 4
  };
  reconciled.activeTurnId = initialTurn.turnId;
  state = reduce(state, {
    sessions: [reconciled],
    type: "session/snapshotReceived"
  }).state;

  const engine = {
    ...createInitialAgentSessionEngineState(),
    sessionLifecycle: state
  };
  const selected = selectEngineTurn(engine, "session-1", "turn-1");
  assert.equal(selected?.phase, "running");
  assert.equal(selected?.origin, "goal_continuation");
  assert.equal(selected?.sourceGoalOperationId, "goal-operation-1");
  assert.equal(selected?.sourceGoalRevision, 7);
  assert.equal(selected?.sourceGoalRepairEpoch, 4);
});

test("legacy_unknown Turn provenance is never inferred during reconcile", () => {
  const legacyTurn: AgentActivityTurn = {
    ...activeTurn(2),
    origin: "legacy_unknown"
  };
  let state = reduce(createInitialSessionLifecycleState(), {
    sessions: [session(legacyTurn, 2)],
    type: "session/snapshotReceived"
  }).state;
  state = reduce(state, {
    turn: {
      ...legacyTurn,
      origin: "goal_continuation",
      sourceGoalOperationId: "goal-operation-guessed",
      sourceGoalRepairEpoch: 1,
      sourceGoalRevision: 1,
      updatedAtUnixMs: 3
    },
    live: true,
    type: "turn/upserted"
  }).state;

  const stored =
    state.turnsById[canonicalTurnKey("session-1", legacyTurn.turnId)];
  assert.equal(stored?.origin, "legacy_unknown");
  assert.equal(stored?.sourceGoalOperationId, undefined);
  assert.equal(stored?.sourceGoalRevision, undefined);
  assert.equal(stored?.sourceGoalRepairEpoch, undefined);
});

test("bounded snapshots preserve page-loaded session entities omitted from the response", () => {
  const pageLoaded = {
    ...session(null, 2),
    agentSessionId: "page-loaded"
  };
  let state = reduce(createInitialSessionLifecycleState(), {
    type: "session/upserted",
    session: pageLoaded
  }).state;
  state = reduce(state, {
    type: "session/snapshotReceived",
    sessions: [session(null, 3)]
  }).state;

  assert.equal(
    state.sessionsById["page-loaded"]?.agentSessionId,
    "page-loaded"
  );
  assert.equal(state.sessionsById["session-1"]?.updatedAtUnixMs, 3);
});


test("send command result rejects session and turn scope mismatch atomically", () => {
  const turn = { ...activeTurn(4), agentSessionId: "session-other" };
  const result = reduce(createInitialSessionLifecycleState(), {
    commandId: "send-1",
    commandType: "queue/sendPrompt",
    outcome: "succeeded",
    type: "engine/commandResult",
    value: { session: session(null, 4), turn, turnId: turn.turnId }
  });
  assert.deepEqual(result.state, createInitialSessionLifecycleState());
});

test("snapshot scopes identical latest turn ids by session", () => {
  const first = session(null, 8);
  first.latestTurn = {
    ...activeTurn(8),
    phase: "settled",
    outcome: "completed"
  };
  const second = {
    ...session(null, 9),
    agentSessionId: "session-2",
    latestTurn: {
      ...activeTurn(9),
      agentSessionId: "session-2",
      phase: "settled" as const,
      outcome: "failed" as const
    }
  };
  const result = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [first, second]
  });
  assert.equal(
    result.state.turnsById[canonicalTurnKey("session-1", "turn-1")]?.outcome,
    "completed"
  );
  assert.equal(
    result.state.turnsById[canonicalTurnKey("session-2", "turn-1")]?.outcome,
    "failed"
  );
});

test("turn and interaction events update independent canonical collections", () => {
  let state = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [session(null, 1)]
  }).state;
  state = reduce(state, {
    live: true,
    type: "turn/upserted",
    turn: activeTurn(2)
  }).state;
  const pending = interaction("pending", 3);
  state = reduce(state, {
    type: "interaction/upserted",
    interaction: pending
  }).state;
  assert.equal(
    state.turnsById[canonicalTurnKey("session-1", "turn-1")]?.phase,
    "running"
  );
  assert.equal(
    state.interactionsById[
      canonicalInteractionKey("session-1", "turn-1", "request-1")
    ]?.status,
    "pending"
  );

  state = reduce(state, {
    type: "interaction/upserted",
    interaction: interaction("answered", 4)
  }).state;
  assert.equal(
    state.interactionsById[
      canonicalInteractionKey("session-1", "turn-1", "request-1")
    ]?.status,
    "answered"
  );
});






test("session metadata patches update the canonical session without a list overlay", () => {
  let state = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [session(null, 1)]
  }).state;
  state = reduce(state, {
    type: "session/metadataPatched",
    agentSessionId: "session-1",
    patch: { title: "Renamed", updatedAtUnixMs: 2 }
  }).state;
  assert.equal(state.sessionsById["session-1"]?.title, "Renamed");
  assert.equal(state.sessionsById["session-1"]?.updatedAtUnixMs, 2);
});

test("identical turn and request ids remain isolated by session", () => {
  const turn1 = activeTurn(2);
  const turn2 = { ...activeTurn(3), agentSessionId: "session-2" };
  const interaction1 = interaction("pending", 2);
  const interaction2 = {
    ...interaction("pending", 3),
    agentSessionId: "session-2"
  };
  const session1 = session(turn1, 2);
  session1.pendingInteractions = [interaction1];
  const session2 = {
    ...session(turn2, 3),
    agentSessionId: "session-2",
    pendingInteractions: [interaction2]
  };
  const lifecycle = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [session1, session2]
  }).state;
  const engineState = {
    ...createInitialAgentSessionEngineState(),
    sessionLifecycle: lifecycle
  };

  assert.equal(
    selectEngineTurn(engineState, "session-1", "turn-1")?.agentSessionId,
    "session-1"
  );
  assert.equal(
    selectEngineTurn(engineState, "session-2", "turn-1")?.agentSessionId,
    "session-2"
  );
  assert.equal(
    selectEngineInteraction(engineState, "session-1", "turn-1", "request-1")
      ?.agentSessionId,
    "session-1"
  );
  assert.equal(
    selectEngineInteraction(engineState, "session-2", "turn-1", "request-1")
      ?.agentSessionId,
    "session-2"
  );
});

test("identical request ids remain isolated across turns in one session", () => {
  const turn1 = activeTurn(1);
  const turn2 = { ...activeTurn(2), turnId: "turn-2" };
  let state = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [session(turn1, 1)]
  }).state;
  state = reduce(state, {
    live: true,
    type: "turn/upserted",
    turn: turn2
  }).state;
  state = reduce(state, {
    type: "interaction/upserted",
    interaction: interaction("pending", 2)
  }).state;
  state = reduce(state, {
    type: "interaction/upserted",
    interaction: { ...interaction("pending", 3), turnId: "turn-2" }
  }).state;

  assert.equal(
    state.interactionsById[
      canonicalInteractionKey("session-1", "turn-1", "request-1")
    ]?.turnId,
    "turn-1"
  );
  assert.equal(
    state.interactionsById[
      canonicalInteractionKey("session-1", "turn-2", "request-1")
    ]?.turnId,
    "turn-2"
  );
});

test("authoritative snapshots remove pending interactions that are no longer present", () => {
  const withPending = session(activeTurn(2), 2);
  withPending.pendingInteractions = [interaction("pending", 2)];
  let state = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [withPending]
  }).state;
  assert.equal(
    state.interactionsById[
      canonicalInteractionKey("session-1", "turn-1", "request-1")
    ]?.status,
    "pending"
  );

  state = reduce(state, {
    type: "session/snapshotReceived",
    sessions: [session(activeTurn(3), 3)]
  }).state;
  assert.equal(
    state.interactionsById[
      canonicalInteractionKey("session-1", "turn-1", "request-1")
    ],
    undefined
  );
});

test("authoritative snapshots remove an old-turn pending interaction when the request id is reused", () => {
  const turn1 = activeTurn(2);
  const withTurn1Pending = session(turn1, 2);
  withTurn1Pending.pendingInteractions = [interaction("pending", 2)];
  let state = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [withTurn1Pending]
  }).state;

  const turn2 = { ...activeTurn(3), turnId: "turn-2" };
  const withTurn2Pending = session(turn2, 3);
  withTurn2Pending.pendingInteractions = [
    { ...interaction("pending", 3), turnId: "turn-2" }
  ];
  state = reduce(state, {
    type: "session/snapshotReceived",
    sessions: [withTurn2Pending]
  }).state;

  assert.equal(
    state.interactionsById[
      canonicalInteractionKey("session-1", "turn-1", "request-1")
    ],
    undefined
  );
  assert.equal(
    state.interactionsById[
      canonicalInteractionKey("session-1", "turn-2", "request-1")
    ]?.status,
    "pending"
  );
});

for (const intentType of [
  "session/snapshotReceived",
  "session/upserted"
] as const) {
  test(`${intentType} only removes omitted pending interactions at an authoritative version`, () => {
    let state = reduce(createInitialSessionLifecycleState(), {
      type: "session/snapshotReceived",
      sessions: [session(activeTurn(1), 1)]
    }).state;
    state = reduce(state, {
      type: "interaction/upserted",
      interaction: interaction("pending", 4)
    }).state;

    const olderEmpty = session(activeTurn(3), 3);
    state = reduce(
      state,
      intentType === "session/snapshotReceived"
        ? { type: intentType, sessions: [olderEmpty] }
        : { type: intentType, session: olderEmpty }
    ).state;
    assert.equal(
      state.interactionsById[
        canonicalInteractionKey("session-1", "turn-1", "request-1")
      ]?.status,
      "pending"
    );

    const newerEmpty = session(activeTurn(5), 5);
    state = reduce(
      state,
      intentType === "session/snapshotReceived"
        ? { type: intentType, sessions: [newerEmpty] }
        : { type: intentType, session: newerEmpty }
    ).state;
    assert.equal(
      state.interactionsById[
        canonicalInteractionKey("session-1", "turn-1", "request-1")
      ],
      undefined
    );
  });
}

















test("same-millisecond snapshots cannot replace or clear a different live turn", () => {
  const turnB = { ...activeTurn(2), turnId: "turn-b" };
  let state = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [session(turnB, 2)]
  }).state;
  state = reduce(state, {
    type: "session/snapshotReceived",
    sessions: [session(null, 2)]
  }).state;
  assert.equal(state.sessionsById["session-1"]?.activeTurnId, "turn-b");
  const turnA = { ...activeTurn(2), turnId: "turn-a" };
  state = reduce(state, {
    type: "session/snapshotReceived",
    sessions: [session(turnA, 2)]
  }).state;
  assert.equal(state.sessionsById["session-1"]?.activeTurnId, "turn-b");
});

test("deleted session tombstone rejects late snapshot resurrection", () => {
  let state = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [session(null, 1)]
  }).state;
  state = reduce(state, {
    type: "session/removed",
    agentSessionId: "session-1"
  }).state;
  const late = reduce(state, {
    type: "session/snapshotReceived",
    sessions: [session(activeTurn(2), 2)]
  });
  assert.equal(late.state.sessionsById["session-1"], undefined);
  assert.equal(late.state.operationBySessionId["session-1"], undefined);
});

test("restart snapshot hydrates terminal latest-turn interactions", () => {
  const restored = session(null, 5);
  restored.latestTurn = {
    ...activeTurn(4),
    phase: "settled",
    outcome: "completed"
  };
  restored.latestTurnInteractions = [interaction("answered", 5)];
  const state = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [restored]
  }).state;
  assert.equal(
    state.interactionsById[
      canonicalInteractionKey("session-1", "turn-1", "request-1")
    ]?.status,
    "answered"
  );
});

for (const terminal of ["answered", "superseded"] as const) {
  test(`stale pending snapshot cannot regress ${terminal} interaction`, () => {
    const current = session(activeTurn(4), 4);
    current.latestTurnInteractions = [interaction(terminal, 4)];
    let state = reduce(createInitialSessionLifecycleState(), {
      type: "session/snapshotReceived",
      sessions: [current]
    }).state;
    const stale = session(activeTurn(3), 3);
    stale.pendingInteractions = [interaction("pending", 3)];
    state = reduce(state, {
      type: "session/snapshotReceived",
      sessions: [stale]
    }).state;
    assert.equal(
      state.interactionsById[
        canonicalInteractionKey("session-1", "turn-1", "request-1")
      ]?.status,
      terminal
    );
  });
}

test("interaction then turn then session converges without exposing orphans", () => {
  let state = reduce(createInitialSessionLifecycleState(), {
    type: "interaction/upserted",
    interaction: interaction("pending", 2)
  }).state;
  let engine = {
    ...createInitialAgentSessionEngineState(),
    sessionLifecycle: state
  };
  assert.equal(
    selectEngineInteraction(engine, "session-1", "turn-1", "request-1"),
    null
  );
  state = reduce(state, {
    live: true,
    type: "turn/upserted",
    turn: activeTurn(2)
  }).state;
  engine = { ...engine, sessionLifecycle: state };
  assert.equal(selectEngineTurn(engine, "session-1", "turn-1"), null);
  const parentSession = session(null, 3);
  parentSession.pendingInteractions = [interaction("pending", 2)];
  state = reduce(state, {
    type: "session/upserted",
    session: parentSession
  }).state;
  engine = { ...engine, sessionLifecycle: state };
  assert.equal(
    selectEngineTurn(engine, "session-1", "turn-1")?.turnId,
    "turn-1"
  );
  assert.equal(
    selectEngineInteraction(engine, "session-1", "turn-1", "request-1")?.status,
    "pending"
  );
});

test("delete tombstone rejects late orphan turn and interaction upserts", () => {
  let state = reduce(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [session(activeTurn(1), 1)]
  }).state;
  state = reduce(state, {
    type: "session/removed",
    agentSessionId: "session-1"
  }).state;
  state = reduce(state, {
    live: true,
    type: "turn/upserted",
    turn: activeTurn(2)
  }).state;
  state = reduce(state, {
    type: "interaction/upserted",
    interaction: interaction("pending", 2)
  }).state;
  assert.equal(Object.keys(state.turnsById).length, 0);
  assert.equal(Object.keys(state.interactionsById).length, 0);
});

test("realtime Turn projection atomically clears its owned Session reference", () => {
  let state = reduce(createInitialSessionLifecycleState(), {
    sessions: [session(activeTurn(1), 1)],
    type: "session/snapshotReceived"
  }).state;

  state = reduce(state, {
    activeTurnId: null,
    turn: {
      ...activeTurn(2),
      outcome: "completed",
      phase: "settled",
      settledAtUnixMs: 2
    },
    type: "turn/projectionReceived",
    workspaceId: "workspace-1"
  }).state;

  assert.equal(state.sessionsById["session-1"]?.activeTurnId, null);
  assert.equal(state.sessionsById["session-1"]?.updatedAtUnixMs, 1);
  assert.equal(state.sessionsById["session-1"]?.lastEventUnixMs, 1);
  assert.equal(
    state.turnsById[canonicalTurnKey("session-1", "turn-1")]?.phase,
    "settled"
  );

  state = reduce(state, {
    session: session(activeTurn(1), 1),
    type: "session/upserted"
  }).state;
  assert.equal(state.sessionsById["session-1"]?.activeTurnId, null);

  state = reduce(state, {
    session: { ...session(null, 3), title: "Reconciled" },
    type: "session/upserted"
  }).state;
  assert.equal(state.sessionsById["session-1"]?.title, "Reconciled");
  assert.equal(state.sessionsById["session-1"]?.updatedAtUnixMs, 3);
});

test("host-fenced same-Turn settlement survives cross-device clock skew", () => {
  const running = activeTurn(200);
  let state = reduce(createInitialSessionLifecycleState(), {
    sessions: [session(running, 300)],
    type: "session/snapshotReceived"
  }).state;

  state = reduce(state, {
    activeTurnId: null,
    hostFencedSameTurnSettlement: true,
    turn: {
      ...running,
      outcome: "completed",
      phase: "settled",
      settledAtUnixMs: 190,
      updatedAtUnixMs: 190
    },
    type: "turn/projectionReceived",
    workspaceId: "workspace-1"
  }).state;

  assert.equal(state.sessionsById["session-1"]?.activeTurnId, null);
  assert.equal(state.sessionsById["session-1"]?.updatedAtUnixMs, 300);
  assert.deepEqual(state.turnsById[canonicalTurnKey("session-1", "turn-1")], {
    ...running,
    outcome: "completed",
    phase: "settled",
    settledAtUnixMs: 190,
    updatedAtUnixMs: 190
  });

  state = reduce(state, {
    session: session(running, 300),
    type: "session/upserted"
  }).state;
  assert.equal(state.sessionsById["session-1"]?.activeTurnId, null);
});

test("host-fenced settlement cannot clear a different active Turn", () => {
  const turnA = { ...activeTurn(200), turnId: "turn-a" };
  const turnB = { ...activeTurn(300), turnId: "turn-b" };
  let state = reduce(createInitialSessionLifecycleState(), {
    sessions: [session(turnB, 300)],
    type: "session/snapshotReceived"
  }).state;
  state = reduce(state, {
    live: true,
    turn: turnA,
    type: "turn/upserted"
  }).state;

  state = reduce(state, {
    activeTurnId: null,
    hostFencedSameTurnSettlement: true,
    turn: {
      ...turnA,
      outcome: "completed",
      phase: "settled",
      settledAtUnixMs: 190,
      updatedAtUnixMs: 190
    },
    type: "turn/projectionReceived",
    workspaceId: "workspace-1"
  }).state;

  assert.equal(
    state.turnsById[canonicalTurnKey("session-1", "turn-a")]?.phase,
    "settled"
  );
  assert.equal(state.sessionsById["session-1"]?.activeTurnId, "turn-b");
  assert.deepEqual(deriveCanonicalSubmitAvailability(state, "session-1"), {
    state: "blocked",
    reason: "active_turn"
  });
});

test("unmarked Turn projection still rejects an older settlement", () => {
  const running = activeTurn(200);
  let state = reduce(createInitialSessionLifecycleState(), {
    sessions: [session(running, 300)],
    type: "session/snapshotReceived"
  }).state;

  state = reduce(state, {
    activeTurnId: null,
    turn: {
      ...running,
      outcome: "completed",
      phase: "settled",
      settledAtUnixMs: 190,
      updatedAtUnixMs: 190
    },
    type: "turn/projectionReceived",
    workspaceId: "workspace-1"
  }).state;

  assert.equal(state.sessionsById["session-1"]?.activeTurnId, "turn-1");
  assert.equal(
    state.turnsById[canonicalTurnKey("session-1", "turn-1")]?.phase,
    "running"
  );
});

test("generic Turn upsert still rejects an older settlement", () => {
  const running = activeTurn(200);
  let state = reduce(createInitialSessionLifecycleState(), {
    sessions: [session(running, 300)],
    type: "session/snapshotReceived"
  }).state;

  state = reduce(state, {
    live: true,
    turn: {
      ...running,
      outcome: "completed",
      phase: "settled",
      settledAtUnixMs: 190,
      updatedAtUnixMs: 190
    },
    type: "turn/upserted"
  }).state;

  assert.equal(state.sessionsById["session-1"]?.activeTurnId, "turn-1");
  assert.equal(
    state.turnsById[canonicalTurnKey("session-1", "turn-1")]?.phase,
    "running"
  );
});

test("cached Turn projection fences a stale Session loaded afterward", () => {
  let state = reduce(createInitialSessionLifecycleState(), {
    activeTurnId: null,
    turn: {
      ...activeTurn(2),
      outcome: "completed",
      phase: "settled",
      settledAtUnixMs: 2
    },
    type: "turn/projectionReceived",
    workspaceId: "workspace-1"
  }).state;
  assert.equal(state.sessionsById["session-1"], undefined);

  state = reduce(state, {
    session: session(activeTurn(1), 1),
    type: "session/upserted"
  }).state;

  assert.equal(state.sessionsById["session-1"]?.activeTurnId, null);
  assert.equal(
    state.turnsById[canonicalTurnKey("session-1", "turn-1")]?.phase,
    "settled"
  );
});

test("a settled Turn cannot clear a different active Turn loaded afterward", () => {
  let state = reduce(createInitialSessionLifecycleState(), {
    activeTurnId: null,
    turn: {
      ...activeTurn(5),
      outcome: "completed",
      phase: "settled",
      settledAtUnixMs: 5,
      turnId: "turn-a"
    },
    type: "turn/projectionReceived",
    workspaceId: "workspace-1"
  }).state;
  const turnB = { ...activeTurn(5), turnId: "turn-b" };

  state = reduce(state, {
    session: session(turnB, 5),
    type: "session/upserted"
  }).state;

  assert.equal(state.sessionsById["session-1"]?.activeTurnId, "turn-b");
});

test("a cached settlement fences only the same incoming Turn", () => {
  let state = reduce(createInitialSessionLifecycleState(), {
    activeTurnId: null,
    turn: {
      ...activeTurn(10),
      outcome: "completed",
      phase: "settled",
      settledAtUnixMs: 10,
      turnId: "turn-a"
    },
    type: "turn/projectionReceived",
    workspaceId: "workspace-1"
  }).state;
  state = reduce(state, {
    activeTurnId: null,
    turn: {
      ...activeTurn(6),
      outcome: "completed",
      phase: "settled",
      settledAtUnixMs: 6,
      turnId: "turn-b"
    },
    type: "turn/projectionReceived",
    workspaceId: "workspace-1"
  }).state;

  state = reduce(state, {
    session: session({ ...activeTurn(5), turnId: "turn-b" }, 5),
    type: "session/upserted"
  }).state;

  assert.equal(state.sessionsById["session-1"]?.activeTurnId, null);
  assert.equal(
    state.turnsById[canonicalTurnKey("session-1", "turn-b")]?.phase,
    "settled"
  );
});

test("an unrelated settled Turn cannot clear the current active Turn", () => {
  const turnB = { ...activeTurn(8), turnId: "turn-b" };
  let state = reduce(createInitialSessionLifecycleState(), {
    sessions: [session(turnB, 8)],
    type: "session/snapshotReceived"
  }).state;
  const snapshot = session(null, 8);
  snapshot.latestTurn = {
    ...activeTurn(10),
    outcome: "completed",
    phase: "settled",
    settledAtUnixMs: 10,
    turnId: "turn-a"
  };

  state = reduce(state, {
    session: snapshot,
    type: "session/upserted"
  }).state;

  assert.equal(state.sessionsById["session-1"]?.activeTurnId, "turn-b");
});

test("same-Turn terminal evidence repairs an inconsistent active pointer", () => {
  const turnB = { ...activeTurn(1), turnId: "turn-b" };
  let state = reduce(createInitialSessionLifecycleState(), {
    sessions: [session(turnB, 1)],
    type: "session/snapshotReceived"
  }).state;
  state = reduce(state, {
    turn: {
      ...turnB,
      outcome: "completed",
      phase: "settled",
      settledAtUnixMs: 2,
      updatedAtUnixMs: 2
    },
    live: true,
    type: "turn/upserted"
  }).state;

  state = reduce(state, {
    session: session(turnB, 1),
    type: "session/upserted"
  }).state;

  assert.equal(state.sessionsById["session-1"]?.activeTurnId, null);
});

test("realtime live Turn projection claims an idle Session atomically", () => {
  let state = reduce(createInitialSessionLifecycleState(), {
    sessions: [session(null, 1)],
    type: "session/snapshotReceived"
  }).state;
  const liveTurn = { ...activeTurn(1), turnId: "turn-2" };

  state = reduce(state, {
    activeTurnId: "turn-2",
    turn: liveTurn,
    type: "turn/projectionReceived",
    workspaceId: "workspace-1"
  }).state;

  assert.equal(state.sessionsById["session-1"]?.activeTurnId, "turn-2");
  assert.deepEqual(
    state.turnsById[canonicalTurnKey("session-1", "turn-2")],
    liveTurn
  );
});

test("late Turn projection cannot clear or replace a newer active Turn", () => {
  const newerTurn = { ...activeTurn(4), turnId: "turn-2" };
  let state = reduce(createInitialSessionLifecycleState(), {
    sessions: [session(newerTurn, 4)],
    type: "session/snapshotReceived"
  }).state;
  state = reduce(state, {
    turn: activeTurn(1),
    live: true,
    type: "turn/upserted"
  }).state;

  state = reduce(state, {
    activeTurnId: null,
    turn: {
      ...activeTurn(2),
      outcome: "completed",
      phase: "settled",
      settledAtUnixMs: 2
    },
    type: "turn/projectionReceived",
    workspaceId: "workspace-1"
  }).state;
  assert.equal(state.sessionsById["session-1"]?.activeTurnId, "turn-2");
  assert.equal(
    state.turnsById[canonicalTurnKey("session-1", "turn-1")]?.phase,
    "settled"
  );

  state = reduce(createInitialSessionLifecycleState(), {
    sessions: [session(newerTurn, 4)],
    type: "session/snapshotReceived"
  }).state;
  state = reduce(state, {
    activeTurnId: "turn-1",
    turn: { ...activeTurn(3), turnId: "turn-1" },
    type: "turn/projectionReceived",
    workspaceId: "workspace-1"
  }).state;
  assert.equal(state.sessionsById["session-1"]?.activeTurnId, "turn-2");
});

test("settled turn is terminal against newer live phases and outcome changes", () => {
  let state = reduce(createInitialSessionLifecycleState(), {
    live: true,
    type: "turn/upserted",
    turn: { ...activeTurn(2), phase: "settled", outcome: "completed" }
  }).state;
  state = reduce(state, {
    live: true,
    type: "turn/upserted",
    turn: { ...activeTurn(3), phase: "running" }
  }).state;
  state = reduce(state, {
    live: true,
    type: "turn/upserted",
    turn: { ...activeTurn(4), phase: "settled", outcome: "failed" }
  }).state;
  const turn = state.turnsById[canonicalTurnKey("session-1", "turn-1")];
  assert.equal(turn?.phase, "settled");
  assert.equal(turn?.outcome, "completed");
});

test("running and waiting transitions remain bidirectional before settle", () => {
  let state = reduce(createInitialSessionLifecycleState(), {
    live: true,
    type: "turn/upserted",
    turn: activeTurn(1)
  }).state;
  state = reduce(state, {
    live: true,
    type: "turn/upserted",
    turn: { ...activeTurn(2), phase: "waiting" }
  }).state;
  assert.equal(
    state.turnsById[canonicalTurnKey("session-1", "turn-1")]?.phase,
    "waiting"
  );
  state = reduce(state, {
    live: true,
    type: "turn/upserted",
    turn: activeTurn(3)
  }).state;
  assert.equal(
    state.turnsById[canonicalTurnKey("session-1", "turn-1")]?.phase,
    "running"
  );
});


function reduce(
  state: ReturnType<typeof createInitialSessionLifecycleState>,
  intent: Parameters<typeof sessionLifecycleReducer>[1]
) {
  return sessionLifecycleReducer(state, intent, {
    sendResultValidation:
      intent.type === "engine/commandResult" &&
      intent.commandType === "queue/sendPrompt" &&
      intent.outcome === "succeeded"
        ? validateSendInputResult(intent.value, {
            acceptedSessionVersion: null,
            agentSessionId: "session-1",
            clientSubmitId: "submit-1",
            content: [],
            errorCode: null,
            errorMessage: null,
            errorReason: null,
            expiresAtUnixMs: 1,
            requestedAtUnixMs: 1,
            status: "requested",
            turnId: null,
            workspaceId: "workspace-1"
          })
        : null
  });
}

function session(
  turn: AgentActivityTurn | null,
  updatedAtUnixMs: number,
  provider = "codex"
): AgentActivitySession {
  return normalizeAgentActivitySession({
    ...{
      activeTurnId: null,
      latestTurnInteractions: [],
      pendingInteractions: []
    },
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    provider,
    cwd: "/workspace",
    title: "Session",
    activeTurnId: turn?.turnId ?? null,
    activeTurn: turn,
    latestTurnInteractions: [],
    pendingInteractions: [],
    updatedAtUnixMs
  });
}

function activeTurn(updatedAtUnixMs: number): AgentActivityTurn {
  return {
    turnId: "turn-1",
    agentSessionId: "session-1",
    origin: "user_prompt",
    phase: "running",
    startedAtUnixMs: 1,
    updatedAtUnixMs
  };
}


function interaction(
  status: AgentActivityInteraction["status"],
  updatedAtUnixMs: number
): AgentActivityInteraction {
  return {
    requestId: "request-1",
    agentSessionId: "session-1",
    turnId: "turn-1",
    kind: "question",
    status,
    createdAtUnixMs: 3,
    updatedAtUnixMs
  };
}

test("equal timestamp terminal turn wins and invalid settling regression is rejected", () => {
  let state = reduce(createInitialSessionLifecycleState(), {
    live: true,
    type: "turn/upserted",
    turn: { ...activeTurn(2), phase: "settling" }
  }).state;
  state = reduce(state, {
    live: true,
    type: "turn/upserted",
    turn: { ...activeTurn(2), phase: "running" }
  }).state;
  assert.equal(
    state.turnsById[canonicalTurnKey("session-1", "turn-1")]?.phase,
    "settling"
  );
  state = reduce(state, {
    live: true,
    type: "turn/upserted",
    turn: { ...activeTurn(2), phase: "settled", outcome: "completed" }
  }).state;
  assert.equal(
    state.turnsById[canonicalTurnKey("session-1", "turn-1")]?.phase,
    "settled"
  );
});
