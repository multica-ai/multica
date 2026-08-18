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
