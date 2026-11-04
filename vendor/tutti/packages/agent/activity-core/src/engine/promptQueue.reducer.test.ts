import assert from "node:assert/strict";
import test from "node:test";
import type {
  AgentActivityInteraction,
  AgentActivitySession,
  AgentActivityTurn
} from "../types.ts";
import { normalizeAgentActivitySession } from "../sessionNormalization.ts";
import type {
  SendInputResultValidation
} from "./commandResult.validation.ts";
import {
  createInitialPromptQueueState,
  promptQueueReducer
} from "./promptQueue.reducer.ts";
import {
  resolvePromptSendNowStrategy,
  resolveQueuedPromptSendNowStrategy,
  type PromptQueueSendNowStrategy
} from "./promptQueue.sendNow.ts";
import { deriveCanonicalSubmitAvailability } from "./sessionLifecycle.availability.ts";
import {
  createInitialSessionLifecycleState,
  sessionLifecycleReducer
} from "./sessionLifecycle.reducer.ts";
import type { SessionLifecycleState } from "./sessionLifecycle.types.ts";
import type { EngineCommand, EngineCommandResultIntent } from "./types.ts";

test("queue drains from canonical lifecycle, not raw session input", () => {
  const running = canonicalLifecycle("running", 1);
  const queued = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    running
  );
  assert.deepEqual(queued.commands, []);

  const settled = reduce(
    queued.state,
    turnUpserted(settledTurn("turn-1", 2)),
    canonicalLifecycle("settled", 2)
  );
  assert.equal(settled.commands[0]?.type, "queue/sendPrompt");
  assert.equal(settled.commands[0]?.commandId, "queue:send:session-1:1");
});

test("available canonical lifecycle sends an enqueued prompt immediately", () => {
  const result = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    canonicalLifecycle("settled", 1)
  );
  assert.equal(result.commands[0]?.type, "queue/sendPrompt");
  assert.deepEqual(
    send(result.commands[0]).submitDiagnostics,
    submitDiagnostics
  );
});

test("queued capability settings and diagnostics survive delivery", () => {
  const queued = reduce(
    createInitialPromptQueueState(),
    {
      ...submit("prompt-capability"),
      requiredSettingsPatch: { computerUse: true }
    },
    canonicalLifecycle("running", 1)
  );
  assert.deepEqual(
    queued.state.recordsBySessionId["session-1"]?.prompts[0]
      ?.requiredSettingsPatch,
    { computerUse: true }
  );
  const sending = reduce(
    queued.state,
    turnUpserted(settledTurn("turn-1", 2)),
    canonicalLifecycle("settled", 2)
  );
  assert.deepEqual(sending.commands, []);
  const execution = sending.followUpIntents?.[0];
  assert.equal(execution?.type, "prompt/executionRequested");
  assert.deepEqual(
    execution?.type === "prompt/executionRequested"
      ? execution.command.requiredSettingsPatch
      : null,
    {
      computerUse: true
    }
  );
});

test("immediate submit bypasses queue storage and preserves diagnostics", () => {
  const result = reduce(
    createInitialPromptQueueState(),
    { ...submit("prompt-immediate"), routing: "immediate" },
    canonicalLifecycle("settled", 1)
  );
  assert.equal(result.state.recordsBySessionId["session-1"], undefined);
  assert.deepEqual(
    send(result.commands[0]).submitDiagnostics,
    submitDiagnostics
  );
});

test("queued Tutti capability reference remains structured through delivery", () => {
  const lifecycle = canonicalLifecycle("running", 1);
  const loaded = reduce(
    createInitialPromptQueueState(),
    {
      type: "session/snapshotReceived",
      sessions: [activitySession("running", 1, "turn-1")]
    },
    lifecycle
  );
  const capabilityRefs = [
    { capability: "tutti" as const, source: "slash_command" as const }
  ];
  const queued = reduce(
    loaded.state,
    {
      ...submit("prompt-tutti"),
      capabilityRefs
    },
    lifecycle
  );

  assert.deepEqual(
    queued.state.recordsBySessionId["session-1"]?.prompts[0]?.capabilityRefs,
    capabilityRefs
  );

  const sending = reduce(
    queued.state,
    turnUpserted(settledTurn("turn-1", 2)),
    canonicalLifecycle("settled", 2)
  );
  assert.deepEqual(send(sending.commands[0]).capabilityRefs, capabilityRefs);
});

test("send-now native guidance can send against a canonical active turn", () => {
  const lifecycle = canonicalLifecycle("running", 1);
  let state = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-guidance"),
    lifecycle
  ).state;
  const guided = reduce(state, sendNow("prompt-guidance"), lifecycle);
  assert.equal(send(guided.commands[0]).guidance, true);
  assert.equal(send(guided.commands[0]).targetTurnId, "turn-1");
});

