import assert from "node:assert/strict";
import { test } from "node:test";
import { createAgentSessionEngine } from "./createAgentSessionEngine.ts";
import type { EngineDiagnosticEvent } from "./diagnostics.ts";
import { createTestEngineCommandPort } from "./testEngineCommandPort.ts";
import type {
  AgentSessionEngine,
  AgentSessionEngineState,
  EngineClock,
  EngineExternalCommand,
  EngineIntent,
  EngineScheduler,
  EngineTypedCommandPort
} from "./types.ts";
import type { AgentActivityTurn } from "../types.ts";
import type { AgentActivitySessionInput } from "../sessionNormalization.ts";

// Event-interleaving tests over synthetic entities: the engine loop is driven
// by a manual clock/scheduler so every timing case is an explicit, enumerable
// transition instead of real-timer flakiness.

interface ManualTimer {
  advance(ms: number): void;
  clock: EngineClock;
  pendingTaskCount(): number;
  scheduler: EngineScheduler;
}

function createManualTimer(): ManualTimer {
  let now = 0;
  let nextSequence = 0;
  const tasks: { at: number; run: () => void; sequence: number }[] = [];
  return {
    advance(ms) {
      now += ms;
      for (;;) {
        const dueIndex = tasks.findIndex((task) => task.at <= now);
        if (dueIndex === -1) {
          return;
        }
        let earliestIndex = dueIndex;
        for (let index = dueIndex + 1; index < tasks.length; index += 1) {
          const task = tasks[index];
          const earliest = tasks[earliestIndex];
          if (
            task !== undefined &&
            earliest !== undefined &&
            task.at <= now &&
            (task.at < earliest.at ||
              (task.at === earliest.at && task.sequence < earliest.sequence))
          ) {
            earliestIndex = index;
          }
        }
        const [dueTask] = tasks.splice(earliestIndex, 1);
        dueTask?.run();
      }
    },
    clock: {
      nowUnixMs: () => now
    },
    pendingTaskCount() {
      return tasks.length;
    },
    scheduler: {
      schedule(delayMs, run) {
        const entry = { at: now + delayMs, run, sequence: nextSequence };
        nextSequence += 1;
        tasks.push(entry);
        return {
          cancel() {
            const index = tasks.indexOf(entry);
            if (index !== -1) {
              tasks.splice(index, 1);
            }
          }
        };
      }
    }
  };
}

interface ManualCommandPort extends EngineTypedCommandPort {
  abortSignalsByCommandId: Map<string, AbortSignal>;
  executedCommands: EngineExternalCommand[];
  fail(commandId: string, error: unknown): void;
  succeed(commandId: string, value?: unknown): void;
}

function createManualCommandPort(): ManualCommandPort {
  const settlersByCommandId = new Map<
    string,
    { reject: (error: unknown) => void; resolve: (value: unknown) => void }
  >();
  const executedCommands: EngineExternalCommand[] = [];
  const abortSignalsByCommandId = new Map<string, AbortSignal>();
  const commandPort = createTestEngineCommandPort((command, options) => {
    executedCommands.push(command);
    if (options?.signal) {
      abortSignalsByCommandId.set(command.commandId, options.signal);
    }
    return new Promise((resolve, reject) => {
      settlersByCommandId.set(command.commandId, { reject, resolve });
    });
  });
  return Object.assign(commandPort, {
    abortSignalsByCommandId,
    executedCommands,
    fail(commandId: string, error: unknown) {
      settlersByCommandId.get(commandId)?.reject(error);
      settlersByCommandId.delete(commandId);
    },
    succeed(commandId: string, value?: unknown) {
      settlersByCommandId.get(commandId)?.resolve(value);
      settlersByCommandId.delete(commandId);
    }
  });
}

