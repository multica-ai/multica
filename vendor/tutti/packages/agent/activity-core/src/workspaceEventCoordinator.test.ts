import assert from "node:assert/strict";
import test from "node:test";
import { createAgentActivitySnapshotProjector } from "./engine/agentActivitySnapshot.projector.ts";
import { createAgentSessionEngine } from "./engine/createAgentSessionEngine.ts";
import { canonicalTurnKey } from "./engine/sessionEntityKeys.ts";
import {
  selectEngineSessionRuntimeActivity,
  selectWorkspaceAgentConsumerSession
} from "./engine/sessionLifecycle.selectors.ts";
import { createTestEngineCommandPort } from "./engine/testEngineCommandPort.ts";
import type { EngineExternalCommand } from "./engine/types.ts";
import { normalizeAgentActivitySession } from "./sessionNormalization.ts";
import type {
  AgentActivityTurn,
  AgentActivityTurnUpdatedEvent
} from "./types.ts";
import { createAgentActivityWorkspaceEventCoordinator } from "./workspaceEventCoordinator.ts";

function createHarness(options?: {
  notificationScheduler?: {
    schedule(delayMs: number, task: () => void): { cancel(): void };
  };
  onCanonicalRead?: () => void;
}) {
  const commands: EngineExternalCommand[] = [];
  const engine = createAgentSessionEngine({
    clock: { nowUnixMs: () => 10 },
    commandPort: createTestEngineCommandPort((command) => {
      commands.push(command);
      return Promise.resolve(undefined);
    }),
    identity: { origin: "test", workspaceId: "workspace-1" },
    scheduler: {
      schedule(_delayMs, task) {
        task();
        return { cancel() {} };
      }
    }
  });
  const projectCanonical = createAgentActivitySnapshotProjector("workspace-1");
  const readCanonicalSnapshot = () => {
    options?.onCanonicalRead?.();
    return projectCanonical(engine.getSnapshot());
  };
  const coordinator = createAgentActivityWorkspaceEventCoordinator({
    engine,
    notificationScheduler: options?.notificationScheduler ?? {
      schedule(_delayMs, task) {
        let canceled = false;
        queueMicrotask(() => {
          if (!canceled) task();
        });
        return {
          cancel() {
            canceled = true;
          }
        };
      }
    },
    readCanonicalSnapshot,
    workspaceId: "workspace-1"
  });
  return { commands, coordinator, engine, readCanonicalSnapshot };
}

test("projects provider runtime activity before a canonical Turn and clears it on disconnect", () => {
  const harness = createHarness();
  const running = harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "runtime_activity_update",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      eventType: "runtime_activity_update",
      state: "running",
      occurredAtUnixMs: 10
    }
  });

  assert.equal(running.accepted, true);
  assert.equal(
    selectEngineSessionRuntimeActivity(
      harness.engine.getSnapshot(),
      "session-1"
    ),
    "running"
  );

  harness.engine.dispatch({
    session: session(null, 10),
    type: "session/upserted"
  });
  assert.equal(
    selectWorkspaceAgentConsumerSession(
      harness.engine.getSnapshot(),
      "session-1"
    )?.displayStatus,
    "working"
  );

  harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "runtime_activity_update",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      eventType: "runtime_activity_update",
      state: "idle",
      occurredAtUnixMs: 11
    }
  });
  assert.equal(
    selectWorkspaceAgentConsumerSession(
      harness.engine.getSnapshot(),
      "session-1"
    )?.displayStatus,
    "idle"
  );

  harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "runtime_activity_update",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      eventType: "runtime_activity_update",
      state: "running",
      occurredAtUnixMs: 12
    }
  });

  harness.coordinator.eventStreamConnectionChanged({ status: "disconnected" });
  assert.equal(
    selectEngineSessionRuntimeActivity(
      harness.engine.getSnapshot(),
      "session-1"
    ),
    "idle"
  );

  harness.coordinator.dispose();
  harness.engine.dispose();
});