test("drain after settle strips a stale guidance flag from the queue head", () => {
  const first = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    canonicalLifecycle("settled", 1, "turn-0")
  );
  assert.equal(send(first.commands[0]).promptId, "prompt-1");

  // While prompt-1's turn runs, a second prompt is promoted to steer it.
  const running = canonicalLifecycle("running", 2, "turn-1");
  const queued = reduce(first.state, enqueue("prompt-2"), running);
  const promoted = reduce(queued.state, sendNow("prompt-2"), running);
  assert.deepEqual(promoted.commands, []);
  assert.equal(
    promoted.state.recordsBySessionId["session-1"]?.prompts[0]?.guidance,
    true
  );

  // turn-1 settles before prompt-1's own send is even confirmed and before
  // the promoted steer ever went out: the stale guidance flag must be
  // stripped and prompt-2 drains as a plain new-turn send, not a doomed
  // steer into a turn that no longer exists.
  const settled = reduce(
    promoted.state,
    commandResult(
      commandId(first.commands[0]),
      "queue/sendPrompt",
      "succeeded"
    ),
    canonicalLifecycle("settled", 3, "turn-1"),
    { sendValidation: validSend("turn-1", "settled", 3) }
  );
  assert.equal(send(settled.commands[0]).promptId, "prompt-2");
  assert.equal(send(settled.commands[0]).guidance, undefined);
});

test("guidance failure without the no-active-turn reason still blocks the queue", () => {
  const running = canonicalLifecycle("running", 1);
  const queued = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    running
  );
  const guided = reduce(queued.state, sendNow("prompt-1"), running);
  const failed = reduce(
    guided.state,
    commandResult(commandId(guided.commands[0]), "queue/sendPrompt", "failed", {
      errorMessage: "boom"
    }),
    canonicalLifecycle("settled", 2)
  );
  assert.equal(
    failed.state.recordsBySessionId["session-1"]?.failedPromptId,
    "prompt-1"
  );
  assert.deepEqual(failed.commands, []);
});

test("cancel-then-send waits until the canonical lifecycle settles", () => {
  const running = canonicalLifecycle("running", 1);
  let state = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    running
  ).state;
  const promoted = reduce(state, sendNow("prompt-1"), running, {
    strategy: "cancel_then_send"
  });
  assert.deepEqual(promoted.commands, []);
  assert.equal(
    promoted.state.recordsBySessionId["session-1"]?.sendNextPromptId,
    "prompt-1"
  );

  const canceled = reduce(
    promoted.state,
    turnUpserted(settledTurn("turn-1", 2, "canceled")),
    canonicalLifecycle("settled", 2)
  );
  assert.equal(canceled.commands[0]?.type, "queue/sendPrompt");
  assert.equal(send(canceled.commands[0]).guidance, undefined);
});

test("successful send establishes an exact-turn barrier before the next prompt", () => {
  const available = canonicalLifecycle("settled", 1, "turn-0");
  const first = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    available
  );
  let state = reduce(first.state, enqueue("prompt-2"), available).state;
  const running = canonicalLifecycle("running", 2, "turn-1");
  const accepted = reduce(
    state,
    commandResult(
      commandId(first.commands[0]),
      "queue/sendPrompt",
      "succeeded"
    ),
    running,
    { sendValidation: validSend("turn-1", "running", 2) }
  );
  assert.deepEqual(accepted.commands, []);
  assert.equal(
    accepted.state.recordsBySessionId["session-1"]?.deliveryBarrierTurnId,
    "turn-1"
  );

  const settled = reduce(
    accepted.state,
    turnUpserted(settledTurn("turn-1", 3)),
    canonicalLifecycle("settled", 3, "turn-1")
  );
  assert.equal(send(settled.commands[0]).promptId, "prompt-2");
});

test("late send result drains once when its exact canonical turn already settled", () => {
  const available = canonicalLifecycle("settled", 1, "turn-0");
  const first = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    available
  );
  const withSecond = reduce(first.state, enqueue("prompt-2"), available).state;
  const completed = reduce(
    withSecond,
    commandResult(
      commandId(first.commands[0]),
      "queue/sendPrompt",
      "succeeded"
    ),
    canonicalLifecycle("settled", 3, "turn-1"),
    { sendValidation: validSend("turn-1", "running", 2) }
  );
  assert.equal(send(completed.commands[0]).promptId, "prompt-2");
});

