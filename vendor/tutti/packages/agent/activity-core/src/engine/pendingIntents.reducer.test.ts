import assert from "node:assert/strict";
import test from "node:test";
import {
  createInitialPendingIntentsState,
  pendingIntentsReducer
} from "./pendingIntents.reducer.ts";
import { canonicalTurnKey } from "./sessionEntityKeys.ts";
import {
  validateScopedSessionResult,
  validateSendInputResult
} from "./commandResult.validation.ts";

test("submit intent declares its confirmation deadline", () => {
  const result = reduce(createInitialPendingIntentsState(), submit());
  assert.equal(
    result.state.submitsByClientSubmitId["submit-1"]?.status,
    "requested"
  );
  assert.deepEqual(result.commands, [
    {
      dueAtUnixMs: 60_000,
      expiryId: "submit:submit-1",
      type: "engine/scheduleExpiry"
    }
  ]);
});

test("command timeout is uncertain until the same client submit id is durable", () => {
  let state = reduce(createInitialPendingIntentsState(), submit()).state;
  state = reduce(state, {
    type: "engine/commandResult",
    commandId: "queue:send:session-1:1",
    commandType: "queue/sendPrompt",
    correlationId: "submit-1",
    outcome: "timedOut"
  }).state;
  assert.equal(state.submitsByClientSubmitId["submit-1"]?.status, "uncertain");
  const unrelated = reduce(state, {
    type: "message/snapshotReceived",
    messages: [message("submit-2")]
  });
  assert.ok(unrelated.state.submitsByClientSubmitId["submit-1"]);
  const confirmed = reduce(unrelated.state, {
    type: "message/snapshotReceived",
    messages: [message("submit-1")]
  });
  assert.equal(
    confirmed.state.submitsByClientSubmitId["submit-1"]?.status,
    "confirmed"
  );
  assert.deepEqual(confirmed.commands, []);
});

test("failed submit preserves the structured protocol reason for presentation", () => {
  let state = reduce(createInitialPendingIntentsState(), submit()).state;
  state = reduce(state, {
    type: "engine/commandResult",
    commandId: "queue:send:session-1:1",
    commandType: "queue/sendPrompt",
    correlationId: "submit-1",
    errorCode: "workspace_operation_failed",
    errorMessage: "agent process cleanup is still pending",
    errorReason: "agent.process_cleanup_pending",
    outcome: "failed"
  }).state;

  const record = state.submitsByClientSubmitId["submit-1"];
  assert.equal(record?.errorCode, "workspace_operation_failed");
  assert.equal(record?.errorMessage, "agent process cleanup is still pending");
  assert.equal(record?.errorReason, "agent.process_cleanup_pending");
  assert.equal(record?.status, "failed");
});

test("successful send records the authoritative turn result", () => {
  let state = reduce(createInitialPendingIntentsState(), submit()).state;
  state = reduce(state, {
    type: "engine/commandResult",
    commandId: "queue:send:session-1:1",
    commandType: "queue/sendPrompt",
    correlationId: "submit-1",
    outcome: "succeeded",
    value: {
      session: {
        agentSessionId: "session-1",
        cwd: "/workspace",
        provider: "codex",
        status: "working",
        title: "Session",
        workspaceId: "workspace-1"
      },
      turnId: "turn-1",
      turn: runningTurn()
    }
  }).state;
  assert.equal(state.submitsByClientSubmitId["submit-1"]?.turnId, "turn-1");
  assert.equal(state.submitsByClientSubmitId["submit-1"]?.status, "accepted");
});

test("invalid successful send results remain uncertain for canonical reconciliation", () => {
  const invalidValues = [
    undefined,
    {
      session: { ...session("session-1"), workspaceId: "workspace-other" },
      turnId: "turn-1",
      turn: runningTurn()
    },
    {
      session: session("session-other"),
      turnId: "turn-1",
      turn: { ...runningTurn(), agentSessionId: "session-other" }
    },
    {
      session: session("session-1"),
      turnId: "turn-other",
      turn: runningTurn()
    }
  ];
  for (const value of invalidValues) {
    let state = reduce(createInitialPendingIntentsState(), submit()).state;
    state = reduce(state, {
      type: "engine/commandResult",
      commandId: "queue:send:session-1:1",
      commandType: "queue/sendPrompt",
      correlationId: "submit-1",
      outcome: "succeeded",
      value
    }).state;
    const record = state.submitsByClientSubmitId["submit-1"];
    assert.equal(record?.status, "uncertain");
    assert.equal(record?.errorCode, "invalid_command_result");
    assert.equal(record?.turnId, null);
  }
});

