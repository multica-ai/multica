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

/**
 * A runtime label that always surfaces the provider family, even when a custom
 * alias is set (#5260). The daemon bakes the provider into `name`
 * ("Codex (host)"), but a user alias replaces that whole string via
 * runtimeDisplayName and hides which CLI actually backs the runtime. When an
 * alias is present we re-attach the provider in parentheses; without one `name`
 * already carries it, so we return it unchanged to avoid a duplicated provider
 * ("Codex (host) (codex)").
 */
export function runtimeDisplayLabel(
  runtime: Pick<AgentRuntime, "name" | "custom_name" | "provider">,
): string {
  const display = runtimeDisplayName(runtime);
  const hasCustom = !!runtime.custom_name?.trim();
  const provider = runtime.provider?.trim();
  if (!hasCustom || !provider) return display;
  return `${display} (${providerLabel(provider)})`;
}

/** Title-case a provider slug ("codex" -> "Codex") for display. */
function providerLabel(provider: string): string {
  return provider.charAt(0).toUpperCase() + provider.slice(1);
}