test("pending canonical interaction blocks drain even after the turn settles", () => {
  const lifecycle = canonicalLifecycle("settled", 2, "turn-1", true);
  const queued = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    lifecycle
  );
  assert.deepEqual(queued.commands, []);
  assert.deepEqual(deriveCanonicalSubmitAvailability(lifecycle, "session-1"), {
    state: "blocked",
    reason: "waiting"
  });
});

test("user stop suspension blocks drain until explicit resume", () => {
  const running = canonicalLifecycle("running", 1);
  let state = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    running
  ).state;
  state = reduce(
    state,
    {
      agentSessionId: "session-1",
      type: "session/stopRequested",
      workspaceId: "workspace-1"
    },
    running
  ).state;
  const settled = canonicalLifecycle("settled", 2);
  state = reduce(state, turnUpserted(settledTurn("turn-1", 2)), settled).state;
  const resumed = reduce(
    state,
    { type: "queue/resumed", agentSessionId: "session-1" },
    settled
  );
  assert.equal(resumed.commands[0]?.type, "queue/sendPrompt");
});

test("ordinary submit resuming a user-stopped queue jumps ahead of the stale backlog", () => {
  // The user explicitly stopped the turn, then typed and sent something new.
  // That new prompt is the user's next instruction, not a continuation of
  // whatever was already queued before the stop — it must not sit behind a
  // backlog the user just interrupted.
  const available = canonicalLifecycle("settled", 2);
  let state = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    canonicalLifecycle("running", 1)
  ).state;
  state = reduce(
    state,
    {
      type: "queue/suspended",
      agentSessionId: "session-1",
      reason: "user_stop"
    },
    canonicalLifecycle("running", 1)
  ).state;
  const submitted = reduce(state, submit("prompt-2"), available);
  assert.equal(send(submitted.commands[0]).promptId, "prompt-2");
  assert.deepEqual(
    submitted.state.recordsBySessionId["session-1"]?.prompts.map(
      (prompt) => prompt.id
    ),
    ["prompt-2", "prompt-1"]
  );
});

test("ordinary submit while the queue is merely busy (not user-stopped) preserves FIFO", () => {
  // Contrast with the user-stop case above: an ordinary submit that lands
  // while the queue is simply waiting on availability (no suspension at all)
  // still appends behind whatever is already queued.
  const running = canonicalLifecycle("running", 1);
  const first = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    running
  );
  assert.deepEqual(first.commands, []);
  const submitted = reduce(first.state, submit("prompt-2"), running);
  assert.deepEqual(submitted.commands, []);
  assert.deepEqual(
    submitted.state.recordsBySessionId["session-1"]?.prompts.map(
      (prompt) => prompt.id
    ),
    ["prompt-1", "prompt-2"]
  );
});

test("send failure stays blocked until send-now retry clears it", () => {
  const available = canonicalLifecycle("settled", 1);
  const sending = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    available
  );
  const failed = reduce(
    sending.state,
    commandResult(commandId(sending.commands[0]), "queue/sendPrompt", "failed"),
    available
  );
  assert.equal(
    failed.state.recordsBySessionId["session-1"]?.failedPromptId,
    "prompt-1"
  );
  const retried = reduce(failed.state, sendNow("prompt-1"), available);
  assert.equal(retried.commands[0]?.type, "queue/sendPrompt");
});

test("failed auto submit is removed before a later independent submit", () => {
  const available = canonicalLifecycle("settled", 1);
  const first = reduce(
    createInitialPromptQueueState(),
    submit("prompt-1"),
    available
  );
  const failed = reduce(
    first.state,
    commandResult(commandId(first.commands[0]), "queue/sendPrompt", "failed", {
      errorMessage: "boom"
    }),
    available
  );

  assert.equal(failed.state.recordsBySessionId["session-1"], undefined);

  const second = reduce(failed.state, submit("prompt-2"), available);
  assert.equal(send(second.commands[0]).clientSubmitId, "prompt-2");
});

test("failed hidden head does not block a later queued prompt", () => {
  const available = canonicalLifecycle("settled", 1);
  const first = reduce(
    createInitialPromptQueueState(),
    submit("prompt-1"),
    available
  );
  const later = reduce(first.state, submit("prompt-2"), available);
  const failed = reduce(
    later.state,
    commandResult(commandId(first.commands[0]), "queue/sendPrompt", "failed", {
      errorMessage: "boom"
    }),
    available
  );

  assert.equal(failed.commands[0]?.type, "queue/sendPrompt");
  assert.equal(send(failed.commands[0]).clientSubmitId, "prompt-2");
});

