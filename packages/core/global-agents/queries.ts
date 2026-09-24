import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Global agents belong to the signed-in account, not to a workspace, so the
// keys carry no wsId. Workspace agent lists that hold linked copies are
// invalidated separately by the mutations.
export const globalAgentKeys = {
  all: () => ["global-agents"] as const,
  list: () => [...globalAgentKeys.all(), "list"] as const,
  workspaces: (id: string) =>
    [...globalAgentKeys.all(), id, "workspaces"] as const,
};

export function globalAgentListOptions() {
  return queryOptions({
    queryKey: globalAgentKeys.list(),
    queryFn: () => api.listGlobalAgents(),
  });
}

export function globalAgentWorkspacesOptions(id: string) {
  return queryOptions({
    queryKey: globalAgentKeys.workspaces(id),
    queryFn: () => api.listGlobalAgentWorkspaces(id),
    enabled: !!id,
  });
}
