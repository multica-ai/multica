import type { Agent } from "../types";
import {
  agentEffectiveRuntimeId,
  type EffectiveRuntimeAgent,
} from "./effective-runtime";

/**
 * Resolve runtime availability through the same effective-runtime contract
 * used by routing and presence. Smart agents (`share_runtime === false`) bind
 * through the current viewer's runtime config; everyone else keeps the
 * additive `runtime_bound` plus legacy `runtime_id` compatibility check.
 *
 * Treat the additive server signal and the legacy runtime id as one invariant.
 * New servers expose `runtime_bound`; older servers only expose a non-empty
 * `runtime_id`. Requiring both available signals also fails closed if a partial
 * response ever carries contradictory data.
 */
export function isAgentRuntimeBound(
  agent: EffectiveRuntimeAgent & Pick<Agent, "runtime_bound">,
): boolean {
  const runtimeId = agentEffectiveRuntimeId(agent);
  if (
    agent.share_runtime === false &&
    agent.runtime_config_summary?.current
  ) {
    return runtimeId !== null;
  }
  return (
    agent.runtime_bound !== false &&
    (runtimeId ?? "").trim().length > 0
  );
}