test("an explicit settled turn confirms its accepted submit", () => {
  let state = reduce(createInitialPendingIntentsState(), submit()).state;
  state = reduce(state, {
    type: "engine/commandResult",
    commandId: "queue:send:session-1:1",
    commandType: "queue/sendPrompt",
    correlationId: "submit-1",
    outcome: "succeeded",
    value: {
      session: session("session-1"),
      turnId: "turn-1",
      turn: runningTurn()
    }
  }).state;
  const turn = {
    turnId: "turn-1",
    agentSessionId: "session-1",
    origin: "user_prompt" as const,
    phase: "settled" as const,
    outcome: "completed" as const,
    startedAtUnixMs: 1,
    settledAtUnixMs: 2,
    updatedAtUnixMs: 2
  };
  const result = pendingIntentsReducer(
    state,
    { live: true, type: "turn/upserted", turn },
    {
      deletedSessionIds: {},
      turnsById: { [canonicalTurnKey("session-1", "turn-1")]: turn },
      sendResultValidation: validateSendInputResult(
        {
          session: session("session-1"),
          turnId: "turn-1",
          turn: runningTurn()
        },
        state.submitsByClientSubmitId["submit-1"]
      )
    }
  );
  assert.equal(
    result.state.submitsByClientSubmitId["submit-1"]?.status,
    "confirmed"
  );
});

test("a late successful send result confirms against an already settled turn", () => {
  let state = reduce(createInitialPendingIntentsState(), submit()).state;
  const turn = {
    turnId: "turn-1",
    agentSessionId: "session-1",
    origin: "user_prompt" as const,
    phase: "settled" as const,
    outcome: "completed" as const,
    startedAtUnixMs: 1,
    settledAtUnixMs: 2,
    updatedAtUnixMs: 2
  };
  const result = pendingIntentsReducer(
    state,
    {
      type: "engine/commandResult",
      commandId: "queue:send:session-1:1",
      commandType: "queue/sendPrompt",
      correlationId: "submit-1",
      outcome: "succeeded",
      value: {
        session: session("session-1"),
        turnId: "turn-1",
        turn: runningTurn()
      }
    },
    {
      deletedSessionIds: {},
      turnsById: { [canonicalTurnKey("session-1", "turn-1")]: turn },
      sendResultValidation: validateSendInputResult(
        {
          session: session("session-1"),
          turnId: "turn-1",
          turn: runningTurn()
        },
        state.submitsByClientSubmitId["submit-1"]
      )
    }
  );
  assert.equal(
    result.state.submitsByClientSubmitId["submit-1"]?.status,
    "confirmed"
  );
});

test("activation intent owns the transport command and confirmation deadline", () => {
  const result = reduce(createInitialPendingIntentsState(), activation());
  const pendingActivation = result.state.activationsByRequestId["activation-1"];
  assert.equal(pendingActivation?.status, "requested");
  assert.equal(pendingActivation?.commandOutcome, "pending");
  assert.equal(pendingActivation?.snapshotOutcome, "not_observed");
  assert.equal(pendingActivation?.lastObservedStage, "requested");
  assert.equal(pendingActivation?.displayPrompt, "/browser");
  assert.equal(pendingActivation?.optimisticTitle, "Review browser flow");
  assert.equal(pendingActivation?.railSectionKey, "project:/workspace");
  assert.deepEqual(pendingActivation?.railPlacement, {
    version: 1,
    kind: "project",
    projectPath: "/workspace",
    sectionKey: "project:/workspace"
  });
  assert.deepEqual(pendingActivation?.content, [
    { type: "text", text: "hello" }
  ]);
  assert.deepEqual(result.commands, [
    {
      dueAtUnixMs: 120_000,
      expiryId: "activation:activation-1",
      type: "engine/scheduleExpiry"
    },
    {
      agentSessionId: "session-new",
      agentTargetId: "target-1",
      clientSubmitId: "submit-new",
      commandId: "activate:activation-1",
      correlationId: "activation-1",
      cwd: "/workspace",
      initialContent: [{ type: "text", text: "runtime instructions" }],
      initialDisplayPrompt: "/browser",
      railPlacement: {
        version: 1,
        kind: "project",
        projectPath: "/workspace",
        sectionKey: "project:/workspace"
      },
      submitDiagnostics: { submittedAtUnixMs: 1 },
      mode: "new",
      settings: { model: "model-1" },
      timeoutMs: 90_000,
      title: "New session",
      type: "session/activate",
      workspaceId: "workspace-1"
    }
  ]);
});

