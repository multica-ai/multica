import type { RootAgentSessionEngineState } from "./rootReducer.types.ts";
import type { AgentSessionEngineState } from "./types.ts";

/**
 * Projects the host-observable Engine state. Keeping this as a real object
 * projection, rather than a type assertion, prevents private reducer ledgers
 * from leaking through getSnapshot() or subscription callbacks at runtime.
 */
export function projectPublicAgentSessionEngineState(
  state: RootAgentSessionEngineState,
  previous?: AgentSessionEngineState
): AgentSessionEngineState {
  const projected: AgentSessionEngineState = {
    engineRuntime: state.engineRuntime,
    pendingIntents: state.pendingIntents,
    promptQueue: state.promptQueue,
    sessionLifecycle: state.sessionLifecycle,
    sessionReconcile: state.sessionReconcile
  };
  if (
    previous &&
    Object.keys(projected).every(
      (key) =>
        previous[key as keyof AgentSessionEngineState] ===
        projected[key as keyof AgentSessionEngineState]
    )
  ) {
    return previous;
  }
  return projected;
}
