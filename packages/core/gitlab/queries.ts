import { queryOptions, useQuery } from "@tanstack/react-query";
import { api } from "../api";
import type { GitlabConnection } from "./types";

export const gitlabKeys = {
  all: (wsId: string) => ["gitlab", "workspace", wsId] as const,
  connection: (wsId: string) => [...gitlabKeys.all(wsId), "connection"] as const,
};

export function workspaceGitlabConnectionOptions(wsId: string) {
  return queryOptions<GitlabConnection>({
    queryKey: gitlabKeys.connection(wsId),
    queryFn: () => api.getWorkspaceGitlabConnection(wsId),
    // 404 means "not connected" — the consumer decides how to render it; no retry loop.
    retry: false,
  });
}

export function useWorkspaceGitlabConnection(wsId: string) {
  return useQuery(workspaceGitlabConnectionOptions(wsId));
}
