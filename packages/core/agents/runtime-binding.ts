import type { Agent } from "../types";

/**
 * A personal runtime supplies the current viewer's execution binding even
 * when the shared default is unbound. Reachability is checked separately.
 * Otherwise treat the server signal and legacy default id as one invariant.
 * New servers expose `runtime_bound`; older servers only expose a non-empty
 * `runtime_id`. Requiring both available signals also fails closed if a partial
 * response ever carries contradictory data.
 */
export function isAgentRuntimeBound(
  agent: Pick<Agent, "runtime_id" | "runtime_bound" | "personal_runtime_id">,
): boolean {
  if ((agent.personal_runtime_id ?? "").trim().length > 0) return true;
  return (
    agent.runtime_bound !== false &&
    (agent.runtime_id ?? "").trim().length > 0
  );
}