test("rejects stale and tombstoned runtime activity observations", () => {
  const harness = createHarness();
  harness.engine.dispatch({
    session: session(null, 10),
    type: "session/upserted"
  });
  harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "runtime_activity_update",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      eventType: "runtime_activity_update",
      state: "idle",
      occurredAtUnixMs: 12
    }
  });

  harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "runtime_activity_update",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      eventType: "runtime_activity_update",
      state: "running",
      occurredAtUnixMs: 11
    }
  });
  assert.equal(
    selectEngineSessionRuntimeActivity(
      harness.engine.getSnapshot(),
      "session-1"
    ),
    "idle"
  );

  harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "session_deleted",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      eventType: "session_deleted",
      deletedAtUnixMs: 13
    }
  });
  const tombstoned = harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "runtime_activity_update",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      eventType: "runtime_activity_update",
      state: "running",
      occurredAtUnixMs: 14
    }
  });
  assert.equal(tombstoned.reason, "tombstoned");
  assert.equal(
    harness.engine.getSnapshot().sessionLifecycle.operationBySessionId[
      "session-1"
    ],
    undefined
  );

  harness.coordinator.dispose();
  harness.engine.dispose();
});

test("rejects runtime activity whose data identity does not match the envelope", () => {
  const harness = createHarness();
  const result = harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "runtime_activity_update",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-other",
      eventType: "runtime_activity_update",
      state: "running",
      occurredAtUnixMs: 10
    }
  });

  assert.equal(result.reason, "identity_mismatch");
  assert.equal(
    harness.engine.getSnapshot().sessionLifecycle.operationBySessionId[
      "session-1"
    ],
    undefined
  );
  harness.coordinator.dispose();
  harness.engine.dispose();
});

test("a stale running observation cannot override a settled canonical Turn", () => {
  const harness = createHarness();
  harness.engine.dispatch({
    session: session(turn("settled", 20), 20),
    type: "session/upserted"
  });
  harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "runtime_activity_update",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      eventType: "runtime_activity_update",
      state: "running",
      occurredAtUnixMs: 19
    }
  });

  assert.equal(
    selectWorkspaceAgentConsumerSession(
      harness.engine.getSnapshot(),
      "session-1"
    )?.displayStatus,
    "completed"
  );

  harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "runtime_activity_update",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      eventType: "runtime_activity_update",
      state: "running",
      occurredAtUnixMs: 21
    }
  });
  assert.equal(
    selectWorkspaceAgentConsumerSession(
      harness.engine.getSnapshot(),
      "session-1"
    )?.displayStatus,
    "working"
  );
  harness.coordinator.dispose();
  harness.engine.dispose();
});

test("projects message deltas and clears them on authoritative deletion", () => {
  const harness = createHarness();
  const applied = harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "message_delta",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      messageId: "message-1",
      turnId: "turn-1",
      role: "assistant",
      kind: "text",
      occurredAtUnixMs: 10,
      content: { operation: "set", value: "hello" }
    }
  });

  assert.equal(applied.accepted, true);
  assert.equal(applied.optimisticMessage?.payload.text, "hello");
  assert.equal(
    harness.coordinator.project(harness.readCanonicalSnapshot())
      .sessionMessagesById["session-1"]?.[0]?.payload.text,
    "hello"
  );

  const deleted = harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "session_deleted",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      eventType: "session_deleted",
      deletedAtUnixMs: 11
    }
  });

  assert.equal(deleted.reason, "deleted");
  assert.equal(harness.coordinator.isSessionDeleted("session-1"), true);
  assert.equal(
    harness.coordinator.project(harness.readCanonicalSnapshot())
      .sessionMessagesById["session-1"],
    undefined
  );
  harness.coordinator.dispose();
  harness.engine.dispose();
});