test("pre-turn failure removes a hidden auto submit before a later submit", () => {
  const available = canonicalLifecycle("settled", 1);
  const first = reduce(
    createInitialPromptQueueState(),
    submit("prompt-1"),
    available
  );
  const failed = reduce(
    first.state,
    commandResult(commandId(first.commands[0]), "queue/sendPrompt", "failed", {
      errorCode: "agent.process_cleanup_pending"
    }),
    available
  );

  assert.equal(failed.state.recordsBySessionId["session-1"], undefined);
  assert.equal(failed.commands[0]?.type, "session/reconcile");

  const second = reduce(failed.state, submit("prompt-2"), available);
  assert.equal(send(second.commands[0]).clientSubmitId, "prompt-2");
});

test("no-active-turn send failure reconciles before retrying queued prompt", () => {
  const available = canonicalLifecycle("settled", 1);
  const sending = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    available
  );
  const failed = reduce(
    sending.state,
    commandResult(
      commandId(sending.commands[0]),
      "queue/sendPrompt",
      "failed",
      { errorReason: "agent.no_active_turn" }
    ),
    available
  );
  assert.equal(failed.commands.length, 1);
  assert.equal(failed.commands[0]?.type, "session/reconcile");
  assert.equal(
    failed.state.recordsBySessionId["session-1"]?.failedPromptId,
    null
  );
  assert.equal(failed.state.recordsBySessionId["session-1"]?.inFlight, null);

  const reconciled = reduce(
    failed.state,
    turnUpserted(settledTurn("turn-2", 2)),
    canonicalLifecycle("settled", 2, "turn-2")
  );
  assert.equal(reconciled.commands[0]?.type, "queue/sendPrompt");
});

test("session no-active-turn app error reconciles before retrying queued prompt", () => {
  const available = canonicalLifecycle("settled", 1);
  const sending = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    available
  );
  const failed = reduce(
    sending.state,
    commandResult(
      commandId(sending.commands[0]),
      "queue/sendPrompt",
      "failed",
      { errorCode: "agent.session_no_active_turn" }
    ),
    available
  );

  assert.equal(failed.commands.length, 1);
  assert.equal(failed.commands[0]?.type, "session/reconcile");
  assert.equal(
    failed.state.recordsBySessionId["session-1"]?.failedPromptId,
    null
  );
  assert.equal(failed.state.recordsBySessionId["session-1"]?.inFlight, null);
});

test("process cleanup failure reconciles before retrying queued prompt", () => {
  const available = canonicalLifecycle("settled", 1);
  const sending = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    available
  );
  const failed = reduce(
    sending.state,
    commandResult(
      commandId(sending.commands[0]),
      "queue/sendPrompt",
      "failed",
      { errorCode: "agent.process_cleanup_pending" }
    ),
    available
  );

  assert.equal(failed.commands.length, 1);
  assert.equal(failed.commands[0]?.type, "session/reconcile");
  assert.equal(
    failed.state.recordsBySessionId["session-1"]?.failedPromptId,
    null
  );
  assert.equal(failed.state.recordsBySessionId["session-1"]?.inFlight, null);
});

test("timeout confirmation waits for its exact canonical turn to settle", () => {
  const available = canonicalLifecycle("settled", 1, "turn-0");
  const first = reduce(
    createInitialPromptQueueState(),
    submit("prompt-1"),
    available
  );
  let state = reduce(first.state, enqueue("prompt-2"), available).state;
  const timedOut = reduce(
    state,
    commandResult(commandId(first.commands[0]), "queue/sendPrompt", "timedOut"),
    available
  );
  assert.equal(timedOut.commands[0]?.type, "session/reconcile");
  assert.equal(
    timedOut.state.recordsBySessionId["session-1"]?.uncertainDelivery?.promptId,
    "prompt-1"
  );

  const confirmedBeforeTurn = reduce(
    timedOut.state,
    messagesReceived("prompt-1", "turn-1"),
    available
  );
  assert.deepEqual(confirmedBeforeTurn.commands, []);
  assert.equal(
    confirmedBeforeTurn.state.recordsBySessionId["session-1"]
      ?.deliveryBarrierTurnId,
    "turn-1"
  );
  assert.equal(
    confirmedBeforeTurn.state.recordsBySessionId["session-1"]
      ?.uncertainDelivery,
    null
  );

  const running = reduce(
    confirmedBeforeTurn.state,
    turnUpserted(runningTurn("turn-1", 2)),
    canonicalLifecycle("running", 2, "turn-1")
  );
  assert.deepEqual(running.commands, []);
  const settled = reduce(
    running.state,
    turnUpserted(settledTurn("turn-1", 3)),
    canonicalLifecycle("settled", 3, "turn-1")
  );
  assert.equal(send(settled.commands[0]).promptId, "prompt-2");
});

