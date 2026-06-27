import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const forgejoKeys = {
  all: (wsId: string) => ["forgejo", wsId] as const,
  connections: (wsId: string) => [...forgejoKeys.all(wsId), "connections"] as const,
};

export const forgejoConnectionsOptions = (wsId: string) =>
  queryOptions({
    queryKey: forgejoKeys.connections(wsId),
    queryFn: () => api.listForgejoConnections(wsId),
    enabled: !!wsId,
  });
