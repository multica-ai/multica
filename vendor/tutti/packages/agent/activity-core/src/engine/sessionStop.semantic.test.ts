import assert from "node:assert/strict";
import { test } from "node:test";
import { normalizeAgentActivitySession } from "../sessionNormalization.ts";
import type { AgentActivityTurn } from "../types.ts";
import { createAgentSessionEngine } from "./createAgentSessionEngine.ts";
import {
  selectLatestStopTargetSubmitForSession,
  selectSessionHasPendingSubmitStopTarget
} from "./pendingIntents.selectors.ts";
import { canonicalTurnKey } from "./sessionEntityKeys.ts";
import { createTestEngineCommandPort } from "./testEngineCommandPort.ts";
import type { EngineExternalCommand, EngineScheduler } from "./types.ts";

function createHarness(active: boolean) {
  let nowUnixMs = 100;
  const commands: EngineExternalCommand[] = [];
  const scheduled: Array<{
    canceled: boolean;
    delayMs: number;
  }> = [];
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
    clock: { nowUnixMs: () => nowUnixMs },
    commandPort,
    identity: { origin: "test", workspaceId: "workspace-1" },
    scheduler
  });
  engine.dispatch({
    sessions: [session(active ? activeTurn() : null)],
    type: "session/snapshotReceived"
  });
  return {
    commands,
    engine,
    scheduled,
    setNow(value: number) {
      nowUnixMs = value;
    }
  };
}

test("stop target selection does not target a visible queued prompt that was never dispatched", () => {
  const harness = createHarness(true);

  assert.deepEqual(
    harness.engine.submitPrompt({
      agentSessionId: "session-1",
      clientSubmitId: "submit-queued",
      content: [{ text: "send after the turn", type: "text" }]
    }),
    { accepted: true, queued: true }
  );

  harness.engine.dispatch({
    agentSessionId: "session-1",
    type: "session/stopRequested",
    workspaceId: "workspace-1"
  });

  const turn = settledTurn();
  harness.engine.dispatch({
    sessions: [
      {
        ...session(null),
        latestTurn: turn,
        updatedAtUnixMs: turn.updatedAtUnixMs
      }
    ],
    type: "session/snapshotReceived"
  });

  const snapshot = harness.engine.getSnapshot();
  assert.equal(
    snapshot.promptQueue.recordsBySessionId["session-1"]?.suspendReason,
    "user_stop"
  );
  assert.equal(
    snapshot.promptQueue.recordsBySessionId["session-1"]?.inFlight,
    null
  );
  assert.equal(
    selectSessionHasPendingSubmitStopTarget(snapshot, "session-1"),
    false
  );
});

test("stop target selection keeps a queued prompt during in-flight and uncertain delivery", () => {
  const harness = createHarness(true);

  assert.deepEqual(
    harness.engine.submitPrompt({
      agentSessionId: "session-1",
      clientSubmitId: "submit-queued",
      content: [{ text: "send after the turn", type: "text" }]
    }),
    { accepted: true, queued: true }
  );
  const turn = settledTurn();
  harness.engine.dispatch({
    sessions: [
      {
        ...session(null),
        latestTurn: turn,
        updatedAtUnixMs: turn.updatedAtUnixMs
      }
    ],
    type: "session/snapshotReceived"
  });

  const snapshot = harness.engine.getSnapshot();
  assert.equal(
    snapshot.promptQueue.recordsBySessionId["session-1"]?.inFlight?.promptId,
    "submit-queued"
  );
  assert.equal(
    selectSessionHasPendingSubmitStopTarget(snapshot, "session-1"),
    true
  );

  const commandId =
    snapshot.promptQueue.recordsBySessionId["session-1"]?.inFlight?.commandId;
  assert.ok(commandId);
  harness.engine.dispatch({
    commandId,
    commandType: "queue/sendPrompt",
    correlationId: "submit-queued",
    errorCode: "timeout",
    outcome: "timedOut",
    type: "engine/commandResult"
  });

  const uncertainSnapshot = harness.engine.getSnapshot();
  assert.equal(
    uncertainSnapshot.promptQueue.recordsBySessionId["session-1"]
      ?.uncertainDelivery?.promptId,
    "submit-queued"
  );
  assert.equal(
    selectSessionHasPendingSubmitStopTarget(uncertainSnapshot, "session-1"),
    true
  );
});

test("stop target selection never correlates a queued prompt by prompt id alone", () => {
  const harness = createHarness(true);

  harness.engine.submitPrompt({
    agentSessionId: "session-1",
    clientSubmitId: "submit-target",
    content: [{ text: "target", type: "text" }]
  });
  harness.engine.dispatch({
    agentSessionId: "session-1",
    promptId: "submit-target",
    type: "queue/removed"
  });
  harness.engine.dispatch({
    agentSessionId: "session-1",
    prompt: {
      clientSubmitId: "other-submit",
      content: [{ text: "other", type: "text" }],
      createdAtUnixMs: 101,
      id: "submit-target"
    },
    type: "queue/enqueued",
    workspaceId: "workspace-1"
  });
  const turn = settledTurn();
  harness.engine.dispatch({
    sessions: [
      {
        ...session(null),
        latestTurn: turn,
        updatedAtUnixMs: turn.updatedAtUnixMs
      }
    ],
    type: "session/snapshotReceived"
  });

  const snapshot = harness.engine.getSnapshot();
  assert.equal(
    snapshot.promptQueue.recordsBySessionId["session-1"]?.inFlight?.promptId,
    "submit-target"
  );
  assert.equal(
    selectSessionHasPendingSubmitStopTarget(snapshot, "session-1"),
    false
  );
});