test("confirmation without exact turn id stays uncertain across expiry", () => {
  const available = canonicalLifecycle("settled", 1);
  const sending = reduce(
    createInitialPromptQueueState(),
    submit("prompt-1"),
    available
  );
  const timedOut = reduce(
    sending.state,
    commandResult(
      commandId(sending.commands[0]),
      "queue/sendPrompt",
      "timedOut"
    ),
    available
  );
  const uncorrelated = reduce(
    timedOut.state,
    messagesReceived("prompt-1", null),
    available
  );
  assert.equal(
    uncorrelated.state.recordsBySessionId["session-1"]?.uncertainDelivery
      ?.promptId,
    "prompt-1"
  );
  const expired = reduce(
    uncorrelated.state,
    {
      type: "engine/intentExpired",
      expiryId: "submit:prompt-1",
      dueAtUnixMs: 60_000
    },
    available
  );
  assert.equal(
    expired.state.recordsBySessionId["session-1"]?.uncertainDelivery?.promptId,
    "prompt-1"
  );
});

test("owned reconcile without turn proof drops timed-out send and drains later prompts", () => {
  const available = canonicalLifecycle("settled", 1);
  const first = reduce(
    createInitialPromptQueueState(),
    submit("prompt-1"),
    available
  );
  let state = reduce(first.state, enqueue("prompt-2"), available).state;
  const timedOut = reduce(
    state,
    commandResult(commandId(first.commands[0]), "queue/sendPrompt", "timedOut"),
    available
  );
  assert.equal(timedOut.commands[0]?.type, "session/reconcile");
  assert.equal(
    timedOut.state.recordsBySessionId["session-1"]?.uncertainDelivery?.promptId,
    "prompt-1"
  );

  const reconciled = reduce(
    timedOut.state,
    commandResult(
      commandId(timedOut.commands[0]),
      "session/reconcile",
      "succeeded"
    ),
    available
  );
  assert.equal(
    reconciled.state.recordsBySessionId["session-1"]?.uncertainDelivery,
    null
  );
  assert.deepEqual(
    reconciled.state.recordsBySessionId["session-1"]?.prompts.map(
      (prompt) => prompt.id
    ),
    ["prompt-2"]
  );
  assert.equal(send(reconciled.commands[0]).promptId, "prompt-2");
});

test("failed owned reconcile releases uncertainty into a retryable failed head", () => {
  const available = canonicalLifecycle("settled", 1);
  const sending = reduce(
    createInitialPromptQueueState(),
    submit("prompt-1"),
    available
  );
  const timedOut = reduce(
    sending.state,
    commandResult(
      commandId(sending.commands[0]),
      "queue/sendPrompt",
      "timedOut"
    ),
    available
  );
  const reconcileFailed = reduce(
    timedOut.state,
    commandResult(
      commandId(timedOut.commands[0]),
      "session/reconcile",
      "failed",
      { errorMessage: "reconcile unavailable" }
    ),
    available
  );
  assert.equal(
    reconcileFailed.state.recordsBySessionId["session-1"]?.uncertainDelivery,
    null
  );
  assert.equal(
    reconcileFailed.state.recordsBySessionId["session-1"]?.failedPromptId,
    "prompt-1"
  );
  assert.equal(
    reconcileFailed.state.recordsBySessionId["session-1"]?.failureMessage,
    "reconcile unavailable"
  );
  assert.deepEqual(reconcileFailed.commands, []);
});

test("session removal cleans queue-owned delivery state", () => {
  const lifecycle = canonicalLifecycle("running", 1);
  const queued = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    lifecycle
  );
  const removed = reduce(
    queued.state,
    { type: "session/removed", agentSessionId: "session-1" },
    createInitialSessionLifecycleState(),
    { deletedSessionIds: { "session-1": true } }
  );
  assert.equal(removed.state.recordsBySessionId["session-1"], undefined);
});

test("contract: plain queued head stays blocked behind a pending delivery barrier", () => {
  const available = canonicalLifecycle("settled", 1, "turn-0");
  const first = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    available
  );
  const state = reduce(first.state, enqueue("prompt-2"), available).state;
  const running = canonicalLifecycle("running", 2, "turn-1");
  const accepted = reduce(
    state,
    commandResult(
      commandId(first.commands[0]),
      "queue/sendPrompt",
      "succeeded"
    ),
    running,
    { sendValidation: validSend("turn-1", "running", 2) }
  );
  // The barrier from prompt-1's own delivery is still pending (turn-1 is
  // still running): a plain prompt-2 must stay queued behind it.
  assert.deepEqual(accepted.commands, []);
  assert.equal(
    accepted.state.recordsBySessionId["session-1"]?.deliveryBarrierTurnId,
    "turn-1"
  );
  assert.equal(
    accepted.state.recordsBySessionId["session-1"]?.prompts[0]?.id,
    "prompt-2"
  );
});

