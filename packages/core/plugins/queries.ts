import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const pluginKeys = {
  all: (wsId: string) => ["workspaces", wsId, "plugins"] as const,
  installed: (wsId: string) => [...pluginKeys.all(wsId), "installed"] as const,
};

export function pluginInstallationsOptions(wsId: string) {
  return queryOptions({
    queryKey: pluginKeys.installed(wsId),
    queryFn: () => api.listPluginInstallations(wsId),
    enabled: wsId.length > 0,
  });
}

/**
 * A hook's recent calls, for the author staring at a failing endpoint.
 *
 * Short-lived in cache: the point of opening it is to see what happened just
 * now, and a stale list is worse than a slow one here.
 */
export function pluginInvocationsOptions(wsId: string, installationId: string) {
  return queryOptions({
    queryKey: [...pluginKeys.all(wsId), installationId, "invocations"] as const,
    queryFn: () => api.listPluginInvocations(wsId, installationId),
    enabled: wsId.length > 0 && installationId.length > 0,
    staleTime: 5_000,
  });
}

/**
 * What an `mcp`-transport hook's server currently offers.
 *
 * Reaches the plugin author's MCP server on every read, which is why it is not
 * prefetched anywhere: it runs when an administrator opens the approval panel
 * and asks. `staleTime` is short because the reason for opening it is to see
 * the current tool list, and drift is exactly what it exists to surface.
 */
export function pluginMCPToolsOptions(wsId: string, installationId: string, hookKey: string) {
  return queryOptions({
    queryKey: [...pluginKeys.all(wsId), installationId, "mcp", hookKey] as const,
    queryFn: () => api.listPluginMCPTools(wsId, installationId, hookKey),
    enabled: wsId.length > 0 && installationId.length > 0 && hookKey.length > 0,
    staleTime: 5_000,
    retry: false,
  });
}