function createHarness(input?: {
  intentObserver?: (intent: EngineIntent) => void;
  origin?: string;
  workspaceId?: string;
}) {
  const timer = createManualTimer();
  const commandPort = createManualCommandPort();
  const diagnosticEvents: EngineDiagnosticEvent[] = [];
  const notifiedStates: AgentSessionEngineState[] = [];
  const engine = createAgentSessionEngine({
    clock: timer.clock,
    commandPort,
    diagnosticSink: (event) => {
      diagnosticEvents.push(event);
    },
    identity: {
      origin: input?.origin ?? "local-tuttid",
      workspaceId: input?.workspaceId ?? "ws-1"
    },
    ...(input?.intentObserver ? { intentObserver: input.intentObserver } : {}),
    scheduler: timer.scheduler
  });
  engine.subscribe((state) => {
    notifiedStates.push(state);
  });
  return { commandPort, diagnosticEvents, engine, notifiedStates, timer };
}

async function flushMicrotasks(): Promise<void> {
  await new Promise((resolve) => {
    setImmediate(resolve);
  });
}

test("identity is a frozen workspace + origin pair", () => {
  const { engine } = createHarness({ origin: "shared-room", workspaceId: "w" });
  assert.deepEqual(engine.identity, {
    origin: "shared-room",
    workspaceId: "w"
  });
  assert.ok(Object.isFrozen(engine.identity));
});

test("factory rejects empty identity parts", () => {
  const timer = createManualTimer();
  assert.throws(() =>
    createAgentSessionEngine({
      clock: timer.clock,
      commandPort: createManualCommandPort(),
      identity: { origin: "local", workspaceId: "  " },
      scheduler: timer.scheduler
    })
  );
  assert.throws(() =>
    createAgentSessionEngine({
      clock: timer.clock,
      commandPort: createManualCommandPort(),
      identity: { origin: "", workspaceId: "ws" },
      scheduler: timer.scheduler
    })
  );
});

test("identity filtering preserves wrong-workspace activation evidence", () => {
  const { engine } = createHarness({ workspaceId: "workspace-1" });
  assert.equal(
    engine.activateSession({
      agentSessionId: "session-created",
      agentTargetId: "target-1",
      clientSubmitId: "submit-created",
      mode: "new",
      requestId: "activation-created"
    }),
    true
  );

  engine.dispatch({
    sessions: [
      activitySession("session-created", { workspaceId: "workspace-other" })
    ],
    type: "session/snapshotReceived"
  });

  const activation =
    engine.getSnapshot().pendingIntents.activationsByRequestId[
      "activation-created"
    ];
  assert.equal(activation?.snapshotOutcome, "workspace_mismatch");
  assert.equal(activation?.snapshotObservedAtUnixMs, 0);
});

test("engine drops intents scoped to another workspace", () => {
  const harness = createHarness({ workspaceId: "workspace-a" });
  harness.engine.dispatch({
    agentSessionId: "session-1",
    prompt: {
      content: [{ type: "text", text: "wrong workspace" }],
      createdAtUnixMs: 1,
      id: "prompt-1"
    },
    type: "queue/enqueued",
    workspaceId: "workspace-b"
  });
  assert.equal(
    harness.engine.getSnapshot().promptQueue.recordsBySessionId["session-1"],
    undefined
  );
  assert.deepEqual(harness.diagnosticEvents, [
    {
      intentType: "queue/enqueued",
      type: "intentDroppedForIdentityMismatch"
    }
  ]);
});

test("immediate dispatch reduces synchronously and notifies once per drain", () => {
  const { engine, notifiedStates } = createHarness();
  engine.dispatch({ status: "connected", type: "engine/connectionChanged" });
  assert.equal(engine.getSnapshot().engineRuntime.connection, "connected");
  assert.equal(engine.getSnapshot().engineRuntime.processedIntentCount, 1);
  assert.equal(notifiedStates.length, 1);
});

test("intent observer sees scoped dispatches including command results", async () => {
  const observed: EngineIntent[] = [];
  const harness = createHarness({
    intentObserver: (intent) => observed.push(intent)
  });
  harness.engine.dispatch({
    probeId: "p-observed",
    type: "engine/probeRequested"
  });
  harness.commandPort.succeed("p-observed", { ok: true });
  await flushMicrotasks();
  assert.deepEqual(
    observed.map((intent) => intent.type),
    ["engine/probeRequested", "engine/commandResult"]
  );
});

