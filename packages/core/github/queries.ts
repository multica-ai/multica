import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const githubKeys = {
  all: (wsId: string) => ["github", wsId] as const,
  installations: (wsId: string) => [...githubKeys.all(wsId), "installations"] as const,
  repositories: (wsId: string, installationId: string) =>
    [...githubKeys.all(wsId), "installations", installationId, "repositories"] as const,
  pullRequests: (issueId: string) => ["github", "pull-requests", issueId] as const,
  /** Under the PR list's prefix, so a `pull_request:` event (new commits)
   *  refreshes an open diff too. */
  pullRequestDiff: (issueId: string, pullRequestId: string) =>
    [...githubKeys.pullRequests(issueId), "diff", pullRequestId] as const,
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

export const issuePullRequestsOptions = (issueId: string) =>
  queryOptions({
    queryKey: githubKeys.pullRequests(issueId),
    queryFn: () => api.listIssuePullRequests(issueId),
    enabled: !!issueId,
  });

// A linked PR's changes (MUL-7651). One attempt: a failure means the GitHub App
// cannot read it, and the viewer falls back to the branch diff at once.
export const issuePullRequestDiffOptions = (issueId: string, pullRequestId: string) =>
  queryOptions({
    queryKey: githubKeys.pullRequestDiff(issueId, pullRequestId),
    queryFn: () => api.getIssuePullRequestDiff(issueId, pullRequestId),
    enabled: !!issueId && !!pullRequestId,
    retry: false,
    gcTime: 60_000,
  });
