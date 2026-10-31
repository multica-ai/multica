import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const githubKeys = {
  all: (wsId: string) => ["github", wsId] as const,
  installations: (wsId: string) => [...githubKeys.all(wsId), "installations"] as const,
  repositories: (wsId: string, installationId: string) =>
    [...githubKeys.all(wsId), "installations", installationId, "repositories"] as const,
  pullRequests: (issueId: string) => ["github", "pull-requests", issueId] as const,
  mergeOptions: (issueId: string, pullRequestId: string) =>
    ["github", "pull-requests", issueId, pullRequestId, "merge-options"] as const,
  reviewerCandidates: (issueId: string, pullRequestId: string, query: string) =>
    ["github", "pull-requests", issueId, pullRequestId, "reviewer-candidates", query] as const,
  /** The signed-in user's GitHub account; account-scoped, not per workspace. */
  account: () => ["github-account"] as const,
};

export const githubAccountOptions = () =>
  queryOptions({
    queryKey: githubKeys.account(),
    queryFn: () => api.getMyGitHubAccount(),
    // Connecting happens in another tab or the system browser; come back to
    // the app and the button should already know.
    refetchOnWindowFocus: "always",
  });

/** Read fresh each time the dialog opens: a push since then changes the head. */
export const pullRequestMergeOptionsOptions = (issueId: string, pullRequestId: string) =>
  queryOptions({
    queryKey: githubKeys.mergeOptions(issueId, pullRequestId),
    queryFn: () => api.getPullRequestMergeOptions(issueId, pullRequestId),
    staleTime: 0,
    gcTime: 0,
    retry: false,
  });

export const pullRequestReviewerCandidatesOptions = (issueId: string, pullRequestId: string, query: string) =>
  queryOptions({
    queryKey: githubKeys.reviewerCandidates(issueId, pullRequestId, query),
    queryFn: () => api.listPullRequestReviewerCandidates(issueId, pullRequestId, query),
    retry: false,
  });

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