test("throwing intent observer is diagnostic-only", () => {
  const observerError = new Error("observer exploded");
  const harness = createHarness({
    intentObserver: () => {
      throw observerError;
    }
  });
  harness.engine.dispatch({
    status: "connected",
    type: "engine/connectionChanged"
  });
  assert.equal(
    harness.engine.getSnapshot().engineRuntime.connection,
    "connected"
  );
  assert.deepEqual(harness.diagnosticEvents, [
    {
      error: observerError,
      intentType: "engine/connectionChanged",
      type: "intentObserverError"
    }
  ]);
});

test("batched intents coalesce into one frame and one notification", () => {
  const { engine, notifiedStates, timer } = createHarness();
  engine.dispatch(
    { status: "connected", type: "engine/connectionChanged" },
    { batch: true }
  );
  engine.dispatch(
    { status: "disconnected", type: "engine/connectionChanged" },
    { batch: true }
  );
  engine.dispatch(
    { status: "connected", type: "engine/connectionChanged" },
    { batch: true }
  );
  assert.equal(engine.getSnapshot().engineRuntime.processedIntentCount, 0);
  assert.equal(notifiedStates.length, 0);

  timer.advance(33);
  assert.equal(engine.getSnapshot().engineRuntime.processedIntentCount, 3);
  assert.equal(engine.getSnapshot().engineRuntime.connection, "connected");
  assert.equal(notifiedStates.length, 1);
});

test("a non-batched dispatch flushes the pending frame first, in order", () => {
  const { engine, timer } = createHarness();
  engine.dispatch(
    { status: "connected", type: "engine/connectionChanged" },
    { batch: true }
  );
  engine.dispatch({
    status: "disconnected",
    type: "engine/connectionChanged"
  });
  // Batched intent reduced first, urgent intent last: final state is the
  // urgent one and both were processed in the same drain.
  assert.equal(engine.getSnapshot().engineRuntime.processedIntentCount, 2);
  assert.equal(engine.getSnapshot().engineRuntime.connection, "disconnected");
  // The frame timer was canceled; advancing time must not replay the batch.
  timer.advance(100);
  assert.equal(engine.getSnapshot().engineRuntime.processedIntentCount, 2);
});

test("command success feeds back into the loop as a result intent", async () => {
  const { commandPort, engine } = createHarness();
  engine.dispatch({ probeId: "p-1", type: "engine/probeRequested" });
  assert.equal(commandPort.executedCommands.length, 1);
  assert.deepEqual(commandPort.executedCommands[0], {
    commandId: "p-1",
    type: "engine/probe"
  });

  commandPort.succeed("p-1", { ok: true });
  await flushMicrotasks();
  assert.deepEqual(engine.getSnapshot().engineRuntime.lastCommandResult, {
    commandId: "p-1",
    outcome: "succeeded"
  });
});

test("command execution observes the state produced by its triggering intent", () => {
  const timer = createManualTimer();
  let engine: AgentSessionEngine;
  let snapshotDuringExecution: AgentSessionEngineState | undefined;
  const commandPort = createTestEngineCommandPort(async () => {
    snapshotDuringExecution = engine.getSnapshot();
    return new Promise(() => {});
  });
  engine = createAgentSessionEngine({
    clock: timer.clock,
    commandPort,
    identity: { origin: "local-tuttid", workspaceId: "ws-1" },
    scheduler: timer.scheduler
  });

  engine.dispatch({ probeId: "snapshot-order", type: "engine/probeRequested" });

  assert.equal(snapshotDuringExecution?.engineRuntime.processedIntentCount, 1);
});

