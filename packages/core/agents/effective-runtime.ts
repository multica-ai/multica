import type { Agent } from "../types";

// Resolves the runtime an agent effectively runs on for the CURRENT viewer.
//
// Smart runtime (`share_runtime === false`): the agent runs on each caller's
// own runtime config, so presence and list surfaces must read
// `runtime_config_summary.current.runtime_id` — the current user's resolved
// config — instead of the template's recommended runtime (`agent.runtime_id`).
// A null current runtime id means the user has no usable runtime configured,
// which must read as "no runtime" (offline), not as the recommended one.
//
// The input is the minimal shape the resolver needs rather than a full Agent:
// daemon-version gates (chat project context, handoff note, quick create)
// resolve runtimes off list rows and picker selections, and keeping the
// parameter structural lets those call sites stay narrow.
export type EffectiveRuntimeAgent = Pick<
  Agent,
  "runtime_id" | "share_runtime" | "runtime_config_summary"
>;

function normalizeRuntimeId(runtimeId: string | null | undefined): string | null {
  const normalized = runtimeId?.trim();
  return normalized ? normalized : null;
}

export function agentEffectiveRuntimeId(
  agent: EffectiveRuntimeAgent,
): string | null {
  if (agent.share_runtime === false) {
    return normalizeRuntimeId(agent.runtime_config_summary?.current.runtime_id);
  }
  return normalizeRuntimeId(agent.runtime_id);
}
