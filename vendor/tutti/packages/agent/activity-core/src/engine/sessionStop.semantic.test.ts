import assert from "node:assert/strict";
import { test } from "node:test";
import { normalizeAgentActivitySession } from "../sessionNormalization.ts";
import type { AgentActivityTurn } from "../types.ts";
import { createAgentSessionEngine } from "./createAgentSessionEngine.ts";
import {
  selectLatestStopTargetSubmitForSession,
  selectSessionHasPendingSubmitStopTarget
} from "./pendingIntents.selectors.ts";
import type { PendingSubmitIntentRecord } from "./pendingIntents.types.ts";
import { createInitialAgentSessionEngineState } from "./rootReducer.ts";
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

function stopTargetFor(input: {
  pending: Partial<PendingSubmitIntentRecord>;
  queue?: Record<string, unknown>;
  turnPhase?: AgentActivityTurn["phase"];
}) {
  const initial = createInitialAgentSessionEngineState();
  const record = {
    agentSessionId: "session-1",
    clientSubmitId: "submit-1",
    requestedAtUnixMs: 1,
    status: "requested",
    turnId: null,
    workspaceId: "workspace-1",
    ...input.pending
  } as PendingSubmitIntentRecord;
  const state = {
    ...initial,
    pendingIntents: {
      ...initial.pendingIntents,
      submitsByClientSubmitId: { "submit-1": record }
    },
    promptQueue: {
      ...initial.promptQueue,
      recordsBySessionId: input.queue ? { "session-1": input.queue } : {}
    },
    sessionLifecycle: {
      ...initial.sessionLifecycle,
      turnsById: input.turnPhase
        ? {
            [canonicalTurnKey("session-1", "turn-1")]: {
              agentSessionId: "session-1",
              phase: input.turnPhase,
              turnId: "turn-1"
            }
          }
        : {}
    }
  } as unknown as Parameters<typeof selectLatestStopTargetSubmitForSession>[0];
  return selectLatestStopTargetSubmitForSession(state, "session-1");
}

const queuedPrompt = { clientSubmitId: "submit-1", id: "prompt-1" };
const queuedPending = { submitDiagnostics: { queued: true } } as Partial<PendingSubmitIntentRecord>;

test("stop target selection does not target a queued prompt never dispatched", () => {
  assert.equal(stopTargetFor({ pending: queuedPending, queue: { prompts: [queuedPrompt] } }), null);
});

test("stop target selection keeps a queued prompt during in-flight and uncertain delivery", () => {
  for (const delivery of [{ inFlight: { promptId: "prompt-1" } }, { uncertainDelivery: { promptId: "prompt-1" } }]) {
    const target = stopTargetFor({ pending: queuedPending, queue: { prompts: [queuedPrompt], ...delivery } });
    assert.equal(target?.clientSubmitId, "submit-1");
  }
});

test("stop target selection never correlates a queued prompt by prompt id alone", () => {
  const other = { clientSubmitId: "other", id: "prompt-1" };
  assert.equal(stopTargetFor({ pending: queuedPending, queue: { prompts: [other], inFlight: { promptId: "prompt-1" } } }), null);
});

test("stop target selection keeps immediate submits until their turn settles", () => {
  assert.equal(stopTargetFor({ pending: {} })?.clientSubmitId, "submit-1");
  assert.equal(stopTargetFor({ pending: { status: "accepted", turnId: "turn-1" } })?.clientSubmitId, "submit-1");
  assert.equal(stopTargetFor({ pending: { status: "accepted", turnId: "turn-1" }, turnPhase: "running" })?.clientSubmitId, "submit-1");
  assert.equal(stopTargetFor({ pending: { status: "accepted", turnId: "turn-1" }, turnPhase: "settled" }), null);
});

test("stop target selection keeps a delivery-unknown submit and drops a failed one", () => {
  assert.equal(stopTargetFor({ pending: { status: "uncertain" } })?.clientSubmitId, "submit-1");
  assert.equal(stopTargetFor({ pending: { status: "failed" } }), null);
});