test("command failure feeds back as a failed result with the error message", async () => {
  const { commandPort, engine } = createHarness();
  engine.dispatch({ probeId: "p-2", type: "engine/probeRequested" });
  commandPort.fail("p-2", new Error("transport down"));
  await flushMicrotasks();
  assert.deepEqual(engine.getSnapshot().engineRuntime.lastCommandResult, {
    commandId: "p-2",
    errorMessage: "transport down",
    outcome: "failed"
  });
});



test("activation settings reach the host and their result updates the session", async () => {
  const harness = createHarness({ workspaceId: "workspace-1" });
  harness.engine.dispatch({
    agentSessionId: "session-new",
    agentTargetId: "target-1",
    clientSubmitId: "submit-create",
    content: [{ text: "create", type: "text" }],
    cwd: "/workspace",
    expiresAtUnixMs: 60_000,
    mode: "new",
    requestedAtUnixMs: 1,
    requestId: "activation-1",
    type: "activation/requested",
    workspaceId: "workspace-1"
  });
  harness.engine.dispatch({
    agentSessionId: "session-new",
    settings: { model: "model-2" },
    type: "activation/settingsPatched"
  });
  harness.engine.dispatch({
    sessions: [
      activitySession("session-new", {
        agentTargetId: "target-1",
        createdAtUnixMs: 2,
        settings: { model: "model-1" }
      })
    ],
    type: "session/snapshotReceived"
  });

  const activationSettings = harness.commandPort.executedCommands.at(-1);
  assert.deepEqual(activationSettings, {
    agentSessionId: "session-new",
    commandId: "activation-settings:activation-1",
    correlationId: "session-new",
    settings: { model: "model-2" },
    type: "session/updateSettings",
    workspaceId: "workspace-1"
  });

  harness.commandPort.succeed(activationSettings!.commandId, {
    agentSessionId: "session-new",
    session: activitySession("session-new", {
      agentTargetId: "target-1",
      createdAtUnixMs: 2,
      settings: { model: "model-2" },
      updatedAtUnixMs: 3
    })
  });
  await flushMicrotasks();

  assert.equal(
    harness.engine.getSnapshot().sessionLifecycle.sessionsById["session-new"]
      ?.settings?.model,
    "model-2"
  );
});


test("command timeout settles as timedOut and a late result is ignored", async () => {
  const { commandPort, diagnosticEvents, engine, timer } = createHarness();
  engine.dispatch({
    probeId: "p-3",
    timeoutMs: 200,
    type: "engine/probeRequested"
  });
  timer.advance(200);
  assert.equal(commandPort.abortSignalsByCommandId.get("p-3")?.aborted, true);
  assert.deepEqual(engine.getSnapshot().engineRuntime.lastCommandResult, {
    commandId: "p-3",
    outcome: "timedOut"
  });

  commandPort.succeed("p-3", { late: true });
  await flushMicrotasks();
  // The late settlement is dropped with a diagnostic, not applied to state.
  assert.deepEqual(engine.getSnapshot().engineRuntime.lastCommandResult, {
    commandId: "p-3",
    outcome: "timedOut"
  });
  assert.deepEqual(diagnosticEvents, [
    { commandId: "p-3", type: "commandResultAfterTimeout" }
  ]);
});

test("expiry request round-trips through the host clock as an expiry intent", () => {
  const { engine, timer } = createHarness();
  engine.dispatch({
    dueAtUnixMs: 150,
    expiryId: "e-1",
    type: "engine/expiryRequested"
  });
  assert.equal(engine.getSnapshot().engineRuntime.lastExpiredIntentId, null);

  timer.advance(149);
  assert.equal(engine.getSnapshot().engineRuntime.lastExpiredIntentId, null);
  timer.advance(1);
  assert.equal(engine.getSnapshot().engineRuntime.lastExpiredIntentId, "e-1");
});

test("a canceled expiry never fires", () => {
  const { engine, timer } = createHarness();
  engine.dispatch({
    dueAtUnixMs: 100,
    expiryId: "e-2",
    type: "engine/expiryRequested"
  });
  engine.dispatch({ expiryId: "e-2", type: "engine/expiryCancelRequested" });
  timer.advance(1000);
  assert.equal(engine.getSnapshot().engineRuntime.lastExpiredIntentId, null);
});