test("control activation can carry content without expecting a Turn", () => {
  const intent = {
    ...activation(),
    content: [{ type: "text" as const, text: "/goal ship it" }],
    initialTurnExpected: false,
    runtimeContent: [{ type: "text" as const, text: "/goal ship it" }]
  };
  const result = reduce(createInitialPendingIntentsState(), intent);
  const pending = result.state.activationsByRequestId["activation-1"];
  assert.equal(pending?.initialTurnExpected, false);
  assert.deepEqual(
    result.commands.find((command) => command.type === "session/activate"),
    {
      agentSessionId: "session-new",
      agentTargetId: "target-1",
      clientSubmitId: "submit-new",
      commandId: "activate:activation-1",
      correlationId: "activation-1",
      cwd: "/workspace",
      initialContent: [{ type: "text", text: "/goal ship it" }],
      initialDisplayPrompt: "/browser",
      railPlacement: {
        version: 1,
        kind: "project",
        projectPath: "/workspace",
        sectionKey: "project:/workspace"
      },
      submitDiagnostics: { submittedAtUnixMs: 1 },
      mode: "new",
      settings: { model: "model-1" },
      timeoutMs: 90_000,
      title: "New session",
      type: "session/activate",
      workspaceId: "workspace-1"
    }
  );
});

test("timed out activation remains uncertain until its exact session appears", () => {
  let state = reduce(createInitialPendingIntentsState(), activation()).state;
  const timedOut = reduce(state, {
    commandId: "activate:activation-1",
    commandType: "session/activate",
    correlationId: "activation-1",
    outcome: "timedOut",
    type: "engine/commandResult"
  });
  state = timedOut.state;
  assert.deepEqual(timedOut.followUpIntents, [
    {
      agentSessionId: "session-new",
      needsMessages: false,
      needsState: true,
      type: "session/reconcileRequested",
      workspaceId: "workspace-1"
    }
  ]);
  assert.equal(
    state.activationsByRequestId["activation-1"]?.status,
    "uncertain"
  );
  state = reduce(state, {
    sessions: [session("another-session")],
    type: "session/snapshotReceived"
  }).state;
  assert.equal(
    state.activationsByRequestId["activation-1"]?.status,
    "uncertain"
  );
  state = reduce(state, {
    sessions: [session("session-new")],
    type: "session/snapshotReceived"
  }).state;
  assert.equal(
    state.activationsByRequestId["activation-1"]?.status,
    "confirmed"
  );
});

test("a realtime session upsert confirms its pending activation", () => {
  const state = reduce(createInitialPendingIntentsState(), activation()).state;
  const confirmed = reduce(state, {
    session: { ...session("session-new"), createdAtUnixMs: 1 },
    type: "session/upserted"
  });
  assert.equal(
    confirmed.state.activationsByRequestId["activation-1"]?.status,
    "confirmed"
  );
});

test("a pre-admitted binding with stale createdAt confirms via updatedAt", () => {
  const state = reduce(createInitialPendingIntentsState(), activation()).state;
  const confirmed = reduce(state, {
    session: {
      ...session("session-new"),
      createdAtUnixMs: 0,
      updatedAtUnixMs: 1
    },
    type: "session/upserted"
  });
  assert.equal(
    confirmed.state.activationsByRequestId["activation-1"]?.status,
    "confirmed"
  );
});

test("a stale pre-admitted binding without post-request update stays requested", () => {
  const state = reduce(createInitialPendingIntentsState(), activation()).state;
  const stillPending = reduce(state, {
    session: {
      ...session("session-new"),
      createdAtUnixMs: 0,
      updatedAtUnixMs: 0
    },
    type: "session/upserted"
  });
  assert.equal(
    stillPending.state.activationsByRequestId["activation-1"]?.status,
    "requested"
  );
});

test("authoritative history retracts only the optimistic initial prompt", () => {
  let state = reduce(createInitialPendingIntentsState(), activation()).state;
  state = reduce(state, {
    sessions: [session("session-new")],
    type: "session/snapshotReceived"
  }).state;

  const result = reduce(state, {
    agentSessionId: "session-new",
    childSessions: [],
    historyRevision: 1,
    messages: [],
    session: session("session-new"),
    turns: [],
    type: "session/historyAuthoritativeSnapshotReceived",
    workspaceId: "workspace-1"
  });

  const retained = result.state.activationsByRequestId["activation-1"];
  assert.equal(retained?.status, "confirmed");
  assert.equal(retained?.initialPromptRetracted, true);
  assert.equal(retained?.clientSubmitId, "submit-new");
  assert.deepEqual(retained?.settings, { model: "model-1" });
});

test("authoritative activation failure is retained for the view to dismiss", () => {
  let state = reduce(createInitialPendingIntentsState(), activation()).state;
  state = reduce(state, {
    commandId: "activate:activation-1",
    commandType: "session/activate",
    correlationId: "activation-1",
    outcome: "succeeded",
    type: "engine/commandResult",
    value: {
      activation: { status: "failed" },
      error: { code: "auth_required", message: "Sign in required" },
      session: { ...session("session-new"), status: "failed" }
    }
  }).state;
  const record = state.activationsByRequestId["activation-1"];
  assert.equal(record?.status, "failed");
  assert.equal(record?.errorCode, "auth_required");
  assert.equal(record?.errorMessage, "Sign in required");
});

