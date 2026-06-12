import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const gitlabKeys = {
  all: (wsId: string) => ["gitlab", wsId] as const,
  settings: (wsId: string) => [...gitlabKeys.all(wsId), "settings"] as const,
  mergeRequests: (issueId: string) => ["gitlab", "merge-requests", issueId] as const,
};

export const gitlabSettingsOptions = (wsId: string) =>
  queryOptions({
    queryKey: gitlabKeys.settings(wsId),
    queryFn: () => api.getGitlabSettings(wsId),
    enabled: !!wsId,
  });

export const issueMergeRequestsOptions = (issueId: string) =>
  queryOptions({
    queryKey: gitlabKeys.mergeRequests(issueId),
    queryFn: () => api.listIssueMergeRequests(issueId),
    enabled: !!issueId,
  });