test("rescheduling an expiry id replaces the previous deadline", () => {
  const { engine, timer } = createHarness();
  engine.dispatch({
    dueAtUnixMs: 100,
    expiryId: "e-3",
    type: "engine/expiryRequested"
  });
  engine.dispatch({
    dueAtUnixMs: 300,
    expiryId: "e-3",
    type: "engine/expiryRequested"
  });
  timer.advance(100);
  assert.equal(engine.getSnapshot().engineRuntime.lastExpiredIntentId, null);
  timer.advance(200);
  assert.equal(engine.getSnapshot().engineRuntime.lastExpiredIntentId, "e-3");
});

test("stop aborts activation immediately", () => {
  const { commandPort, engine } = createHarness();
  engine.dispatch({
    agentSessionId: "session-1",
    agentTargetId: "cursor",
    clientSubmitId: "submit-1",
    content: [{ type: "text", text: "hello" }],
    cwd: "/workspace",
    expiresAtUnixMs: 60_000,
    mode: "new",
    requestedAtUnixMs: 1,
    requestId: "activation-1",
    type: "activation/requested",
    workspaceId: "ws-1"
  });
  assert.equal(
    commandPort.abortSignalsByCommandId.get("activate:activation-1")?.aborted,
    false
  );

  engine.dispatch({
    agentSessionId: "session-1",
    type: "session/stopRequested",
    workspaceId: "ws-1"
  });

  assert.equal(
    commandPort.abortSignalsByCommandId.get("activate:activation-1")?.aborted,
    true
  );
  assert.equal(
    engine.getSnapshot().pendingIntents.activationsByRequestId["activation-1"]
      ?.status,
    "canceled"
  );
});

test("activation command result settles a new Session inside the Engine", async () => {
  const { engine } = createHarness({
    workspaceId: "workspace-1"
  });
  assert.equal(
    engine.activateSession({
      agentSessionId: "session-created",
      agentTargetId: "target-1",
      clientSubmitId: "submit-created",
      mode: "new",
      requestId: "activation-created"
    }),
    true
  );
  const created = activitySession("session-created", {
    createdAtUnixMs: 1,
    updatedAtUnixMs: 1
  });
  engine.dispatch({
    commandId: "activate:activation-created",
    commandType: "session/activate",
    correlationId: "activation-created",
    outcome: "succeeded",
    resultContract: "activation-v1",
    settledAtUnixMs: 250,
    type: "engine/commandResult",
    value: {
      activation: { mode: "new", status: "attached" },
      session: created
    }
  });
  await flushMicrotasks();

  assert.equal(
    engine.getSnapshot().sessionLifecycle.sessionsById["session-created"]
      ?.agentSessionId,
    "session-created"
  );
  assert.equal(
    engine.getSnapshot().pendingIntents.activationsByRequestId[
      "activation-created"
    ]?.status,
    "confirmed"
  );
  assert.equal(
    engine.getSnapshot().pendingIntents.activationsByRequestId[
      "activation-created"
    ]?.snapshotObservedAtUnixMs,
    250
  );
});

test("activation command result hydrates existing Session detail inside the Engine", async () => {
  const { engine } = createHarness({
    workspaceId: "workspace-1"
  });
  assert.equal(
    engine.activateSession({
      agentSessionId: "session-existing",
      mode: "existing",
      requestId: "activation-existing"
    }),
    true
  );
  const existing = activitySession("session-existing", {
    updatedAtUnixMs: 2
  });
  const turn = activityTurn("session-existing", "turn-existing");
  engine.dispatch({
    commandId: "activate:activation-existing",
    commandType: "session/activate",
    correlationId: "activation-existing",
    outcome: "succeeded",
    resultContract: "activation-v1",
    settledAtUnixMs: 350,
    type: "engine/commandResult",
    value: {
      activation: { mode: "existing", status: "already_attached" },
      detail: {
        childSessions: [],
        lifecycleCapabilitiesProjected: true,
        projection: "authoritative",
        session: existing,
        turns: [turn]
      },
      session: existing
    }
  });
  await flushMicrotasks();

  const snapshot = engine.getSnapshot();
  assert.equal(
    snapshot.pendingIntents.activationsByRequestId["activation-existing"]
      ?.status,
    "confirmed"
  );
  assert.equal(
    snapshot.pendingIntents.activationsByRequestId["activation-existing"]
      ?.snapshotObservedAtUnixMs,
    350
  );
  assert.equal(
    Object.values(snapshot.sessionLifecycle.turnsById)[0]?.turnId,
    "turn-existing"
  );
});