test("invalid successful activation acknowledgement remains uncertain", () => {
  let state = reduce(createInitialPendingIntentsState(), activation()).state;
  state = reduce(state, {
    commandId: "activate:activation-1",
    commandType: "session/activate",
    correlationId: "activation-1",
    outcome: "succeeded",
    type: "engine/commandResult",
    value: { unexpected: true }
  }).state;
  assert.equal(
    state.activationsByRequestId["activation-1"]?.status,
    "uncertain"
  );
  assert.equal(
    state.activationsByRequestId["activation-1"]?.errorCode,
    "invalid_command_result"
  );
});

test("legacy activation results remain opaque acknowledgements", () => {
  const state = reduce(
    createInitialPendingIntentsState(),
    existingActivation()
  ).state;
  const result = reduce(state, {
    commandId: "activate:activation-existing",
    commandType: "session/activate",
    correlationId: "activation-existing",
    outcome: "succeeded",
    type: "engine/commandResult",
    value: {
      activation: { mode: "existing", status: "already_attached" },
      session: session("session-new")
    }
  });

  assert.equal(
    result.state.activationsByRequestId["activation-existing"]?.status,
    "requested"
  );
  assert.deepEqual(result.followUpIntents, undefined);
});

test("typed new-session activation returns its authoritative Session to the Engine", () => {
  const state = reduce(createInitialPendingIntentsState(), activation()).state;
  const authoritativeSession = {
    ...session("session-new"),
    createdAtUnixMs: 2
  };
  const result = reduce(state, {
    commandId: "activate:activation-1",
    commandType: "session/activate",
    correlationId: "activation-1",
    outcome: "succeeded",
    resultContract: "activation-v1",
    type: "engine/commandResult",
    value: {
      activation: { mode: "new", status: "attached" },
      session: authoritativeSession
    }
  });

  assert.deepEqual(result.followUpIntents, [
    { session: authoritativeSession, type: "session/upserted" }
  ]);
  assert.equal(
    result.state.activationsByRequestId["activation-1"]?.status,
    "requested"
  );
});

test("typed existing-session activation returns its detail aggregate to the Engine", () => {
  const state = reduce(
    createInitialPendingIntentsState(),
    existingActivation()
  ).state;
  const authoritativeSession = session("session-new");
  const turn = { ...runningTurn(), agentSessionId: "session-new" };
  const result = reduce(state, {
    commandId: "activate:activation-existing",
    commandType: "session/activate",
    correlationId: "activation-existing",
    outcome: "succeeded",
    resultContract: "activation-v1",
    type: "engine/commandResult",
    value: {
      activation: { mode: "existing", status: "already_attached" },
      detail: {
        childSessions: [],
        lifecycleCapabilitiesProjected: true,
        projection: "authoritative",
        session: authoritativeSession,
        turns: [turn]
      },
      session: authoritativeSession
    }
  });

  assert.deepEqual(result.followUpIntents, [
    {
      childSessions: [],
      session: authoritativeSession,
      turns: [turn],
      type: "session/detailSnapshotReceived",
      workspaceId: "workspace-1"
    }
  ]);
});

test("typed activation results cannot omit or escape their requested scope", () => {
  const newState = reduce(
    createInitialPendingIntentsState(),
    activation()
  ).state;
  const missingSession = reduce(newState, {
    commandId: "activate:activation-1",
    commandType: "session/activate",
    correlationId: "activation-1",
    outcome: "succeeded",
    resultContract: "activation-v1",
    type: "engine/commandResult",
    value: {
      activation: { mode: "new", status: "attached" }
    }
  });
  assert.equal(
    missingSession.state.activationsByRequestId["activation-1"]?.status,
    "uncertain"
  );
  assert.deepEqual(missingSession.followUpIntents, [
    {
      agentSessionId: "session-new",
      needsMessages: false,
      needsState: true,
      type: "session/reconcileRequested",
      workspaceId: "workspace-1"
    }
  ]);

  const existingState = reduce(
    createInitialPendingIntentsState(),
    existingActivation()
  ).state;
  const escapedChild = reduce(existingState, {
    commandId: "activate:activation-existing",
    commandType: "session/activate",
    correlationId: "activation-existing",
    outcome: "succeeded",
    resultContract: "activation-v1",
    type: "engine/commandResult",
    value: {
      activation: { mode: "existing", status: "already_attached" },
      detail: {
        childSessions: [
          {
            ...session("session-child"),
            workspaceId: "workspace-other"
          }
        ],
        lifecycleCapabilitiesProjected: true,
        projection: "authoritative",
        session: session("session-new"),
        turns: []
      },
      session: session("session-new")
    }
  });
  assert.equal(
    escapedChild.state.activationsByRequestId["activation-existing"]?.status,
    "uncertain"
  );
  assert.deepEqual(escapedChild.followUpIntents, undefined);
});

