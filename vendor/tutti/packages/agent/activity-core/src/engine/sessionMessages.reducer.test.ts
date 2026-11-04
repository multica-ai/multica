import assert from "node:assert/strict";
import test from "node:test";
import type { AgentActivityDurableMessage } from "../types.ts";
import {
  createInitialSessionMessagesState,
  sessionMessagesReducer,
  type SessionMessagesReducerContext
} from "./sessionMessages.reducer.ts";

function message(
  overrides: Partial<AgentActivityDurableMessage> & {
    messageId: string;
    agentSessionId: string;
  }
): AgentActivityDurableMessage {
  return {
    workspaceId: "workspace-1",
    role: "assistant",
    sequence: overrides.sequence ?? overrides.version ?? 1,
    kind: "text",
    turnId: "turn-1",
    version: 1,
    status: null,
    payload: {},
    occurredAtUnixMs: 1,
    ...overrides
  };
}

const context: SessionMessagesReducerContext = {
  sessionsById: {
    "session-1": {
      agentSessionId: "session-1",
      provider: "codex",
      providerSessionId: "provider-1"
    }
  }
};

test("merges messages into the canonical session bucket", () => {
  const state = sessionMessagesReducer(
    createInitialSessionMessagesState(),
    {
      type: "message/snapshotReceived",
      messages: [message({ messageId: "m1", agentSessionId: "session-1" })]
    },
    context
  ).state;
  assert.deepEqual(
    state.messagesBySessionId["session-1"]?.map((item) => item.messageId),
    ["m1"]
  );
});

test("authoritative history replaces the session bucket and removes retracted messages", () => {
  let state = sessionMessagesReducer(
    createInitialSessionMessagesState(),
    {
      type: "message/snapshotReceived",
      messages: [
        message({ messageId: "retracted", agentSessionId: "session-1" }),
        message({
          messageId: "effective",
          agentSessionId: "session-1",
          version: 2
        })
      ]
    },
    context
  ).state;

  state = sessionMessagesReducer(
    state,
    {
      agentSessionId: "session-1",
      childSessions: [],
      historyRevision: 1,
      messages: [
        message({
          messageId: "effective",
          agentSessionId: "session-1",
          version: 2
        })
      ],
      session: authoritativeSession(),
      turns: [],
      type: "session/historyAuthoritativeSnapshotReceived",
      workspaceId: "workspace-1"
    },
    context
  ).state;

  assert.deepEqual(
    state.messagesBySessionId["session-1"]?.map((item) => item.messageId),
    ["effective"]
  );
});

function authoritativeSession() {
  return {
    activeTurn: null,
    activeTurnId: null,
    agentSessionId: "session-1",
    agentTargetId: null,
    capabilities: null,
    createdAtUnixMs: 1,
    cwd: "/workspace",
    endedAtUnixMs: null,
    imported: false,
    kind: "root" as const,
    lastEventUnixMs: 2,
    latestTurn: null,
    latestTurnInteractions: [],
    messageVersion: 2,
    parentAgentSessionId: null,
    parentToolCallId: null,
    parentTurnId: null,
    pendingInteractions: [],
    permissionConfig: { configurable: false, modes: [] },
    pinnedAtUnixMs: null,
    provider: "codex",
    providerSessionId: "provider-1",
    resumable: true,
    rootAgentSessionId: null,
    rootTurnId: null,
    settings: {},
    startedAtUnixMs: 1,
    title: "Session",
    updatedAtUnixMs: 2,
    usage: null,
    visible: true,
    workspaceId: "workspace-1"
  };
}

test("stores authoritative message-window coverage without inferring from versions", () => {
  const state = sessionMessagesReducer(
    createInitialSessionMessagesState(),
    {
      type: "message/snapshotReceived",
      messages: [
        message({
          messageId: "streamed-message",
          agentSessionId: "session-1",
          version: 446
        })
      ],
      sessionMessageWindows: [
        {
          agentSessionId: "session-1",
          hasOlderMessages: false,
          oldestLoadedVersion: 446
        }
      ]
    },
    context
  ).state;

  assert.deepEqual(state.windowsBySessionId["session-1"], {
    hasOlderMessages: false,
    oldestLoadedVersion: 446
  });
});