test("late activation projection cannot regress a newer realtime Session", async () => {
  const { engine } = createHarness({
    workspaceId: "workspace-1"
  });
  assert.equal(
    engine.activateSession({
      agentSessionId: "session-existing",
      mode: "existing",
      requestId: "activation-existing"
    }),
    true
  );
  engine.dispatch({
    session: activitySession("session-existing", {
      title: "Realtime title",
      updatedAtUnixMs: 20
    }),
    type: "session/upserted"
  });
  const stale = activitySession("session-existing", {
    title: "Stale activation title",
    updatedAtUnixMs: 10
  });
  engine.dispatch({
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
        session: stale,
        turns: []
      },
      session: stale
    }
  });
  await flushMicrotasks();

  assert.equal(
    engine.getSnapshot().sessionLifecycle.sessionsById["session-existing"]
      ?.title,
    "Realtime title"
  );
});

test("interleaving: expiry firing between probe start and probe result", async () => {
  const { commandPort, engine, timer } = createHarness();
  engine.dispatch({ probeId: "p-4", type: "engine/probeRequested" });
  engine.dispatch({
    dueAtUnixMs: 50,
    expiryId: "e-4",
    type: "engine/expiryRequested"
  });
  // The expiry elapses while the probe is still in flight.
  timer.advance(50);
  assert.equal(engine.getSnapshot().engineRuntime.lastExpiredIntentId, "e-4");
  assert.equal(engine.getSnapshot().engineRuntime.lastCommandResult, null);

  commandPort.succeed("p-4");
  await flushMicrotasks();
  assert.deepEqual(engine.getSnapshot().engineRuntime.lastCommandResult, {
    commandId: "p-4",
    outcome: "succeeded"
  });
  assert.equal(engine.getSnapshot().engineRuntime.lastExpiredIntentId, "e-4");
});

test("dispose cancels pending frames, expiries, and in-flight results", async () => {
  const { commandPort, diagnosticEvents, engine, notifiedStates, timer } =
    createHarness();
  engine.dispatch({
    dueAtUnixMs: 100,
    expiryId: "e-5",
    type: "engine/expiryRequested"
  });
  engine.dispatch({ probeId: "p-5", type: "engine/probeRequested" });
  engine.dispatch(
    { status: "connected", type: "engine/connectionChanged" },
    { batch: true }
  );
  const notificationsBeforeDispose = notifiedStates.length;

  engine.dispose();
  assert.equal(commandPort.abortSignalsByCommandId.get("p-5")?.aborted, true);
  assert.equal(timer.pendingTaskCount(), 0);

  timer.advance(1000);
  commandPort.succeed("p-5");
  await flushMicrotasks();
  engine.dispatch({ status: "connected", type: "engine/connectionChanged" });

  assert.equal(notifiedStates.length, notificationsBeforeDispose);
  assert.equal(engine.getSnapshot().engineRuntime.lastExpiredIntentId, null);
  assert.deepEqual(diagnosticEvents, [
    { commandId: "p-5", type: "commandResultAfterDispose" },
    {
      intentType: "engine/connectionChanged",
      type: "intentDroppedAfterDispose"
    }
  ]);
});

