import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type {
  GitHubMergeMethod,
  IssuePullRequestsResponse,
  MergePullRequestRequest,
  MergePullRequestResponse,
  RerunFailedChecksResponse,
} from "../types";
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

// PR writes run on GitHub as the signed-in user. GitHub's answer lands in the
// card through the snapshot refresh the server queues and its realtime event;
// refetching on settle also covers a missed event.
function useRefetchPullRequests(issueId: string) {
  const qc = useQueryClient();
  return () => qc.invalidateQueries({ queryKey: githubKeys.pullRequests(issueId) });
}

export type PullRequestAction =
  | { kind: "merge"; body: MergePullRequestRequest }
  | { kind: "enable_auto_merge"; method: GitHubMergeMethod }
  | { kind: "disable_auto_merge" }
  | { kind: "update_branch" }
  | { kind: "rerun_failed_checks" }
  | { kind: "ready_for_review" }
  | { kind: "convert_to_draft" }
  | { kind: "approve" }
  | { kind: "request_review"; reviewers: string[] }
  | { kind: "close" };

export type PullRequestActionResult =
  | { kind: "merge"; result: MergePullRequestResponse }
  | { kind: "rerun_failed_checks"; result: RerunFailedChecksResponse }
  | { kind: Exclude<PullRequestAction["kind"], "merge" | "rerun_failed_checks"> };

const SIMPLE_ACTION_PATH = {
  update_branch: "update-branch",
  ready_for_review: "ready-for-review",
  convert_to_draft: "convert-to-draft",
  approve: "approve",
  close: "close",
} as const;

export async function runPullRequestAction(
  issueId: string,
  pullRequestId: string,
  action: PullRequestAction,
): Promise<PullRequestActionResult> {
  switch (action.kind) {
    case "merge":
      return { kind: "merge", result: await api.mergePullRequest(issueId, pullRequestId, action.body) };
    case "rerun_failed_checks":
      return { kind: "rerun_failed_checks", result: await api.rerunFailedPullRequestChecks(issueId, pullRequestId) };
    case "enable_auto_merge":
      await api.setPullRequestAutoMerge(issueId, pullRequestId, action.method);
      return { kind: action.kind };
    case "disable_auto_merge":
      await api.setPullRequestAutoMerge(issueId, pullRequestId, null);
      return { kind: action.kind };
    case "request_review":
      await api.requestPullRequestReview(issueId, pullRequestId, action.reviewers);
      return { kind: action.kind };
    default:
      await api.runPullRequestAction(issueId, pullRequestId, SIMPLE_ACTION_PATH[action.kind]);
      return { kind: action.kind };
  }
}

export function usePullRequestAction(issueId: string, pullRequestId: string) {
  const refetch = useRefetchPullRequests(issueId);
  return useMutation({
    mutationFn: (action: PullRequestAction) => runPullRequestAction(issueId, pullRequestId, action),
    onSettled: refetch,
  });
}

export function useCompleteGitHubAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { code: string; state: string }) => api.completeMyGitHubAccount(body),
    onSuccess: (account) => qc.setQueryData(githubKeys.account(), account),
  });
}

export function useDisconnectGitHubAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.disconnectMyGitHubAccount(),
    onSettled: () => qc.invalidateQueries({ queryKey: githubKeys.account() }),
  });
}
