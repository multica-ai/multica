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
