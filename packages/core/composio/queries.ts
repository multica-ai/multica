import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/** Query-key namespace for Composio integration data. */
export const composioKeys = {
  all: ["composio"] as const,
  toolkits: () => [...composioKeys.all, "toolkits"] as const,
  connections: () => [...composioKeys.all, "connections"] as const,
};

/** The project's connectable Composio toolkits (those with an enabled auth
 * config; see MUL-4009). The list changes rarely, so a long staleTime avoids
 * refetching it every time the Settings tab mounts. */
export const composioToolkitsOptions = () =>
  queryOptions({
    queryKey: composioKeys.toolkits(),
    queryFn: () => api.listComposioToolkits(),
    staleTime: 5 * 60 * 1000,
  });

/** The current user's active Composio connections. */
export const composioConnectionsOptions = () =>
  queryOptions({
    queryKey: composioKeys.connections(),
    queryFn: () => api.listComposioConnections(),
  });
