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

test("semantic prompt submission owns scope, confirmation window, and send projection", () => {
  const harness = createHarness(false);

  const result = harness.engine.submitPrompt({
    agentSessionId: " session-1 ",
    capabilityRefs: [{ capability: "tutti", source: "slash_command" }],
    clientSubmitId: " submit-1 ",
    content: [{ text: "display text", type: "text" }],
    displayPrompt: " Display text ",
    runtimeContent: [{ text: "runtime text", type: "text" }],
    submitDiagnostics: { source: "test" }
  });

  assert.deepEqual(result, { accepted: true, queued: false });
  assert.deepEqual(harness.observedIntents.at(-1), {
    agentSessionId: "session-1",
    capabilityRefs: [{ capability: "tutti", source: "slash_command" }],
    clientSubmitId: "submit-1",
    content: [{ text: "display text", type: "text" }],
    displayPrompt: "Display text",
    expiresAtUnixMs: 120_100,
    requestedAtUnixMs: 100,
    routing: "auto",
    runtimeContent: [{ text: "runtime text", type: "text" }],
    submitDiagnostics: { queued: false, source: "test" },
    type: "submit/requested",
    workspaceId: "workspace-1"
  });
  assert.deepEqual(harness.commands, [
    {
      agentSessionId: "session-1",
      capabilityRefs: [{ capability: "tutti", source: "slash_command" }],
      clientSubmitId: "submit-1",
      commandId: "queue:send:session-1:1",
      content: [{ text: "runtime text", type: "text" }],
      correlationId: "submit-1",
      displayPrompt: "Display text",
      promptId: "submit-1",
      submitDiagnostics: {
        blockCount: 1,
        queued: false,
        source: "test",
        submittedAtUnixMs: 100
      },
      timeoutMs: 90_000,
      type: "queue/sendPrompt",
      workspaceId: "workspace-1"
    }
  ]);
  assert.deepEqual(
    harness.scheduled.map((task) => task.delayMs),
    [120_000, 90_000]
  );
});

test("semantic prompt submission reports visible queue admission for a busy Session", () => {
  const harness = createHarness(true);

  const result = harness.engine.submitPrompt({
    agentSessionId: "session-1",
    clientSubmitId: "submit-1",
    content: [{ text: "continue", type: "text" }],
    submitDiagnostics: { source: "test" }
  });

  assert.deepEqual(result, { accepted: true, queued: true });
  const observed = harness.observedIntents.at(-1);
  assert.equal(observed?.type, "submit/requested");
  assert.equal(
    observed && observed.type === "submit/requested"
      ? observed.submitDiagnostics?.queued
      : undefined,
    true
  );
  assert.deepEqual(harness.commands, []);
  assert.deepEqual(
    harness.scheduled.map((task) => task.delayMs),
    [120_000]
  );
});

test("send-now retains the prompt until authoritative guidance capabilities arrive", () => {
  const harness = createHarness(true);

  const result = harness.engine.submitPrompt({
    agentSessionId: "session-1",
    clientSubmitId: "submit-guidance",
    content: [{ text: "steer when ready", type: "text" }],
    routing: "send_now"
  });

  assert.deepEqual(result, { accepted: true, queued: true });
  assert.equal(harness.commands.length, 0);

  const capabilitySnapshot = {
    sessions: [
      sessionWithCapabilities(
        activeTurn(),
        capabilities({ activeTurnGuidance: true })
      )
    ],
    type: "session/snapshotReceived" as const
  };
  harness.engine.dispatch(capabilitySnapshot);
  harness.engine.dispatch(capabilitySnapshot);

  assert.deepEqual(harness.commands, [
    {
      agentSessionId: "session-1",
      clientSubmitId: "submit-guidance",
      commandId: "queue:send:session-1:1",
      content: [{ text: "steer when ready", type: "text" }],
      correlationId: "submit-guidance",
      guidance: true,
      promptId: "submit-guidance",
      submitDiagnostics: {
        blockCount: 1,
        queued: true,
        submittedAtUnixMs: 100
      },
      targetTurnId: "turn-1",
      timeoutMs: 90_000,
      type: "queue/sendPrompt",
      workspaceId: "workspace-1"
    }
  ]);
});

test("send-now retains the prompt until authoritative interrupt capability arrives", () => {
  const harness = createHarness(true);

  const result = harness.engine.submitPrompt({
    agentSessionId: "session-1",
    clientSubmitId: "submit-interrupt",
    content: [{ text: "interrupt when ready", type: "text" }],
    routing: "send_now"
  });

  assert.deepEqual(result, { accepted: true, queued: true });
  assert.equal(harness.commands.length, 0);

  harness.engine.dispatch({
    sessions: [
      sessionWithCapabilities(activeTurn(), capabilities({ interrupt: true }))
    ],
    type: "session/snapshotReceived"
  });

  assert.deepEqual(harness.commands, []);
  assert.equal(
    harness.engine.getSnapshot().promptQueue.recordsBySessionId["session-1"]
      ?.sendNextPromptId,
    "submit-interrupt"
  );
});

