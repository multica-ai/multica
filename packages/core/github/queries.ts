import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const githubKeys = {
  all: (wsId: string) => ["github", wsId] as const,
  installations: (wsId: string) => [...githubKeys.all(wsId), "installations"] as const,
  repositories: (wsId: string, installationId: string) =>
    [...githubKeys.all(wsId), "installations", installationId, "repositories"] as const,
  pullRequests: (issueId: string) => ["github", "pull-requests", issueId] as const,
  queuedPullRequests: (wsId: string) =>
    [...githubKeys.all(wsId), "pull-requests", "queued"] as const,
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

// One workspace-wide read backing the merge-queue indicator on every board
// card. Deliberately not per-card: a board can hold hundreds of cards, and the
// queued set is a handful of PRs at most. A queue entry advances on GitHub's
// schedule rather than on any user action here, so this refetches on a timer.
export const queuedPullRequestsOptions = (wsId: string) =>
  queryOptions({
    queryKey: githubKeys.queuedPullRequests(wsId),
    queryFn: () => api.listGitHubQueuedPullRequests(wsId),
    enabled: !!wsId,
    staleTime: 30_000,
    refetchInterval: 60_000,
  });

export const issuePullRequestsOptions = (issueId: string) =>
  queryOptions({
    queryKey: githubKeys.pullRequests(issueId),
    queryFn: () => api.listIssuePullRequests(issueId),
    enabled: !!issueId,
  });
