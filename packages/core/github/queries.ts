import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { IssuePullRequestSummary } from "../types";

export const githubKeys = {
  all: (wsId: string) => ["github", wsId] as const,
  installations: (wsId: string) => [...githubKeys.all(wsId), "installations"] as const,
  repositories: (wsId: string, installationId: string) =>
    [...githubKeys.all(wsId), "installations", installationId, "repositories"] as const,
  pullRequests: (issueId: string) => ["github", "pull-requests", issueId] as const,
  /** Under the same prefix as `pullRequests`, so every `pull_request` realtime
   *  event and link/unlink refreshes the issue views' PR chips too. */
  pullRequestSummaries: (wsId: string) => ["github", "pull-requests", "summaries", wsId] as const,
};

export const githubInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: githubKeys.installations(wsId),
    queryFn: () => api.listGitHubInstallations(wsId),
    enabled: !!wsId,
  });

export const githubInstallationRepositoriesOptions = (
  wsId: string,
  installationId: string,
) =>
  infiniteQueryOptions({
    queryKey: githubKeys.repositories(wsId, installationId),
    queryFn: ({ pageParam }) =>
      api.listGitHubInstallationRepositories(wsId, installationId, {
        page: pageParam,
        per_page: 100,
      }),
    initialPageParam: 1,
    getNextPageParam: (lastPage) => lastPage.next_page ?? undefined,
    enabled: !!wsId && !!installationId,
  });

export type IssuePullRequestSummariesByIssue = Readonly<
  Record<string, readonly IssuePullRequestSummary[]>
>;

/**
 * Every linked PR in the workspace, grouped by issue id (newest first). One
 * workspace-wide read backs the PR chips on board cards, list rows and table
 * cells; enable it only while one of those surfaces shows PRs.
 */
export const issuePullRequestSummariesOptions = (wsId: string) =>
  queryOptions({
    queryKey: githubKeys.pullRequestSummaries(wsId),
    queryFn: async (): Promise<IssuePullRequestSummariesByIssue> => {
      const { pull_requests } = await api.listIssuePullRequestSummaries();
      const byIssue: Record<string, IssuePullRequestSummary[]> = {};
      for (const pr of pull_requests) {
        (byIssue[pr.issue_id] ??= []).push(pr);
      }
      return byIssue;
    },
    enabled: !!wsId,
  });

export const issuePullRequestsOptions = (issueId: string) =>
  queryOptions({
    queryKey: githubKeys.pullRequests(issueId),
    queryFn: () => api.listIssuePullRequests(issueId),
    enabled: !!issueId,
  });