test("contract: send-now guidance drains through a pending delivery barrier into the active turn", () => {
  const available = canonicalLifecycle("settled", 1, "turn-0");
  const first = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    available
  );
  const state = reduce(first.state, enqueue("prompt-2"), available).state;
  const running = canonicalLifecycle("running", 2, "turn-1");
  const accepted = reduce(
    state,
    commandResult(
      commandId(first.commands[0]),
      "queue/sendPrompt",
      "succeeded"
    ),
    running,
    { sendValidation: validSend("turn-1", "running", 2) }
  );
  assert.equal(
    accepted.state.recordsBySessionId["session-1"]?.deliveryBarrierTurnId,
    "turn-1"
  );

  // prompt-2 is promoted to steer turn-1 while the barrier from prompt-1's
  // own delivery is still pending on that exact turn. Guidance is exempt
  // from the barrier it is steering, so it must drain immediately instead
  // of deadlocking behind its own target turn.
  const promoted = reduce(accepted.state, sendNow("prompt-2"), running);
  assert.equal(send(promoted.commands[0]).guidance, true);
  assert.equal(send(promoted.commands[0]).promptId, "prompt-2");
  assert.equal(
    promoted.state.recordsBySessionId["session-1"]?.inFlight?.guidance,
    true
  );
  assert.equal(
    promoted.state.recordsBySessionId["session-1"]?.deliveryBarrierTurnId,
    "turn-1"
  );
});

test("contract: a delivered guidance send preserves the barrier for the next plain prompt", () => {
  const available = canonicalLifecycle("settled", 1, "turn-0");
  const first = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    available
  );
  const state = reduce(first.state, enqueue("prompt-2"), available).state;
  const running = canonicalLifecycle("running", 2, "turn-1");
  const accepted = reduce(
    state,
    commandResult(
      commandId(first.commands[0]),
      "queue/sendPrompt",
      "succeeded"
    ),
    running,
    { sendValidation: validSend("turn-1", "running", 2) }
  );
  const promoted = reduce(accepted.state, sendNow("prompt-2"), running);
  assert.equal(send(promoted.commands[0]).guidance, true);

  // The guidance steer into turn-1 is delivered, but turn-1 keeps running.
  const delivered = reduce(
    promoted.state,
    commandResult(
      commandId(promoted.commands[0]),
      "queue/sendPrompt",
      "succeeded"
    ),
    running,
    { sendValidation: validSend("turn-1", "running", 3) }
  );
  assert.deepEqual(delivered.commands, []);
  assert.equal(
    delivered.state.recordsBySessionId["session-1"]?.deliveryBarrierTurnId,
    "turn-1"
  );

  // A plain prompt-3 queued after the guidance delivery must still stay
  // blocked behind the still-pending barrier: the guidance send did not
  // clear it.
  const withPlain = reduce(delivered.state, enqueue("prompt-3"), running);
  assert.deepEqual(withPlain.commands, []);
  assert.equal(
    withPlain.state.recordsBySessionId["session-1"]?.deliveryBarrierTurnId,
    "turn-1"
  );
  assert.equal(
    withPlain.state.recordsBySessionId["session-1"]?.prompts[0]?.id,
    "prompt-3"
  );

  // Once turn-1 actually settles, the barrier clears and prompt-3 drains as
  // a plain new-turn send.
  const settled = reduce(
    withPlain.state,
    turnUpserted(settledTurn("turn-1", 4)),
    canonicalLifecycle("settled", 4, "turn-1")
  );
  assert.equal(send(settled.commands[0]).promptId, "prompt-3");
  assert.equal(send(settled.commands[0]).guidance, undefined);
  assert.equal(
    settled.state.recordsBySessionId["session-1"]?.deliveryBarrierTurnId,
    null
  );
});

