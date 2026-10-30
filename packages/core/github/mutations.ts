import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { IssuePullRequestsResponse } from "../types";
import { githubKeys } from "./queries";

// Each endpoint answers with the issue's fresh PR list and auto-complete
// decision, so the cache is replaced with the server's answer rather than
// patched optimistically. A status change the action caused (auto-complete)
// arrives through the issue:updated realtime event.
function useWritePullRequests(issueId: string) {
  const qc = useQueryClient();
  return (data: IssuePullRequestsResponse) => {
    qc.setQueryData(githubKeys.pullRequests(issueId), data);
  };
}

export function useLinkIssuePullRequest(issueId: string) {
  const write = useWritePullRequests(issueId);
  return useMutation({
    mutationFn: (body: { url: string } | { pull_request_id: string }) =>
      api.linkIssuePullRequest(issueId, body),
    onSuccess: write,
  });
}

export function useUnlinkIssuePullRequest(issueId: string) {
  const write = useWritePullRequests(issueId);
  return useMutation({
    mutationFn: (pullRequestId: string) => api.unlinkIssuePullRequest(issueId, pullRequestId),
    onSuccess: write,
  });
}

export function useSetIssuePRAutoComplete(issueId: string) {
  const write = useWritePullRequests(issueId);
  return useMutation({
    mutationFn: (disabled: boolean) => api.setIssuePRAutoComplete(issueId, disabled),
    onSuccess: write,
  });
}