test("two instances with different origins do not interfere", () => {
  const local = createHarness({ origin: "local-tuttid" });
  const shared = createHarness({ origin: "shared-room" });

  local.engine.dispatch({
    status: "connected",
    type: "engine/connectionChanged"
  });
  assert.equal(
    local.engine.getSnapshot().engineRuntime.connection,
    "connected"
  );
  assert.equal(shared.engine.getSnapshot().engineRuntime.connection, "unknown");
  assert.equal(
    shared.engine.getSnapshot().engineRuntime.processedIntentCount,
    0
  );
  assert.notEqual(local.engine.identity.origin, shared.engine.identity.origin);
});

test("unsubscribed listeners stop receiving notifications", () => {
  const { engine } = createHarness();
  let callCount = 0;
  const unsubscribe = engine.subscribe(() => {
    callCount += 1;
  });
  engine.dispatch({ status: "connected", type: "engine/connectionChanged" });
  assert.equal(callCount, 1);
  unsubscribe();
  engine.dispatch({
    status: "disconnected",
    type: "engine/connectionChanged"
  });
  assert.equal(callCount, 1);
});

test("an authoritative Session detail snapshot notifies subscribers once", () => {
  const { engine, notifiedStates } = createHarness({
    workspaceId: "workspace-1"
  });
  const rootSession = activitySession("session-root");
  const childSession = activitySession("session-child", {
    kind: "child",
    parentAgentSessionId: "session-root",
    rootAgentSessionId: "session-root"
  });
  const turn = activityTurn("session-root", "turn-1");

  engine.dispatch({
    childSessions: [childSession],
    live: true,
    session: { ...rootSession, latestTurn: turn },
    turns: [turn],
    type: "session/detailSnapshotReceived",
    workspaceId: "workspace-1"
  });

  assert.equal(notifiedStates.length, 1);
  const snapshot = engine.getSnapshot();
  assert.ok(snapshot.sessionLifecycle.sessionsById["session-root"]);
  assert.ok(snapshot.sessionLifecycle.sessionsById["session-child"]);
  assert.equal(
    Object.values(snapshot.sessionLifecycle.turnsById)[0]?.turnId,
    "turn-1"
  );
});

test("a throwing listener is reported and does not block other listeners", () => {
  const { diagnosticEvents, engine } = createHarness();
  const listenerError = new Error("listener exploded");
  engine.subscribe(() => {
    throw listenerError;
  });
  let laterListenerCalls = 0;
  engine.subscribe(() => {
    laterListenerCalls += 1;
  });
  engine.dispatch({ status: "connected", type: "engine/connectionChanged" });
  assert.equal(laterListenerCalls, 1);
  assert.deepEqual(diagnosticEvents, [
    { error: listenerError, type: "listenerError" }
  ]);
});

test("intents dispatched from a listener are reduced in a follow-up drain", () => {
  const { engine, notifiedStates } = createHarness();
  let reentered = false;
  engine.subscribe(() => {
    if (!reentered) {
      reentered = true;
      engine.dispatch({ probeId: "p-6", type: "engine/probeRequested" });
    }
  });
  engine.dispatch({ status: "connected", type: "engine/connectionChanged" });
  assert.equal(engine.getSnapshot().engineRuntime.processedIntentCount, 2);
  // Two drains happened: the original intent and the reentrant one.
  assert.equal(notifiedStates.length, 2);
});

function activityTurn(
  agentSessionId: string,
  turnId: string
): AgentActivityTurn {
  return {
    agentSessionId,
    origin: "user_prompt",
    phase: "settled",
    settledAtUnixMs: 3,
    startedAtUnixMs: 2,
    turnId,
    updatedAtUnixMs: 3
  };
}

function activitySession(
  agentSessionId: string,
  overrides: Partial<AgentActivitySessionInput> = {}
): AgentActivitySessionInput {
  return {
    activeTurnId: null,
    agentSessionId,
    cwd: "/workspace",
    latestTurnInteractions: [],
    pendingInteractions: [],
    provider: "codex",
    title: "Session",
    workspaceId: "workspace-1",
    ...overrides
  };
}