test("contract: successive guidance promotions each deliver into the same active turn", () => {
  const available = canonicalLifecycle("settled", 1, "turn-0");
  const first = reduce(
    createInitialPromptQueueState(),
    enqueue("prompt-1"),
    available
  );
  const state = reduce(first.state, enqueue("prompt-2"), available).state;
  const running = canonicalLifecycle("running", 2, "turn-1");
  const accepted = reduce(
    state,
    commandResult(
      commandId(first.commands[0]),
      "queue/sendPrompt",
      "succeeded"
    ),
    running,
    { sendValidation: validSend("turn-1", "running", 2) }
  );

  const firstGuidance = reduce(accepted.state, sendNow("prompt-2"), running);
  assert.equal(send(firstGuidance.commands[0]).guidance, true);
  assert.equal(send(firstGuidance.commands[0]).promptId, "prompt-2");

  const firstDelivered = reduce(
    firstGuidance.state,
    commandResult(
      commandId(firstGuidance.commands[0]),
      "queue/sendPrompt",
      "succeeded"
    ),
    running,
    { sendValidation: validSend("turn-1", "running", 3) }
  );
  assert.equal(
    firstDelivered.state.recordsBySessionId["session-1"]?.deliveryBarrierTurnId,
    "turn-1"
  );

  // A second prompt queued behind the still-pending barrier stays blocked as
  // a plain send...
  const withThird = reduce(firstDelivered.state, enqueue("prompt-3"), running);
  assert.deepEqual(withThird.commands, []);

  // ...but a second, successive guidance promotion also drains through the
  // very same persisting barrier into the very same active turn.
  const secondGuidance = reduce(withThird.state, sendNow("prompt-3"), running);
  assert.equal(send(secondGuidance.commands[0]).guidance, true);
  assert.equal(send(secondGuidance.commands[0]).promptId, "prompt-3");
  assert.equal(
    secondGuidance.state.recordsBySessionId["session-1"]?.deliveryBarrierTurnId,
    "turn-1"
  );
});

test("contract: guidance send-now stays blocked while availability is blocked by a pending interaction", () => {
  const lifecycle = canonicalLifecycle("settled", 2, "turn-1", true);
  const queued = reduce(
    createInitialPromptQueueState(),
    enqueueGuidance("prompt-1"),
    lifecycle
  );
  // Guidance is only exempt from the barrier while availability is blocked
  // on the active turn it steers; a pending interaction blocks for an
  // unrelated reason and guidance does not bypass it.
  assert.deepEqual(queued.commands, []);
  assert.equal(
    queued.state.recordsBySessionId["session-1"]?.prompts[0]?.id,
    "prompt-1"
  );
  assert.deepEqual(deriveCanonicalSubmitAvailability(lifecycle, "session-1"), {
    state: "blocked",
    reason: "waiting"
  });
});

function reduce(
  state: ReturnType<typeof createInitialPromptQueueState>,
  intent: Parameters<typeof promptQueueReducer>[1],
  lifecycle: SessionLifecycleState,
  options: {
    deletedSessionIds?: Readonly<Record<string, true>>;
    sendValidation?: SendInputResultValidation;
    strategy?: PromptQueueSendNowStrategy;
  } = {}
) {
  const availability = deriveCanonicalSubmitAvailability(
    lifecycle,
    "session-1"
  );
  const strategy =
    options.strategy ??
    (intent.type === "submit/requested" && intent.routing === "send_now"
      ? resolvePromptSendNowStrategy(availability, {
          activeTurnGuidance: true,
          interrupt: true
        })
      : intent.type === "queue/sendNowRequested"
        ? resolveQueuedPromptSendNowStrategy(
            state,
            intent.agentSessionId,
            intent.promptId,
            availability,
            { activeTurnGuidance: true, interrupt: true }
          )
        : null);
  return promptQueueReducer(state, intent, {
    deletedSessionIds: options.deletedSessionIds ?? {},
    lifecycle,
    sendNowStrategy: strategy,
    sendResultValidation: options.sendValidation,
    submitRequestAccepted: true
  });
}

function canonicalLifecycle(
  phase: "running" | "settled",
  updatedAtUnixMs: number,
  turnId = "turn-1",
  pendingInteraction = false
): SessionLifecycleState {
  return sessionLifecycleReducer(createInitialSessionLifecycleState(), {
    type: "session/snapshotReceived",
    sessions: [
      activitySession(phase, updatedAtUnixMs, turnId, pendingInteraction)
    ]
  }).state;
}

function activitySession(
  phase: "running" | "settled",
  updatedAtUnixMs: number,
  turnId: string,
  pendingInteraction = false
): AgentActivitySession {
  const turn =
    phase === "running"
      ? runningTurn(turnId, updatedAtUnixMs)
      : settledTurn(turnId, updatedAtUnixMs);
  const interaction: AgentActivityInteraction = {
    agentSessionId: "session-1",
    createdAtUnixMs: updatedAtUnixMs,
    input: {},
    kind: "question",
    metadata: {},
    requestId: "request-1",
    status: "pending",
    turnId,
    updatedAtUnixMs
  };
  return normalizeAgentActivitySession({
    activeTurn: phase === "running" ? turn : null,
    activeTurnId: phase === "running" ? turnId : null,
    agentSessionId: "session-1",
    cwd: "/workspace",
    latestTurn: turn,
    latestTurnInteractions: pendingInteraction ? [interaction] : [],
    pendingInteractions: pendingInteraction ? [interaction] : [],
    provider: "codex",
    title: "Session",
    updatedAtUnixMs,
    workspaceId: "workspace-1"
  });
}

