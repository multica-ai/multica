import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const jiraKeys = {
  all: (wsId: string) => ["jira", wsId] as const,
  connections: (wsId: string) => [...jiraKeys.all(wsId), "connections"] as const,
};

export const jiraConnectionsOptions = (wsId: string) =>
  queryOptions({
    queryKey: jiraKeys.connections(wsId),
    queryFn: () => api.listJiraConnections(wsId),
    enabled: !!wsId,
  });
