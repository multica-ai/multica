import type { AgentRuntime } from "../types";
import { providerSupportsMcpConfig } from "./mcp-support";

/**
 * Determines whether a detail tab should be visible for the given runtime.
 *
 * - Built-in runtimes (external !== true) use the hard-coded
 *   `providerSupportsMcpConfig` set and default everything else to true.
 * - External runtimes (from runtime.json) are capabilities-driven: the
 *   manifest's `capabilities` flags gate which tabs appear. A capability
 *   that is absent or `false` hides the matching tab; the user cannot
 *   configure a feature their runtime doesn't support.
 *
 * The fallback when `runtime` is null (loading / not found) is to show
 * all tabs so the UI doesn't flicker-hide things the user expects to see
 * during the initial fetch.
 */
export function isTabVisibleForRuntime(
  tab: string,
  runtime: AgentRuntime | null,
): boolean {
  if (!runtime) return true;

  // Built-in runtimes: legacy behaviour.
  if (!runtime.external) {
    switch (tab) {
      case "mcp_config":
        return providerSupportsMcpConfig(runtime.provider);
      default:
        return true;
    }
  }

  // External runtime — capabilities-driven.
  const caps = runtime.capabilities;
  if (!caps) {
    // No capabilities block at all → show everything (safest default
    // for a manifest that just declares the minimum fields).
    return true;
  }

  switch (tab) {
    case "mcp_config":
      return caps.mcp_config === true;
    case "custom_args":
      return caps.custom_args === true;
    case "skills":
      return caps.local_skills === true;
    case "environment":
      // Environment tab is always visible — every runtime can use env.
      return true;
    default:
      return true;
  }
}