test("an explicit restore event clears only its tombstone and requests authoritative hydration", () => {
  const harness = createHarness();
  harness.engine.dispatch({
    session: session(null, 10),
    type: "session/upserted"
  });
  harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "session_deleted",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      eventType: "session_deleted",
      deletedAtUnixMs: 11
    }
  });

  const restored = harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "session_restored",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      eventType: "session_restored",
      restoredAtUnixMs: 12
    }
  });

  assert.equal(restored.reason, "restored");
  assert.equal(harness.coordinator.isSessionDeleted("session-1"), false);
  assert.ok(
    harness.commands.some(
      (command) =>
        command.type === "session/reconcile" &&
        command.agentSessionId === "session-1"
    )
  );
  harness.engine.dispatch({
    session: session(null, 10),
    type: "session/upserted"
  });
  assert.equal(
    harness
      .readCanonicalSnapshot()
      .sessions.some((candidate) => candidate.agentSessionId === "session-1"),
    true
  );
  harness.coordinator.dispose();
  harness.engine.dispose();
});

test("applies a settled Turn and its cleared Session reference atomically", () => {
  const harness = createHarness();
  const runningTurn = turn("running", 1);
  harness.engine.dispatch({
    session: session(runningTurn, 1),
    type: "session/upserted"
  });
  const observations: Array<{
    activeTurnId: string | null;
    turnPhase: AgentActivityTurn["phase"] | null;
  }> = [];
  const unsubscribe = harness.engine.subscribe((state) => {
    observations.push({
      activeTurnId:
        state.sessionLifecycle.sessionsById["session-1"]?.activeTurnId ?? null,
      turnPhase:
        state.sessionLifecycle.turnsById[
          canonicalTurnKey("session-1", "turn-1")
        ]?.phase ?? null
    });
  });

  const result = harness.coordinator.ingestEvent(turnUpdateEvent("settled", 2));
  const lifecycle = harness.engine.getSnapshot().sessionLifecycle;

  assert.equal(result.accepted, true);
  assert.equal(lifecycle.sessionsById["session-1"]?.activeTurnId, null);
  assert.equal(lifecycle.sessionsById["session-1"]?.updatedAtUnixMs, 1);
  assert.equal(
    lifecycle.turnsById[canonicalTurnKey("session-1", "turn-1")]?.phase,
    "settled"
  );
  assert.ok(
    observations.every(
      (observation) =>
        observation.turnPhase !== "settled" || observation.activeTurnId === null
    )
  );
  assert.ok(
    harness.commands.some(
      (command) =>
        command.type === "session/reconcile" &&
        command.agentSessionId === "session-1" &&
        command.live &&
        command.scope === "state_and_messages"
    )
  );

  unsubscribe();
  harness.coordinator.dispose();
  harness.engine.dispose();
});

test("rejects a late Turn fact", () => {
  const harness = createHarness();
  const runningTurn = turn("running", 4);
  harness.engine.dispatch({
    session: session(runningTurn, 4),
    type: "session/upserted"
  });

  const result = harness.coordinator.ingestEvent(
    turnUpdateEvent("settled", 2, "turn-1", 100)
  );
  const snapshot = harness.engine.getSnapshot();

  assert.equal(result.accepted, true);
  assert.equal(
    snapshot.sessionLifecycle.turnsById[canonicalTurnKey("session-1", "turn-1")]
      ?.phase,
    "running"
  );
  assert.equal(
    snapshot.sessionLifecycle.sessionsById["session-1"]?.activeTurnId,
    "turn-1"
  );

  harness.coordinator.dispose();
  harness.engine.dispose();
});

test("accepts host-fenced settlement across source version domains", () => {
  const harness = createHarness();
  const runningTurn = turn("running", 4);
  harness.engine.dispatch({
    session: session(runningTurn, 4),
    type: "session/upserted"
  });

  const result = harness.coordinator.ingestEvent(
    turnUpdateEvent("settled", 2, "turn-1", 100),
    { hostFencedSameTurnSettlement: true }
  );
  const snapshot = harness.engine.getSnapshot();

  assert.equal(result.accepted, true);
  assert.equal(
    snapshot.sessionLifecycle.turnsById[canonicalTurnKey("session-1", "turn-1")]
      ?.phase,
    "settled"
  );
  assert.equal(
    snapshot.sessionLifecycle.sessionsById["session-1"]?.activeTurnId,
    null
  );

  harness.coordinator.dispose();
  harness.engine.dispose();
});