test("typed activation rejects malformed nested Session entities", () => {
  const state = reduce(createInitialPendingIntentsState(), activation()).state;
  const result = reduce(state, {
    commandId: "activate:activation-1",
    commandType: "session/activate",
    correlationId: "activation-1",
    outcome: "succeeded",
    resultContract: "activation-v1",
    type: "engine/commandResult",
    value: {
      activation: { mode: "new", status: "attached" },
      session: {
        ...session("session-new"),
        pendingInteractions: [null]
      }
    }
  });

  assert.equal(
    result.state.activationsByRequestId["activation-1"]?.errorCode,
    "invalid_command_result"
  );
  assert.equal(
    result.state.activationsByRequestId["activation-1"]?.status,
    "uncertain"
  );
  assert.deepEqual(result.followUpIntents, [
    {
      agentSessionId: "session-new",
      needsMessages: false,
      needsState: true,
      type: "session/reconcileRequested",
      workspaceId: "workspace-1"
    }
  ]);
});

test("confirmed activation may hydrate detail but cannot be failed by a late result", () => {
  let state = reduce(
    createInitialPendingIntentsState(),
    existingActivation()
  ).state;
  state = reduce(state, {
    session: session("session-new"),
    type: "session/upserted"
  }).state;
  assert.equal(
    state.activationsByRequestId["activation-existing"]?.status,
    "confirmed"
  );

  const failed = reduce(state, {
    commandId: "activate:activation-existing",
    commandType: "session/activate",
    correlationId: "activation-existing",
    outcome: "succeeded",
    resultContract: "activation-v1",
    type: "engine/commandResult",
    value: {
      activation: { mode: "existing", status: "failed" },
      error: { code: "late_failure", message: "late failure" }
    }
  });
  assert.equal(
    failed.state.activationsByRequestId["activation-existing"]?.status,
    "confirmed"
  );
  assert.equal(
    failed.state.activationsByRequestId["activation-existing"]?.commandOutcome,
    "failed"
  );
  assert.equal(
    failed.state.activationsByRequestId["activation-existing"]
      ?.lastObservedStage,
    "stale_command_result"
  );
  assert.deepEqual(failed.followUpIntents, undefined);

  const hydrated = reduce(state, {
    commandId: "activate:activation-existing",
    commandType: "session/activate",
    correlationId: "activation-existing",
    outcome: "succeeded",
    resultContract: "activation-v1",
    type: "engine/commandResult",
    value: {
      activation: { mode: "existing", status: "already_attached" },
      detail: {
        childSessions: [],
        lifecycleCapabilitiesProjected: true,
        projection: "authoritative",
        session: session("session-new"),
        turns: [{ ...runningTurn(), agentSessionId: "session-new" }]
      },
      session: session("session-new")
    }
  });
  assert.equal(
    hydrated.state.activationsByRequestId["activation-existing"]?.status,
    "confirmed"
  );
  assert.equal(
    hydrated.state.activationsByRequestId["activation-existing"]
      ?.commandOutcome,
    "succeeded"
  );
  assert.equal(
    hydrated.followUpIntents?.[0]?.type,
    "session/detailSnapshotReceived"
  );
});

test("activation confirmation requires an exact workspace-scoped fresh snapshot", () => {
  let state = reduce(createInitialPendingIntentsState(), activation()).state;
  state = reduce(state, {
    sessions: [
      {
        ...{
          activeTurnId: null,
          latestTurnInteractions: [],
          pendingInteractions: []
        },
        ...session("session-new"),
        createdAtUnixMs: 1,
        workspaceId: "workspace-other"
      }
    ],
    type: "session/snapshotReceived"
  }).state;
  assert.equal(
    state.activationsByRequestId["activation-1"]?.status,
    "requested"
  );
  assert.equal(
    state.activationsByRequestId["activation-1"]?.snapshotOutcome,
    "workspace_mismatch"
  );
  assert.equal(
    state.activationsByRequestId["activation-1"]?.lastObservedStage,
    "snapshot_observed"
  );
});

