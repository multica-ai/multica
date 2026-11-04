import assert from "node:assert/strict";
import test from "node:test";
import {
  createAgentActivityEphemeralConversationProjector,
  type AgentActivityEphemeralConversationEvent
} from "./ephemeralConversationProjector.ts";

function projector() {
  return createAgentActivityEphemeralConversationProjector({
    workspaceId: "workspace-1",
    agentSessionId: "side-1",
    sourceAgentSessionId: "source-1",
    provider: "codex",
    cwd: "/workspace",
    occurredAtUnixMs: 100
  });
}

function event(
  sequence: number,
  change: AgentActivityEphemeralConversationEvent["change"]
): AgentActivityEphemeralConversationEvent {
  return {
    workspaceId: "workspace-1",
    agentSessionId: "side-1",
    sourceAgentSessionId: "source-1",
    sequence,
    change
  };
}

test("projects optimistic and provider messages into one ephemeral snapshot", () => {
  const subject = projector();
  subject.beginTurn({
    turnId: "turn-1",
    content: "question",
    occurredAtUnixMs: 110
  });
  subject.apply(
    event(1, {
      kind: "message_update",
      message: {
        workspaceId: "workspace-1",
        agentSessionId: "side-1",
        messageId: "provider-user-1",
        version: 1,
        turnId: "turn-1",
        role: "user",
        kind: "text",
        status: "completed",
        payload: { text: "question", content: "question" },
        occurredAtUnixMs: 111
      }
    })
  );
  subject.apply(
    event(2, {
      kind: "message_delta",
      data: {
        workspaceId: "workspace-1",
        agentSessionId: "side-1",
        messageId: "assistant-1",
        turnId: "turn-1",
        role: "assistant",
        kind: "text",
        occurredAtUnixMs: 112,
        content: { operation: "set", value: "Hel" }
      }
    })
  );
  subject.apply(
    event(3, {
      kind: "message_delta",
      data: {
        workspaceId: "workspace-1",
        agentSessionId: "side-1",
        messageId: "assistant-1",
        turnId: "turn-1",
        role: "assistant",
        kind: "text",
        occurredAtUnixMs: 113,
        content: { operation: "append_text", text: "lo" }
      }
    })
  );

  const snapshot = subject.getSnapshot();
  assert.equal(snapshot.expired, false);
  assert.equal(snapshot.sequence, 3);
  assert.equal(snapshot.activitySnapshot.sessions[0]?.activeTurnId, "turn-1");
  assert.deepEqual(
    snapshot.messages.map((message) => ({
      id: message.messageId,
      role: message.role,
      text: message.payload.text
    })),
    [
      { id: "provider-user-1", role: "user", text: "question" },
      { id: "assistant-1", role: "assistant", text: "Hello" }
    ]
  );
});

test("projects tool, reasoning, turn, and interaction state without durable reconciliation", () => {
  const subject = projector();
  subject.apply(
    event(1, {
      kind: "state_patch",
      patch: {
        currentPhase: "working",
        activeTurnId: "turn-1",
        occurredAtUnixMs: 120,
        turn: {
          turnId: "turn-1",
          activeTurnId: "turn-1",
          phase: "running",
          origin: "user_prompt",
          startedAtUnixMs: 120
        },
        interaction: {
          requestId: "request-1",
          turnId: "turn-1",
          kind: "approval",
          status: "pending",
          toolName: "shell",
          input: { command: "git status" },
          metadata: {
            actions: [{ id: "allow", label: "Allow" }]
          }
        }
      }
    })
  );
  subject.apply(
    event(2, {
      kind: "message_update",
      message: {
        workspaceId: "workspace-1",
        agentSessionId: "side-1",
        messageId: "reasoning-1",
        version: 2,
        turnId: "turn-1",
        role: "assistant",
        kind: "reasoning",
        status: "streaming",
        payload: { text: "Thinking" },
        occurredAtUnixMs: 121
      }
    })
  );
  subject.apply(
    event(3, {
      kind: "message_update",
      message: {
        workspaceId: "workspace-1",
        agentSessionId: "side-1",
        messageId: "tool-1",
        version: 3,
        turnId: "turn-1",
        role: "assistant",
        kind: "tool_call",
        status: "running",
        payload: {
          toolName: "shell",
          input: { command: "git status" }
        },
        occurredAtUnixMs: 122
      }
    })
  );

  const snapshot = subject.getSnapshot();
  assert.equal(snapshot.sessionTurns[0]?.phase, "running");
  assert.equal(snapshot.interactions[0]?.status, "pending");
  assert.deepEqual(
    snapshot.messages.map(
      (message) => message.kind
    ),
    ["reasoning", "tool_call"]
  );
});

test("expires on sequence gaps and terminal session patches", () => {
  const gap = projector();
  assert.deepEqual(gap.apply(event(2, { kind: "noop" })), {
    applied: false,
    expired: true,
    reason: "sequence_gap"
  });

  const terminal = projector();
  terminal.apply(
    event(1, {
      kind: "state_patch",
      patch: {
        lifecycleStatus: "completed",
        occurredAtUnixMs: 130
      }
    })
  );
  assert.equal(terminal.getSnapshot().expired, true);
  assert.equal(terminal.getSnapshot().expiryReason, "terminal_session");
});
