import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const giteaKeys = {
  all: (wsId: string) => ["gitea", wsId] as const,
  connections: (wsId: string) => [...giteaKeys.all(wsId), "connections"] as const,
};

export const giteaConnectionsOptions = (wsId: string) =>
  queryOptions({
    queryKey: giteaKeys.connections(wsId),
    queryFn: () => api.listGiteaConnections(wsId),
    enabled: !!wsId,
  });