test("send-now stays queued when authoritative capabilities do not support active-turn delivery", () => {
  const harness = createHarness(true);

  const result = harness.engine.submitPrompt({
    agentSessionId: "session-1",
    clientSubmitId: "submit-queued",
    content: [{ text: "send after the turn", type: "text" }],
    routing: "send_now"
  });

  assert.deepEqual(result, { accepted: true, queued: true });
  harness.engine.dispatch({
    sessions: [sessionWithCapabilities(activeTurn(), capabilities({}))],
    type: "session/snapshotReceived"
  });
  assert.equal(harness.commands.length, 0);
  assert.equal(
    harness.engine.getSnapshot().promptQueue.recordsBySessionId["session-1"]
      ?.prompts[0]?.clientSubmitId,
    "submit-queued"
  );

  harness.engine.dispatch({
    sessions: [
      sessionWithCapabilities(
        {
          ...activeTurn(),
          outcome: "completed",
          phase: "settled",
          settledAtUnixMs: 3,
          updatedAtUnixMs: 3
        },
        capabilities({})
      )
    ],
    type: "session/snapshotReceived"
  });

  const send = harness.commands.at(-1);
  assert.equal(send?.type, "queue/sendPrompt");
  assert.equal(
    send?.type === "queue/sendPrompt" ? send.clientSubmitId : null,
    "submit-queued"
  );
  assert.equal(
    send?.type === "queue/sendPrompt" ? send.guidance : undefined,
    undefined
  );
});

test("deferred send-now survives a temporary interaction blocker for the same turn", () => {
  const harness = createHarness(true);
  harness.engine.submitPrompt({
    agentSessionId: "session-1",
    clientSubmitId: "submit-after-approval",
    content: [{ text: "steer after approval", type: "text" }],
    routing: "send_now"
  });
  const pendingInteraction: AgentActivityInteraction = {
    agentSessionId: "session-1",
    createdAtUnixMs: 3,
    kind: "approval",
    requestId: "approval-1",
    status: "pending",
    turnId: "turn-1",
    updatedAtUnixMs: 3
  };
  const blockedSession = {
    ...sessionWithCapabilities(
      activeTurn(),
      capabilities({ activeTurnGuidance: true })
    ),
    latestTurnInteractions: [pendingInteraction],
    pendingInteractions: [pendingInteraction]
  };

  harness.engine.dispatch({
    sessions: [blockedSession],
    type: "session/snapshotReceived"
  });

  assert.equal(harness.commands.length, 0);
  assert.ok(
    harness.engine.getSnapshot().promptQueue.recordsBySessionId["session-1"]
      ?.pendingSendNowByPromptId?.["submit-after-approval"]
  );

  harness.engine.dispatch({
    interaction: {
      ...pendingInteraction,
      status: "answered",
      updatedAtUnixMs: 4
    },
    type: "interaction/upserted"
  });

  const send = harness.commands.at(-1);
  assert.equal(send?.type, "queue/sendPrompt");
  assert.equal(
    send?.type === "queue/sendPrompt" ? send.clientSubmitId : null,
    "submit-after-approval"
  );
  assert.equal(
    send?.type === "queue/sendPrompt" ? send.targetTurnId : null,
    "turn-1"
  );
});

test("unsupported deferred send-now restores ordinary FIFO order", () => {
  const harness = createHarness(true);
  harness.engine.submitPrompt({
    agentSessionId: "session-1",
    clientSubmitId: "ordinary-first",
    content: [{ text: "first", type: "text" }]
  });
  harness.engine.submitPrompt({
    agentSessionId: "session-1",
    clientSubmitId: "send-now-second",
    content: [{ text: "second", type: "text" }],
    routing: "send_now"
  });

  harness.engine.dispatch({
    sessions: [sessionWithCapabilities(activeTurn(), capabilities({}))],
    type: "session/snapshotReceived"
  });

  const queued =
    harness.engine.getSnapshot().promptQueue.recordsBySessionId["session-1"];
  assert.deepEqual(
    queued?.prompts.map((prompt) => prompt.id),
    ["ordinary-first", "send-now-second"]
  );
  assert.deepEqual(queued?.pendingSendNowByPromptId, undefined);
});

