import type { Agent } from "@multica/core/types";

/**
 * Mobile-owned mirror of packages/core/agents/runtime-binding.ts.
 * New servers expose `runtime_bound`; older servers only expose a non-empty
 * `runtime_id`. Requiring both available signals fails closed on partial data.
 */
export function isAgentRuntimeBound(
  agent: Pick<Agent, "runtime_id" | "runtime_bound">,
): boolean {
  return (
    agent.runtime_bound !== false &&
    (agent.runtime_id ?? "").trim().length > 0
  );
}
