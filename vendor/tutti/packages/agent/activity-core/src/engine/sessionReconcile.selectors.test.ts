import assert from "node:assert/strict";
import test from "node:test";
import {
  createInitialAgentSessionEngineState,
  rootEngineReducer
} from "./rootReducer.ts";
import {
  selectEngineSessionDetailHydrated,
  selectEngineSessionDetailLoading,
  selectEngineSessionReconcile,
  selectEngineSessionStateHydrated
} from "./sessionReconcile.selectors.ts";
import { normalizeAgentActivitySession } from "../sessionNormalization.ts";

test("state-only reconcile does not block conversation detail", () => {
  const state = rootEngineReducer(createInitialAgentSessionEngineState(), {
    agentSessionId: "session-1",
    needsMessages: false,
    needsState: true,
    type: "session/reconcileRequested",
    workspaceId: "workspace-1"
  }).state;

  assert.equal(selectEngineSessionDetailHydrated(state, "session-1"), false);
  assert.equal(selectEngineSessionDetailLoading(state, "session-1"), false);
});

test("state hydration requires an authoritative lifecycle capability projection", () => {
  let state = rootEngineReducer(createInitialAgentSessionEngineState(), {
    session: normalizeAgentActivitySession({
      activeTurnId: null,
      agentSessionId: "session-1",
      cwd: "/workspace",
      latestTurnInteractions: [],
      pendingInteractions: [],
      provider: "codex",
      title: "Session",
      workspaceId: "workspace-1"
    }),
    type: "session/upserted"
  }).state;
  assert.equal(selectEngineSessionStateHydrated(state, "session-1"), false);

  state = rootEngineReducer(state, {
    session: normalizeAgentActivitySession({
      activeTurnId: null,
      agentSessionId: "session-1",
      cwd: "/workspace",
      latestTurnInteractions: [],
      lifecycleCapabilities: { fork: false, forkThroughTurn: true },
      lifecycleCapabilitiesProjected: true,
      pendingInteractions: [],
      provider: "codex",
      title: "Session",
      workspaceId: "workspace-1"
    }),
    type: "session/upserted"
  }).state;
  assert.equal(selectEngineSessionStateHydrated(state, " session-1 "), true);
});