test("send-now drains as a plain prompt when the active turn settles before capabilities arrive", () => {
  const harness = createHarness(true);

  const result = harness.engine.submitPrompt({
    agentSessionId: "session-1",
    clientSubmitId: "submit-after-settle",
    content: [{ text: "send after settlement", type: "text" }],
    routing: "send_now"
  });

  assert.deepEqual(result, { accepted: true, queued: true });
  assert.equal(harness.commands.length, 0);

  const settledTurn: AgentActivityTurn = {
    ...activeTurn(),
    outcome: "completed",
    phase: "settled",
    settledAtUnixMs: 3,
    updatedAtUnixMs: 3
  };
  harness.engine.dispatch({
    sessions: [
      {
        ...session(null),
        latestTurn: settledTurn,
        updatedAtUnixMs: settledTurn.updatedAtUnixMs
      }
    ],
    type: "session/snapshotReceived"
  });

  assert.equal(harness.commands.length, 1);
  const send = harness.commands[0];
  assert.equal(send?.type, "queue/sendPrompt");
  assert.equal(
    send?.type === "queue/sendPrompt" ? send.clientSubmitId : null,
    "submit-after-settle"
  );
  assert.equal(
    send?.type === "queue/sendPrompt" ? send.guidance : undefined,
    undefined
  );
});

test("deferred send-now never targets a successor active turn", () => {
  const harness = createHarness(true);
  harness.engine.submitPrompt({
    agentSessionId: "session-1",
    clientSubmitId: "submit-for-turn-a",
    content: [{ text: "only steer turn A", type: "text" }],
    routing: "send_now"
  });

  const turnB: AgentActivityTurn = {
    ...activeTurn(),
    startedAtUnixMs: 3,
    turnId: "turn-2",
    updatedAtUnixMs: 4
  };
  harness.engine.dispatch({
    sessions: [
      sessionWithCapabilities(
        turnB,
        capabilities({ activeTurnGuidance: true, interrupt: true })
      )
    ],
    type: "session/snapshotReceived"
  });

  assert.equal(harness.commands.length, 0);
  const waiting =
    harness.engine.getSnapshot().promptQueue.recordsBySessionId["session-1"];
  assert.deepEqual(waiting?.pendingSendNowByPromptId, undefined);
  assert.equal(waiting?.prompts[0]?.guidance, undefined);

  harness.engine.dispatch({
    sessions: [
      sessionWithCapabilities(
        {
          ...turnB,
          outcome: "completed",
          phase: "settled",
          settledAtUnixMs: 5,
          updatedAtUnixMs: 5
        },
        capabilities({ activeTurnGuidance: true, interrupt: true })
      )
    ],
    type: "session/snapshotReceived"
  });

  assert.equal(harness.commands.length, 1);
  const send = harness.commands[0];
  assert.equal(send?.type, "queue/sendPrompt");
  assert.equal(
    send?.type === "queue/sendPrompt" ? send.clientSubmitId : null,
    "submit-for-turn-a"
  );
  assert.equal(
    send?.type === "queue/sendPrompt" ? send.guidance : undefined,
    undefined
  );
});

test("consecutive deferred send-now prompts retain independent decisions", () => {
  const scenarios = [
    {
      capabilities: capabilities({ activeTurnGuidance: true }),
      commandType: "queue/sendPrompt",
      name: "guidance"
    },
    {
      capabilities: capabilities({ interrupt: true }),
      commandType: null,
      name: "interrupt"
    },
    {
      capabilities: capabilities({}),
      commandType: null,
      name: "unsupported"
    }
  ] as const;

  for (const scenario of scenarios) {
    const harness = createHarness(true);
    for (const id of ["first", "second"] as const) {
      const result = harness.engine.submitPrompt({
        agentSessionId: "session-1",
        clientSubmitId: `${scenario.name}-${id}`,
        content: [{ text: `${scenario.name} ${id}`, type: "text" }],
        routing: "send_now"
      });
      assert.deepEqual(result, { accepted: true, queued: true });
    }

    const before =
      harness.engine.getSnapshot().promptQueue.recordsBySessionId["session-1"];
    assert.deepEqual(
      Object.keys(before?.pendingSendNowByPromptId ?? {}).sort(),
      [`${scenario.name}-first`, `${scenario.name}-second`]
    );
    assert.deepEqual(
      before?.prompts.map((prompt) => prompt.id),
      [`${scenario.name}-first`, `${scenario.name}-second`]
    );

    harness.engine.dispatch({
      sessions: [sessionWithCapabilities(activeTurn(), scenario.capabilities)],
      type: "session/snapshotReceived"
    });

    assert.equal(harness.commands[0]?.type ?? null, scenario.commandType);
    const after =
      harness.engine.getSnapshot().promptQueue.recordsBySessionId["session-1"];
    assert.deepEqual(Object.keys(after?.pendingSendNowByPromptId ?? {}), [
      `${scenario.name}-second`
    ]);
    const command = harness.commands[0];
    if (command?.type === "queue/sendPrompt") {
      assert.equal(command.clientSubmitId, `${scenario.name}-first`);
    }
  }
});

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