test("rejects an inconsistent Turn projection and reconciles state", () => {
  const harness = createHarness();
  const runningTurn = turn("running", 1);
  harness.engine.dispatch({
    session: session(runningTurn, 1),
    type: "session/upserted"
  });
  const inconsistent = turnUpdateEvent("settled", 2);
  inconsistent.data.activeTurnId = "turn-1";

  const result = harness.coordinator.ingestEvent(inconsistent);
  const lifecycle = harness.engine.getSnapshot().sessionLifecycle;

  assert.equal(result.accepted, false);
  assert.equal(result.reason, "invalid_turn");
  assert.equal(
    lifecycle.turnsById[canonicalTurnKey("session-1", "turn-1")]?.phase,
    "running"
  );
  assert.equal(lifecycle.sessionsById["session-1"]?.activeTurnId, "turn-1");
  assert.ok(
    harness.commands.some(
      (command) =>
        command.type === "session/reconcile" &&
        command.agentSessionId === "session-1" &&
        command.live &&
        command.scope === "state"
    )
  );

  harness.coordinator.dispose();
  harness.engine.dispose();
});

test("authoritative history drops a terminal optimistic row from a retracted Turn", () => {
  const harness = createHarness();
  harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "message_delta",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      messageId: "retracted-message",
      turnId: "retracted-turn",
      role: "assistant",
      kind: "text",
      occurredAtUnixMs: 10,
      completedAtUnixMs: 11,
      status: "completed",
      content: { operation: "set", value: "old answer" }
    }
  });

  assert.equal(
    harness.coordinator.project(harness.readCanonicalSnapshot())
      .sessionMessagesById["session-1"]?.length,
    1
  );

  harness.coordinator.reconcileAuthoritativeHistory("session-1", [], []);

  assert.equal(
    harness.coordinator.project(harness.readCanonicalSnapshot())
      .sessionMessagesById["session-1"]?.length,
    0
  );
  harness.coordinator.dispose();
  harness.engine.dispose();
});

test("preserves unrelated Session message projections during an optimistic delta", () => {
  const harness = createHarness();
  harness.engine.dispatch({
    messages: [
      message("session-a", "message-a", "canonical a"),
      message("session-b", "message-b", "canonical b")
    ],
    type: "message/snapshotReceived",
    workspaceId: "workspace-1"
  });
  const before = harness.coordinator.project(harness.readCanonicalSnapshot());

  harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-a",
    eventType: "message_delta",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-a",
      messageId: "message-a",
      turnId: "turn-1",
      role: "assistant",
      kind: "text",
      occurredAtUnixMs: 10,
      content: { operation: "set", value: "canonical a live" }
    }
  });

  const after = harness.coordinator.project(harness.readCanonicalSnapshot());
  assert.notEqual(
    after.sessionMessagesById["session-a"],
    before.sessionMessagesById["session-a"]
  );
  assert.equal(
    after.sessionMessagesById["session-a"]?.[0]?.payload.text,
    "canonical a live"
  );
  assert.equal(
    after.sessionMessagesById["session-b"],
    before.sessionMessagesById["session-b"]
  );
  harness.coordinator.dispose();
  harness.engine.dispose();
});