test("activation diagnostics preserve command and snapshot evidence through expiry", () => {
  let state = reduce(createInitialPendingIntentsState(), activation()).state;
  state = reduce(state, {
    commandId: "activate:activation-1",
    commandType: "session/activate",
    correlationId: "activation-1",
    outcome: "timedOut",
    settledAtUnixMs: 90_001,
    type: "engine/commandResult"
  }).state;
  state = reduce(state, {
    sessions: [],
    type: "session/snapshotReceived"
  }).state;
  const expired = reduce(state, {
    dueAtUnixMs: 120_000,
    expiryId: "activation:activation-1",
    type: "engine/intentExpired"
  });
  assert.deepEqual(expired.followUpIntents, [
    {
      agentSessionId: "session-new",
      needsMessages: false,
      needsState: true,
      type: "session/reconcileRequested",
      workspaceId: "workspace-1"
    }
  ]);
  const activationRecord = expired.state.activationsByRequestId["activation-1"];
  assert.equal(activationRecord?.status, "failed");
  assert.equal(activationRecord?.commandOutcome, "timed_out");
  assert.equal(activationRecord?.commandSettledAtUnixMs, 90_001);
  assert.equal(activationRecord?.snapshotOutcome, "session_missing");
  assert.equal(activationRecord?.lastObservedStage, "expired");
  assert.equal(activationRecord?.errorCode, "activation_confirmation_expired");
});

test("repeated identical activation snapshot evidence preserves state identity", () => {
  let state = reduce(createInitialPendingIntentsState(), activation()).state;
  state = reduce(state, {
    observedAtUnixMs: 1_000,
    sessions: [],
    type: "session/snapshotReceived"
  }).state;
  const repeated = reduce(state, {
    observedAtUnixMs: 2_000,
    sessions: [],
    type: "session/snapshotReceived"
  });

  assert.equal(repeated.state, state);
  assert.equal(
    repeated.state.activationsByRequestId["activation-1"]
      ?.snapshotObservedAtUnixMs,
    1_000
  );
});

test("repeated snapshot evidence after command settlement preserves first observation time", () => {
  let state = reduce(createInitialPendingIntentsState(), activation()).state;
  state = reduce(state, {
    observedAtUnixMs: 1_000,
    sessions: [],
    type: "session/snapshotReceived"
  }).state;
  state = reduce(state, {
    commandId: "activate:activation-1",
    commandType: "session/activate",
    correlationId: "activation-1",
    outcome: "timedOut",
    settledAtUnixMs: 2_000,
    type: "engine/commandResult"
  }).state;

  const repeated = reduce(state, {
    observedAtUnixMs: 3_000,
    sessions: [],
    type: "session/snapshotReceived"
  }).state.activationsByRequestId["activation-1"];

  assert.equal(repeated?.snapshotOutcome, "session_missing");
  assert.equal(repeated?.snapshotObservedAtUnixMs, 1_000);
  assert.equal(repeated?.lastObservedStage, "snapshot_observed");
});

test("stopping after activation command success preserves the command outcome", () => {
  let state = reduce(createInitialPendingIntentsState(), activation()).state;
  state = reduce(state, {
    commandId: "activate:activation-1",
    commandType: "session/activate",
    correlationId: "activation-1",
    outcome: "succeeded",
    settledAtUnixMs: 1_000,
    type: "engine/commandResult",
    value: {
      activation: { mode: "new", status: "attached" },
      session: { ...session("session-new"), createdAtUnixMs: 1 }
    }
  }).state;
  const stopped = reduce(state, {
    agentSessionId: "session-new",
    type: "session/stopRequested",
    workspaceId: "workspace-1"
  }).state.activationsByRequestId["activation-1"];

  assert.equal(stopped?.status, "canceled");
  assert.equal(stopped?.commandOutcome, "succeeded");
  assert.equal(stopped?.lastObservedStage, "canceled");
});

test("late command failure cannot downgrade a snapshot-confirmed activation", () => {
  let state = reduce(createInitialPendingIntentsState(), activation()).state;
  state = reduce(state, {
    sessions: [
      {
        ...session("session-new"),
        createdAtUnixMs: 2
      }
    ],
    type: "session/snapshotReceived"
  }).state;
  const settled = reduce(state, {
    commandId: "activate:activation-1",
    commandType: "session/activate",
    correlationId: "activation-1",
    errorCode: "request_timed_out",
    errorMessage: "late timeout",
    outcome: "failed",
    settledAtUnixMs: 30_001,
    type: "engine/commandResult"
  }).state.activationsByRequestId["activation-1"];
  assert.equal(settled?.status, "confirmed");
  assert.equal(settled?.snapshotOutcome, "matched");
  assert.equal(settled?.commandOutcome, "failed");
  assert.equal(settled?.lastObservedStage, "stale_command_result");
  assert.equal(settled?.errorCode, null);
});