function validSend(
  turnId: string,
  phase: "running" | "settled",
  updatedAtUnixMs: number
): SendInputResultValidation {
  const session = activitySession(phase, updatedAtUnixMs, turnId);
  return {
    kind: "valid",
    result: { session, turn: session.latestTurn!, turnId }
  };
}

function runningTurn(
  turnId: string,
  updatedAtUnixMs: number
): AgentActivityTurn {
  return {
    agentSessionId: "session-1",
    origin: "user_prompt",
    phase: "running",
    startedAtUnixMs: updatedAtUnixMs,
    turnId,
    updatedAtUnixMs
  };
}

function settledTurn(
  turnId: string,
  updatedAtUnixMs: number,
  outcome: "completed" | "canceled" = "completed"
): AgentActivityTurn {
  return {
    agentSessionId: "session-1",
    origin: "user_prompt",
    outcome,
    phase: "settled",
    settledAtUnixMs: updatedAtUnixMs,
    startedAtUnixMs: Math.max(0, updatedAtUnixMs - 1),
    turnId,
    updatedAtUnixMs
  };
}

function turnUpserted(turn: AgentActivityTurn) {
  return { live: true, type: "turn/upserted" as const, turn };
}

function messagesReceived(clientSubmitId: string, turnId: string | null) {
  return {
    type: "message/snapshotReceived" as const,
    messages: [
      {
        agentSessionId: "session-1",
        kind: "text",
        messageId: "message-1",
        occurredAtUnixMs: 4,
        payload: { clientSubmitId, text: clientSubmitId },
        role: "user",
        sequence: 1,
        turnId,
        version: 1
      }
    ]
  };
}

function sendNow(promptId: string) {
  return {
    type: "queue/sendNowRequested" as const,
    agentSessionId: "session-1",
    awaitingTurnExpiresAtUnixMs: 30_000,
    cancelCommandId: "cancel-1",
    promptId,
    timeoutMs: 30_000
  };
}

function enqueue(promptId: string) {
  return {
    type: "queue/enqueued" as const,
    agentSessionId: "session-1",
    prompt: {
      id: promptId,
      content: [{ type: "text" as const, text: promptId }],
      createdAtUnixMs: 1,
      submitDiagnostics
    },
    workspaceId: "workspace-1"
  };
}

function enqueueGuidance(promptId: string) {
  return {
    type: "queue/enqueued" as const,
    agentSessionId: "session-1",
    prompt: {
      id: promptId,
      content: [{ type: "text" as const, text: promptId }],
      createdAtUnixMs: 1,
      guidance: true,
      submitDiagnostics
    },
    workspaceId: "workspace-1"
  };
}

function submit(clientSubmitId: string) {
  return {
    type: "submit/requested" as const,
    agentSessionId: "session-1",
    clientSubmitId,
    content: [{ type: "text" as const, text: clientSubmitId }],
    expiresAtUnixMs: 60_000,
    requestedAtUnixMs: 1,
    submitDiagnostics,
    workspaceId: "workspace-1"
  };
}

const submitDiagnostics = {
  blockCount: 1,
  hasImage: false,
  promptLength: 8,
  queued: false,
  source: "agent-gui",
  submittedAtUnixMs: 1
} as const;

function commandResult(
  commandId: string,
  commandType: EngineCommandResultIntent["commandType"],
  outcome: EngineCommandResultIntent["outcome"],
  options: Pick<
    EngineCommandResultIntent,
    "errorCode" | "errorMessage" | "errorReason"
  > = {}
): EngineCommandResultIntent {
  return {
    type: "engine/commandResult",
    commandId,
    commandType,
    outcome,
    ...options
  };
}

function commandId(command: EngineCommand | undefined): string {
  assert.ok(command && "commandId" in command && command.commandId);
  return command.commandId;
}

function send(
  command: EngineCommand | undefined
): Extract<EngineCommand, { type: "queue/sendPrompt" }> {
  assert.equal(command?.type, "queue/sendPrompt");
  return command as Extract<EngineCommand, { type: "queue/sendPrompt" }>;
}
