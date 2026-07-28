import type { AgentRuntime } from "../types";

/**
 * The name to show for a runtime (MUL-4217): the user's custom override when
 * set, otherwise the daemon-proposed default. Defends against older backends
 * that omit custom_name and against whitespace-only overrides.
 */
export function runtimeDisplayName(
  runtime: Pick<AgentRuntime, "name" | "custom_name">,
): string {
  const custom = runtime.custom_name?.trim();
  return custom ? custom : runtime.name;
}