test("confirmed activation emits its request-scoped pending settings command once", () => {
  let state = reduce(createInitialPendingIntentsState(), activation()).state;
  state = reduce(state, {
    agentSessionId: "session-new",
    settings: { model: "model-2" },
    type: "activation/settingsPatched"
  }).state;
  const confirmed = reduce(state, {
    commandId: "activate:activation-1",
    commandType: "session/activate",
    correlationId: "activation-1",
    outcome: "succeeded",
    type: "engine/commandResult",
    value: {
      activation: { status: "active" },
      session: { ...session("session-new"), createdAtUnixMs: 1 }
    }
  });
  assert.deepEqual(confirmed.commands, []);
  const attached = reduce(confirmed.state, {
    sessions: [{ ...session("session-new"), createdAtUnixMs: 1 }],
    type: "session/snapshotReceived"
  });
  assert.equal(attached.followUpIntents, undefined);
  assert.deepEqual(attached.commands, [
    {
      agentSessionId: "session-new",
      commandId: "activation-settings:activation-1",
      correlationId: "session-new",
      settings: { model: "model-2" },
      type: "session/updateSettings",
      workspaceId: "workspace-1"
    }
  ]);
  assert.deepEqual(
    reduce(attached.state, {
      sessions: [{ ...session("session-new"), createdAtUnixMs: 1 }],
      type: "session/snapshotReceived"
    }).commands,
    []
  );
  const settingsSucceeded = reduce(attached.state, {
    commandId: "activation-settings:activation-1",
    commandType: "session/updateSettings",
    correlationId: "session-new",
    outcome: "succeeded",
    type: "engine/commandResult",
    value: {
      agentSessionId: "session-new",
      session: { ...session("session-new"), settings: { model: "model-2" } }
    }
  });
  assert.equal(
    settingsSucceeded.state.activationsByRequestId["activation-1"]
      ?.pendingSettingsPatch,
    undefined
  );
});

test("settings update failure remains request-scoped and retryable without double send", () => {
  let state = reduce(createInitialPendingIntentsState(), activation()).state;
  state = reduce(state, {
    agentSessionId: "session-new",
    settings: { model: "model-2" },
    type: "activation/settingsPatched"
  }).state;
  const confirmed = reduce(state, {
    commandId: "activate:activation-1",
    commandType: "session/activate",
    correlationId: "activation-1",
    outcome: "succeeded",
    type: "engine/commandResult",
    value: {
      activation: { status: "active" },
      session: { ...session("session-new"), createdAtUnixMs: 1 }
    }
  });
  const attached = reduce(confirmed.state, {
    sessions: [{ ...session("session-new"), createdAtUnixMs: 1 }],
    type: "session/snapshotReceived"
  });
  assert.equal(attached.commands[0]?.type, "session/updateSettings");
  const failed = reduce(attached.state, {
    commandId: "activation-settings:activation-1",
    commandType: "session/updateSettings",
    correlationId: "session-new",
    errorMessage: "settings failed",
    outcome: "failed",
    type: "engine/commandResult"
  });
  assert.equal(
    failed.state.activationsByRequestId["activation-1"]?.settingsUpdateStatus,
    "failed"
  );
  assert.deepEqual(
    failed.state.activationsByRequestId["activation-1"]?.pendingSettingsPatch,
    { model: "model-2" }
  );
  const retried = reduce(failed.state, {
    agentSessionId: "session-new",
    settings: { model: "model-3" },
    type: "activation/settingsPatched"
  });
  assert.equal(retried.commands[0]?.type, "session/updateSettings");
  const timedOut = reduce(retried.state, {
    commandId: "activation-settings:activation-1",
    commandType: "session/updateSettings",
    correlationId: "session-new",
    outcome: "timedOut",
    type: "engine/commandResult"
  });
  assert.equal(
    timedOut.state.activationsByRequestId["activation-1"]?.settingsUpdateStatus,
    "unknown"
  );
});

test("failed, superseded, and late reused activations never flush settings", () => {
  let state = reduce(createInitialPendingIntentsState(), activation()).state;
  state = reduce(state, {
    ...activation(),
    requestId: "activation-2",
    requestedAtUnixMs: 10
  }).state;
  state = reduce(state, {
    agentSessionId: "session-new",
    settings: { model: "model-2" },
    type: "activation/settingsPatched"
  }).state;
  const oldConfirmed = reduce(state, {
    commandId: "activate:activation-1",
    commandType: "session/activate",
    correlationId: "activation-1",
    outcome: "succeeded",
    type: "engine/commandResult",
    value: {
      activation: { status: "active" },
      session: { ...session("session-new"), createdAtUnixMs: 1 }
    }
  });
  assert.deepEqual(oldConfirmed.commands, []);
  const lateReuse = reduce(oldConfirmed.state, {
    sessions: [{ ...session("session-new"), createdAtUnixMs: 2 }],
    type: "session/snapshotReceived"
  });
  assert.deepEqual(lateReuse.commands, []);
  const failed = reduce(lateReuse.state, {
    commandId: "activate:activation-2",
    commandType: "session/activate",
    correlationId: "activation-2",
    errorMessage: "failed",
    outcome: "failed",
    type: "engine/commandResult"
  });
  assert.deepEqual(failed.commands, []);
});

