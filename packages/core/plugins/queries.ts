import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const pluginKeys = {
  all: (wsId: string) => ["workspaces", wsId, "plugins"] as const,
  catalog: (wsId: string) => [...pluginKeys.all(wsId), "catalog"] as const,
  installed: (wsId: string) => [...pluginKeys.all(wsId), "installed"] as const,
};

export function pluginCatalogOptions(wsId: string) {
  return queryOptions({
    queryKey: pluginKeys.catalog(wsId),
    queryFn: () => api.listPluginCatalog(wsId),
    enabled: wsId.length > 0,
  });
}

export function pluginInstallationsOptions(wsId: string) {
  return queryOptions({
    queryKey: pluginKeys.installed(wsId),
    queryFn: () => api.listPluginInstallations(wsId),
    enabled: wsId.length > 0,
  });
}
