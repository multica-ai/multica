import { queryOptions } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import type { GitLabIssue } from "../types/gitlab";

export const gitlabKeys = {
  all: (wsId: string) => ["gitlab", wsId] as const,
  connections: (wsId: string) => [...gitlabKeys.all(wsId), "connections"] as const,
  mergeRequests: (issueId: string) => ["gitlab", "merge-requests", issueId] as const,
  gitlabIssue: (issueId: string) => ["gitlab", "issue", issueId] as const,
};

export const gitlabConnectionsOptions = (wsId: string) =>
  queryOptions({
    queryKey: gitlabKeys.connections(wsId),
    queryFn: () => api.listGitLabConnections(wsId),
    enabled: !!wsId,
  });

export const issueMergeRequestsOptions = (issueId: string) =>
  queryOptions({
    queryKey: gitlabKeys.mergeRequests(issueId),
    queryFn: () => api.listIssueMergeRequests(issueId),
    enabled: !!issueId,
  });

export const issueGitLabIssueOptions = (issueId: string) =>
  queryOptions<GitLabIssue | null>({
    queryKey: gitlabKeys.gitlabIssue(issueId),
    queryFn: async () => {
      try {
        return await api.getGitLabIssue(issueId);
      } catch (e) {
        if (e instanceof ApiError && e.status === 404) return null;
        throw e;
      }
    },
    enabled: !!issueId,
  });
