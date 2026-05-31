import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const adoKeys = {
  all: (wsId: string) => ["azuredevops", wsId] as const,
  installations: (wsId: string) => [...adoKeys.all(wsId), "installations"] as const,
  pullRequests: (issueId: string) => ["azuredevops", "pull-requests", issueId] as const,
};

export const adoInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: adoKeys.installations(wsId),
    queryFn: () => api.listADOInstallations(wsId),
    enabled: !!wsId,
  });

export const issueADOPullRequestsOptions = (issueId: string) =>
  queryOptions({
    queryKey: adoKeys.pullRequests(issueId),
    queryFn: () => api.listIssueADOPullRequests(issueId),
    enabled: !!issueId,
  });
