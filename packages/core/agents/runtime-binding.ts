import type { Agent } from "../types";

/**
 * Treat the additive server signal and the legacy runtime id as one invariant.
 * New servers expose `runtime_bound`; older servers only expose a non-empty
 * `runtime_id`. Requiring both available signals also fails closed if a partial
 * response ever carries contradictory data.
 */
export function isAgentRuntimeBound(
  agent: Pick<Agent, "runtime_id" | "runtime_bound">,
): boolean {
  return (
    agent.runtime_bound !== false &&
    (agent.runtime_id ?? "").trim().length > 0
  );
}