test("reconnect hydrates only the workspace, priority sessions, and optimistic messages", () => {
  const harness = createHarness();
  harness.engine.dispatch({
    messages: [
      {
        workspaceId: "workspace-1",
        agentSessionId: "session-cached",
        messageId: "message-1",
        version: 1,
        sequence: 1,
        turnId: "turn-1",
        role: "assistant",
        kind: "text",
        payload: { text: "cached" },
        occurredAtUnixMs: 1
      }
    ],
    type: "message/snapshotReceived",
    workspaceId: "workspace-1"
  });
  harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-overlay-only",
    eventType: "message_delta",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-overlay-only",
      messageId: "message-overlay",
      turnId: "turn-overlay",
      role: "assistant",
      kind: "text",
      occurredAtUnixMs: 2,
      content: { operation: "set", value: "optimistic" }
    }
  });

  harness.coordinator.eventStreamConnectionChanged({
    status: "connected",
    prioritySessionIds: ["session-selected"]
  });
  harness.coordinator.eventStreamConnectionChanged({
    status: "disconnected"
  });
  harness.coordinator.eventStreamConnectionChanged({
    status: "connected",
    prioritySessionIds: ["session-selected"]
  });

  assert.ok(
    harness.commands.some(
      (command) =>
        command.type === "engine/reconcileWorkspace" &&
        command.workspaceId === "workspace-1"
    )
  );
  assert.ok(
    harness.commands.some(
      (command) =>
        command.type === "session/reconcile" &&
        command.agentSessionId === "session-selected" &&
        command.scope === "state_and_messages"
    )
  );
  assert.equal(
    harness.commands.some(
      (command) =>
        command.type === "session/reconcile" &&
        command.agentSessionId === "session-cached"
    ),
    false
  );
  assert.ok(
    harness.commands.some(
      (command) =>
        command.type === "session/reconcile" &&
        command.agentSessionId === "session-overlay-only" &&
        command.scope === "state_and_messages"
    )
  );
  harness.coordinator.dispose();
  harness.engine.dispose();
});

test("coalesces optimistic delta notifications without dropping ordered content", () => {
  const scheduled: Array<{ cancelled: boolean; task: () => void }> = [];
  let canonicalReads = 0;
  const harness = createHarness({
    onCanonicalRead: () => {
      canonicalReads += 1;
    },
    notificationScheduler: {
      schedule(_delayMs, task) {
        const entry = { cancelled: false, task };
        scheduled.push(entry);
        return {
          cancel() {
            entry.cancelled = true;
          }
        };
      }
    }
  });
  let notifications = 0;
  harness.coordinator.subscribe(() => {
    notifications += 1;
  });

  for (let index = 0; index < 100; index += 1) {
    harness.coordinator.ingestEvent({
      workspaceId: "workspace-1",
      agentSessionId: "session-streaming",
      eventType: "message_delta",
      data: {
        workspaceId: "workspace-1",
        agentSessionId: "session-streaming",
        messageId: "message-streaming",
        turnId: "turn-streaming",
        role: "assistant",
        kind: "text",
        occurredAtUnixMs: index + 1,
        content:
          index === 0
            ? { operation: "set", value: "0" }
            : { operation: "append_text", text: String(index) }
      }
    });
  }

  assert.equal(notifications, 0);
  assert.equal(scheduled.length, 1);
  assert.equal(canonicalReads, 0);
  scheduled[0]?.task();
  assert.equal(notifications, 1);
  assert.equal(
    harness.coordinator.project(harness.readCanonicalSnapshot())
      .sessionMessagesById["session-streaming"]?.[0]?.payload.text,
    Array.from({ length: 100 }, (_, index) => String(index)).join("")
  );

  harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-streaming",
    eventType: "message_delta",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-streaming",
      messageId: "message-streaming",
      turnId: "turn-streaming",
      role: "assistant",
      kind: "text",
      occurredAtUnixMs: 101,
      content: { operation: "append_text", text: "100" }
    }
  });

  assert.equal(notifications, 1);
  assert.equal(scheduled.length, 2);
  scheduled[1]?.task();
  assert.equal(notifications, 2);
  assert.equal(
    harness.coordinator.project(harness.readCanonicalSnapshot())
      .sessionMessagesById["session-streaming"]?.[0]?.payload.text,
    Array.from({ length: 101 }, (_, index) => String(index)).join("")
  );

  harness.coordinator.dispose();
  harness.engine.dispose();
});

