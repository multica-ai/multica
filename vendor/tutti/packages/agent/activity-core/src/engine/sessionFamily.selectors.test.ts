import assert from "node:assert/strict";
import test from "node:test";
import {
  createInitialAgentSessionEngineState,
  rootEngineReducer
} from "./rootReducer.ts";
import { createAgentSessionFamilySnapshotSelector } from "./sessionFamily.selectors.ts";

test("session family selector preserves unrelated root projection identity", () => {
  let state = rootEngineReducer(createInitialAgentSessionEngineState(), {
    sessions: [
      session("session-a"),
      session("session-a-child", {
        kind: "child",
        parentAgentSessionId: "session-a",
        rootAgentSessionId: "session-a"
      }),
      session("session-b")
    ],
    type: "session/snapshotReceived"
  }).state;

  const selectA = createAgentSessionFamilySnapshotSelector("session-a");
  const selectB = createAgentSessionFamilySnapshotSelector("session-b");
  const initialA = selectA(state);
  const initialB = selectB(state);

  state = rootEngineReducer(state, {
    session: session("session-a", { title: "renamed" }),
    type: "session/upserted"
  }).state;

  const updatedA = selectA(state);
  const updatedB = selectB(state);
  assert.notEqual(updatedA, initialA);
  assert.equal(updatedB, initialB);
});

function session(
  agentSessionId: string,
  overrides: Record<string, unknown> = {}
) {
  return {
    activeTurnId: null,
    agentSessionId,
    cwd: "/workspace",
    latestTurnInteractions: [],
    pendingInteractions: [],
    provider: "codex",
    title: agentSessionId,
    workspaceId: "workspace-1",
    ...overrides
  };
}
