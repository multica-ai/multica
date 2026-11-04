import { createInitialPromptQueueState } from "./promptQueue.initialState.ts";
import {
  createInitialSessionLifecycleState,
  sessionLifecycleReducer
} from "./sessionLifecycle.reducer.ts";
import type { AgentSessionEngineStateBase, EngineIntent } from "./types.ts";

export function createInitialAgentSessionEngineState(): AgentSessionEngineStateBase {
  return {
    pendingIntents: { activationsByRequestId: {}, inactiveSessionIds: {}, submitsByClientSubmitId: {} },
    promptQueue: createInitialPromptQueueState(),
    sessionReconcile: { nextCommandSequence: 1, recordsBySessionId: {} },
    sessionLifecycle: createInitialSessionLifecycleState()
  };
}

export function rootEngineReducer(
  state: AgentSessionEngineStateBase,
  intent: EngineIntent
): { state: AgentSessionEngineStateBase } {
  const sessionLifecycle = sessionLifecycleReducer(state.sessionLifecycle, intent);
  return {
    state:
      sessionLifecycle === state.sessionLifecycle
        ? state
        : { ...state, sessionLifecycle }
  };
}