test("settled turn updates request a combined reconcile even without inline messages", () => {
  const harness = createHarness();

  harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "turn_update",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      eventType: "turn_update",
      occurredAtUnixMs: 10,
      activeTurnId: null,
      turn: {
        agentSessionId: "session-1",
        completedCommand: null,
        error: null,
        fileChanges: null,
        origin: "user_prompt",
        outcome: "completed",
        phase: "settled",
        startedAtUnixMs: 1,
        settledAtUnixMs: 10,
        turnId: "turn-1",
        updatedAtUnixMs: 10
      }
    }
  });

  assert.ok(
    harness.commands.some(
      (command) =>
        command.type === "session/reconcile" &&
        command.agentSessionId === "session-1" &&
        command.scope === "state_and_messages"
    )
  );
  harness.coordinator.dispose();
  harness.engine.dispose();
});
function message(agentSessionId: string, messageId: string, text: string) {
  return {
    workspaceId: "workspace-1",
    agentSessionId,
    messageId,
    version: 1,
    sequence: 1,
    turnId: "turn-1",
    role: "assistant" as const,
    kind: "text",
    payload: { text },
    occurredAtUnixMs: 1
  };
}

function session(turnValue: AgentActivityTurn | null, updatedAtUnixMs: number) {
  return normalizeAgentActivitySession({
    activeTurn: turnValue,
    activeTurnId: turnValue?.turnId ?? null,
    agentSessionId: "session-1",
    cwd: "/workspace",
    latestTurn: turnValue,
    latestTurnInteractions: [],
    pendingInteractions: [],
    provider: "codex",
    title: "Session",
    updatedAtUnixMs,
    userId: "user-1",
    workspaceId: "workspace-1"
  });
}

function turn(
  phase: AgentActivityTurn["phase"],
  updatedAtUnixMs: number,
  turnId = "turn-1"
): AgentActivityTurn {
  return {
    agentSessionId: "session-1",
    ...(phase === "settled"
      ? {
          completedCommand: null,
          error: null,
          fileChanges: null,
          outcome: "completed" as const,
          settledAtUnixMs: updatedAtUnixMs
        }
      : {}),
    origin: "user_prompt",
    phase,
    startedAtUnixMs: 1,
    turnId,
    updatedAtUnixMs
  };
}

function turnUpdateEvent(
  phase: AgentActivityTurn["phase"],
  updatedAtUnixMs: number,
  turnId = "turn-1",
  occurredAtUnixMs = updatedAtUnixMs
): AgentActivityTurnUpdatedEvent {
  const turnValue = turn(phase, updatedAtUnixMs, turnId);
  return {
    agentSessionId: "session-1",
    data: {
      activeTurnId: phase === "settled" ? null : turnId,
      agentSessionId: "session-1",
      eventType: "turn_update",
      occurredAtUnixMs,
      turn: {
        ...turnValue,
        completedCommand: null,
        error: turnValue.error ?? null,
        fileChanges: turnValue.fileChanges ?? null,
        outcome: turnValue.outcome ?? null,
        settledAtUnixMs: turnValue.settledAtUnixMs ?? null
      },
      workspaceId: "workspace-1"
    },
    eventType: "turn_update",
    workspaceId: "workspace-1"
  };
}

test("invalid wire delta stays inside the coordinator and requests reconcile", () => {
  const harness = createHarness();
  const result = harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "message_delta",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-1",
      content: { operation: "append_text" }
    }
  });

  assert.equal(result.reason, "invalid_delta");
  assert.ok(
    harness.commands.some(
      (command) =>
        command.type === "session/reconcile" &&
        command.agentSessionId === "session-1"
    )
  );
  harness.coordinator.dispose();
  harness.engine.dispose();
});

test("mismatched deletion identity reconciles instead of tombstoning", () => {
  const harness = createHarness();
  const result = harness.coordinator.ingestEvent({
    workspaceId: "workspace-1",
    agentSessionId: "session-1",
    eventType: "session_deleted",
    data: {
      workspaceId: "workspace-1",
      agentSessionId: "session-other",
      eventType: "session_deleted",
      deletedAtUnixMs: 11
    }
  });

  assert.equal(result.reason, "identity_mismatch");
  assert.equal(harness.coordinator.isSessionDeleted("session-1"), false);
  assert.ok(
    harness.commands.some(
      (command) =>
        command.type === "session/reconcile" &&
        command.agentSessionId === "session-1"
    )
  );
  harness.coordinator.dispose();
  harness.engine.dispose();
});
