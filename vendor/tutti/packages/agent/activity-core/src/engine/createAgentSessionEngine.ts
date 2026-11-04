import {
  createInitialAgentSessionEngineState,
  rootEngineReducer
} from "./rootReducer.ts";
import type {
  AgentSessionEngine,
  AgentSessionEngineIdentity,
  AgentSessionEngineListener,
  AgentSessionEngineStateBase
} from "./types.ts";

/** Holds what a host has reported about its conversations; sending and activation belong to services over it. */
export function createAgentSessionEngine({
  identity
}: {
  identity: AgentSessionEngineIdentity;
}): AgentSessionEngine {
  if (!identity.workspaceId.trim() || !identity.origin.trim()) {
    throw new Error("agent session engine requires a workspaceId and an origin");
  }
  let state: AgentSessionEngineStateBase = createInitialAgentSessionEngineState();
  const listeners = new Set<AgentSessionEngineListener>();
  return {
    identity: Object.freeze({ ...identity }),
    activateSession: () => false,
    submitPrompt: () => ({ accepted: false, queued: false }),
    dispatch(intent) {
      const next = rootEngineReducer(state, intent).state;
      if (next === state) return;
      state = next;
      for (const listener of listeners) {
        try {
          listener(state);
        } catch (error) {
          console.error("[agent-session-engine]", error);
        }
      }
    },
    getSnapshot: () => state,
    subscribe(listener) {
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    },
    dispose() { listeners.clear(); }
  };
}