test("same-session activation requests are latest-wins and old results cannot revive", () => {
  let state = reduce(createInitialPendingIntentsState(), activation()).state;
  const newer = reduce(state, {
    ...activation(),
    requestId: "activation-2",
    requestedAtUnixMs: 10
  });
  assert.equal(newer.state.activationsByRequestId["activation-1"], undefined);
  assert.equal(
    newer.state.activationsByRequestId["activation-2"]?.status,
    "requested"
  );
  assert.equal(newer.commands[0]?.type, "engine/cancelExpiry");
  const lateOldResult = reduce(newer.state, {
    commandId: "activate:activation-1",
    commandType: "session/activate",
    correlationId: "activation-1",
    outcome: "succeeded",
    type: "engine/commandResult",
    value: {
      activation: { status: "active" },
      session: { ...session("session-new"), createdAtUnixMs: 1 }
    }
  });
  assert.deepEqual(lateOldResult.commands, []);
  assert.equal(
    lateOldResult.state.activationsByRequestId["activation-2"]?.status,
    "requested"
  );
});

function submit() {
  return {
    type: "submit/requested" as const,
    agentSessionId: "session-1",
    clientSubmitId: "submit-1",
    content: [{ type: "text" as const, text: "hello" }],
    expiresAtUnixMs: 60_000,
    requestedAtUnixMs: 1,
    workspaceId: "workspace-1"
  };
}

function activation() {
  return {
    type: "activation/requested" as const,
    agentSessionId: "session-new",
    agentTargetId: "target-1",
    clientSubmitId: "submit-new",
    content: [{ type: "text" as const, text: "hello" }],
    cwd: "/workspace",
    expiresAtUnixMs: 120_000,
    initialDisplayPrompt: "/browser",
    optimisticTitle: "Review browser flow",
    railPlacement: {
      version: 1 as const,
      kind: "project" as const,
      projectPath: "/workspace",
      sectionKey: "project:/workspace"
    },
    railSectionKey: "project:/workspace",
    runtimeContent: [{ type: "text" as const, text: "runtime instructions" }],
    submitDiagnostics: { submittedAtUnixMs: 1 },
    mode: "new" as const,
    requestedAtUnixMs: 1,
    requestId: "activation-1",
    settings: { model: "model-1" },
    title: "New session",
    workspaceId: "workspace-1"
  };
}

function existingActivation() {
  const {
    clientSubmitId: _clientSubmitId,
    optimisticTitle: _optimisticTitle,
    ...input
  } = activation();
  return {
    ...input,
    mode: "existing" as const,
    requestId: "activation-existing"
  };
}

function session(agentSessionId: string) {
  return {
    activeTurnId: null,
    agentSessionId,
    cwd: "/workspace",
    createdAtUnixMs: 1,
    provider: "codex",
    latestTurnInteractions: [],
    pendingInteractions: [],
    status: "ready",
    title: "Session",
    workspaceId: "workspace-1"
  };
}

function runningTurn() {
  return {
    agentSessionId: "session-1",
    origin: "user_prompt" as const,
    phase: "running" as const,
    startedAtUnixMs: 1,
    turnId: "turn-1",
    updatedAtUnixMs: 1
  };
}

function message(clientSubmitId: string) {
  return {
    agentSessionId: "session-1",
    kind: "text",
    messageId: `message-${clientSubmitId}`,
    occurredAtUnixMs: 2,
    payload: { clientSubmitId },
    role: "user",
    sequence: 1,
    turnId: "turn-1",
    version: 1
  };
}

function reduce(
  state: ReturnType<typeof createInitialPendingIntentsState>,
  intent: Parameters<typeof pendingIntentsReducer>[1]
) {
  return pendingIntentsReducer(state, intent, {
    deletedSessionIds: {},
    turnsById: {},
    sendResultValidation:
      intent.type === "engine/commandResult" &&
      intent.commandType === "queue/sendPrompt" &&
      intent.outcome === "succeeded"
        ? validateSendInputResult(
            intent.value,
            state.submitsByClientSubmitId[intent.correlationId?.trim() ?? ""]
          )
        : null,
    settingsResultValidation:
      intent.type === "engine/commandResult" &&
      intent.commandType === "session/updateSettings" &&
      intent.outcome === "succeeded"
        ? (() => {
            const agentSessionId = intent.correlationId?.trim() ?? "";
            const activation = Object.values(state.activationsByRequestId).find(
              (candidate) =>
                candidate.agentSessionId === agentSessionId &&
                intent.commandId ===
                  `activation-settings:${candidate.requestId}`
            );
            return validateScopedSessionResult(
              intent.value,
              activation
                ? {
                    agentSessionId: activation.agentSessionId,
                    workspaceId: activation.workspaceId
                  }
                : undefined,
              true
            );
          })()
        : null
  });
}