test("stop target selection keeps immediate submits with missing or running canonical Turn evidence", () => {
  const missingTurnHarness = createHarness(false);
  missingTurnHarness.engine.submitPrompt({
    agentSessionId: "session-1",
    clientSubmitId: "submit-missing-turn",
    content: [{ text: "hello", type: "text" }]
  });
  const missingTurnSnapshot = missingTurnHarness.engine.getSnapshot();
  const missingTurnPending =
    missingTurnSnapshot.pendingIntents.submitsByClientSubmitId[
      "submit-missing-turn"
    ];
  assert.ok(missingTurnPending);
  assert.equal(
    selectSessionHasPendingSubmitStopTarget(
      {
        ...missingTurnSnapshot,
        pendingIntents: {
          ...missingTurnSnapshot.pendingIntents,
          submitsByClientSubmitId: {
            ...missingTurnSnapshot.pendingIntents.submitsByClientSubmitId,
            "submit-missing-turn": {
              ...missingTurnPending,
              turnId: "turn-not-observed"
            }
          }
        }
      },
      "session-1"
    ),
    true
  );

  const runningTurnHarness = createHarness(true);
  runningTurnHarness.engine.submitPrompt({
    agentSessionId: "session-1",
    clientSubmitId: "submit-running-turn",
    content: [{ text: "hello", type: "text" }]
  });
  const runningTurnSnapshot = runningTurnHarness.engine.getSnapshot();
  const runningTurnPending =
    runningTurnSnapshot.pendingIntents.submitsByClientSubmitId[
      "submit-running-turn"
    ];
  assert.ok(runningTurnPending);
  assert.ok(
    runningTurnSnapshot.sessionLifecycle.turnsById[
      canonicalTurnKey("session-1", "turn-1")
    ]
  );
  assert.equal(
    selectSessionHasPendingSubmitStopTarget(
      {
        ...runningTurnSnapshot,
        pendingIntents: {
          ...runningTurnSnapshot.pendingIntents,
          submitsByClientSubmitId: {
            ...runningTurnSnapshot.pendingIntents.submitsByClientSubmitId,
            "submit-running-turn": {
              ...runningTurnPending,
              turnId: "turn-1"
            }
          }
        }
      },
      "session-1"
    ),
    true
  );
});

test("stop target selection does not target a failed submit admission", () => {
  const harness = createHarness(false);

  harness.engine.submitPrompt({
    agentSessionId: "session-1",
    clientSubmitId: "submit-1",
    content: [{ text: "hello", type: "text" }]
  });
  harness.engine.dispatch({
    commandId: "send-1",
    commandType: "queue/sendPrompt",
    correlationId: "submit-1",
    errorCode: "send_failed",
    errorMessage: "send failed",
    outcome: "failed",
    type: "engine/commandResult"
  });

  assert.equal(
    selectLatestStopTargetSubmitForSession(
      harness.engine.getSnapshot(),
      "session-1"
    ),
    null
  );
});

test("stop target selection keeps a delivery-unknown submit as its target", () => {
  const harness = createHarness(false);

  harness.engine.submitPrompt({
    agentSessionId: "session-1",
    clientSubmitId: "submit-1",
    content: [{ text: "hello", type: "text" }]
  });
  harness.engine.dispatch({
    commandId: "send-1",
    commandType: "queue/sendPrompt",
    correlationId: "submit-1",
    errorCode: "timeout",
    outcome: "timedOut",
    type: "engine/commandResult"
  });

  assert.equal(
    selectLatestStopTargetSubmitForSession(
      harness.engine.getSnapshot(),
      "session-1"
    )?.clientSubmitId,
    "submit-1"
  );
});

test("stop target selection ignores a submit whose canonical Turn is settled", () => {
  const harness = createHarness(false);
  const turn = settledTurn();

  harness.engine.submitPrompt({
    agentSessionId: "session-1",
    clientSubmitId: "submit-1",
    content: [{ text: "hello", type: "text" }]
  });
  harness.engine.dispatch({
    commandId: "send-1",
    commandType: "queue/sendPrompt",
    correlationId: "submit-1",
    outcome: "succeeded",
    type: "engine/commandResult",
    value: { session: session(turn), turn, turnId: turn.turnId }
  });

  assert.equal(
    selectLatestStopTargetSubmitForSession(
      harness.engine.getSnapshot(),
      "session-1"
    ),
    null
  );
  assert.equal(
    selectSessionHasPendingSubmitStopTarget(
      harness.engine.getSnapshot(),
      "session-1"
    ),
    false
  );
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

function settledTurn(): AgentActivityTurn {
  return {
    ...activeTurn(),
    outcome: "completed",
    phase: "settled",
    settledAtUnixMs: 3
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