test("canonicalizes and removes message-window buckets with session identity", () => {
  let state = sessionMessagesReducer(
    createInitialSessionMessagesState(),
    {
      type: "message/snapshotReceived",
      messages: [],
      sessionMessageWindows: [
        {
          agentSessionId: "provider-1",
          hasOlderMessages: true,
          oldestLoadedVersion: 101
        }
      ]
    },
    { sessionsById: {} }
  ).state;

  state = sessionMessagesReducer(
    state,
    {
      type: "session/upserted",
      session: {
        activeTurnId: null,
        agentSessionId: "session-1",
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "codex",
        providerSessionId: "provider-1",
        title: "Session",
        workspaceId: "workspace-1"
      }
    },
    context
  ).state;

  assert.equal(state.windowsBySessionId["provider-1"], undefined);
  assert.deepEqual(state.windowsBySessionId["session-1"], {
    hasOlderMessages: true,
    oldestLoadedVersion: 101
  });

  state = sessionMessagesReducer(state, {
    type: "session/removed",
    agentSessionId: "session-1"
  }).state;
  assert.equal(state.windowsBySessionId["session-1"], undefined);
});

test("a higher version replaces the existing message; a lower version is dropped", () => {
  let state = sessionMessagesReducer(
    createInitialSessionMessagesState(),
    {
      type: "message/snapshotReceived",
      messages: [
        message({ messageId: "m1", agentSessionId: "session-1", version: 2 })
      ]
    },
    context
  ).state;
  state = sessionMessagesReducer(
    state,
    {
      type: "message/snapshotReceived",
      messages: [
        message({ messageId: "m1", agentSessionId: "session-1", version: 1 })
      ]
    },
    context
  ).state;
  assert.equal(state.messagesBySessionId["session-1"]?.[0]?.version, 2);
});

test("folds a provider-scoped alias bucket into the canonical bucket", () => {
  let state = sessionMessagesReducer(
    createInitialSessionMessagesState(),
    {
      type: "message/snapshotReceived",
      // arrives before the canonical session identity is known
      messages: [message({ messageId: "m1", agentSessionId: "provider-1" })]
    },
    { sessionsById: {} }
  ).state;
  assert.ok(state.messagesBySessionId["provider-1"]);
  // once the session is known, a canonical write collapses the alias bucket
  state = sessionMessagesReducer(
    state,
    {
      type: "message/snapshotReceived",
      messages: [message({ messageId: "m2", agentSessionId: "provider-1" })]
    },
    context
  ).state;
  assert.equal(state.messagesBySessionId["provider-1"], undefined);
  assert.deepEqual(
    state.messagesBySessionId["session-1"]?.map((item) => item.messageId),
    ["m1", "m2"]
  );
});

test("session identity arrival folds an existing provider alias bucket", () => {
  let state = sessionMessagesReducer(
    createInitialSessionMessagesState(),
    {
      type: "message/snapshotReceived",
      messages: [message({ messageId: "m1", agentSessionId: "provider-1" })]
    },
    { sessionsById: {} }
  ).state;
  state = sessionMessagesReducer(
    state,
    {
      type: "session/upserted",
      session: {
        activeTurnId: null,
        agentSessionId: "session-1",
        cwd: "/workspace",
        latestTurnInteractions: [],
        pendingInteractions: [],
        provider: "codex",
        providerSessionId: "provider-1",
        title: "Session",
        workspaceId: "workspace-1"
      }
    },
    context
  ).state;
  assert.equal(state.messagesBySessionId["provider-1"], undefined);
  assert.equal(
    state.messagesBySessionId["session-1"]?.[0]?.agentSessionId,
    "session-1"
  );
});

test("session/removed drops the session bucket", () => {
  let state = sessionMessagesReducer(
    createInitialSessionMessagesState(),
    {
      type: "message/snapshotReceived",
      messages: [message({ messageId: "m1", agentSessionId: "session-1" })]
    },
    context
  ).state;
  state = sessionMessagesReducer(state, {
    type: "session/removed",
    agentSessionId: "session-1"
  }).state;
  assert.equal(state.messagesBySessionId["session-1"], undefined);
});

test("session/removed drops a provider alias bucket using previous identity", () => {
  let state = sessionMessagesReducer(
    createInitialSessionMessagesState(),
    {
      type: "message/snapshotReceived",
      messages: [message({ messageId: "m1", agentSessionId: "provider-1" })]
    },
    { sessionsById: {} }
  ).state;
  state = sessionMessagesReducer(
    state,
    {
      type: "session/removed",
      agentSessionId: "session-1"
    },
    { previousSessionsById: context.sessionsById, sessionsById: {} }
  ).state;
  assert.equal(state.messagesBySessionId["provider-1"], undefined);
});
