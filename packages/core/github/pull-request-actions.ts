import { errorCode } from "../api";
import type { GitHubPullRequest } from "../types";
import { derivePullRequestVerdict } from "./pull-request-status";

// What a PR row offers: one primary action that removes whatever stands
// between the PR and a merge, and the rest in the row menu. Problems that need
// code changes go to the issue's agent instead of being fixed by hand.

export type PullRequestPrimaryAction =
  | "merge"
  | "enable_auto_merge"
  | "disable_auto_merge"
  | "hand_off_checks"
  | "hand_off_conflict"
  | "update_branch"
  | "approve"
  | "request_review"
  | "ready_for_review";

export type PullRequestMenuAction =
  | "approve"
  | "request_review"
  | "update_branch"
  | "rerun_failed_checks"
  | "disable_auto_merge"
  | "convert_to_draft"
  | "copy_branch"
  | "close";

/** GitHub refuses a review from the PR's author, so they request one instead. */
export function viewerAuthoredPullRequest(pr: Pick<GitHubPullRequest, "author_login">, viewerLogin: string | null): boolean {
  return !!viewerLogin && !!pr.author_login && pr.author_login.toLowerCase() === viewerLogin.toLowerCase();
}

export function primaryPullRequestAction(
  pr: GitHubPullRequest,
  viewerLogin: string | null,
): PullRequestPrimaryAction | null {
  if (pr.state === "merged" || pr.state === "closed") return null;
  if (pr.state === "draft") return "ready_for_review";
  if (pr.auto_merge) return "disable_auto_merge";
  const verdict = derivePullRequestVerdict(pr);
  switch (verdict.kind) {
    case "ready":
    case "passed":
    case "no_checks":
      return "merge";
    case "running":
      return "enable_auto_merge";
    case "failed":
      return "hand_off_checks";
    case "conflicting":
      return "hand_off_conflict";
    case "behind":
      return "update_branch";
    case "awaiting_review":
      return viewerAuthoredPullRequest(pr, viewerLogin) ? "request_review" : "approve";
    default:
      return null;
  }
}

/** Menu entries in display order, minus whatever the primary button already does. */
export function pullRequestMenuActions(
  pr: GitHubPullRequest,
  viewerLogin: string | null,
): PullRequestMenuAction[] {
  if (pr.state === "merged" || pr.state === "closed") return pr.branch ? ["copy_branch"] : [];
  const primary = primaryPullRequestAction(pr, viewerLogin);
  const verdict = derivePullRequestVerdict(pr);
  const actions: PullRequestMenuAction[] = [];
  if (pr.state === "open") {
    actions.push(viewerAuthoredPullRequest(pr, viewerLogin) ? "request_review" : "approve");
  }
  // GitHub only offers the update while the base has moved ahead.
  if (pr.merge_state_status === "behind") actions.push("update_branch");
  if (verdict.kind === "failed") actions.push("rerun_failed_checks");
  if (pr.auto_merge) actions.push("disable_auto_merge");
  if (pr.state === "open") actions.push("convert_to_draft");
  if (pr.branch) actions.push("copy_branch");
  actions.push("close");
  return actions.filter((action) => action !== primary);
}

/** How the UI answers a failed PR action, from the server's error code. */
export type GitHubActionFailure =
  | { kind: "not_configured" }
  | { kind: "connect" }
  | { kind: "reconnect" }
  | { kind: "app_permission" }
  | { kind: "head_changed" }
  | { kind: "own_pr" }
  | { kind: "auto_merge_not_allowed" }
  | { kind: "rate_limited" }
  | { kind: "installation_missing" }
  | { kind: "github"; message: string }
  | { kind: "unknown" };

export function classifyGitHubActionError(err: unknown): GitHubActionFailure {
  switch (errorCode(err)) {
    case "github_user_auth_not_configured":
      return { kind: "not_configured" };
    case "github_account_not_connected":
      return { kind: "connect" };
    case "github_reauth_required":
      return { kind: "reconnect" };
    case "github_app_permission_missing":
      return { kind: "app_permission" };
    case "github_head_changed":
      return { kind: "head_changed" };
    case "github_cannot_approve_own_pr":
      return { kind: "own_pr" };
    case "github_auto_merge_not_allowed":
      return { kind: "auto_merge_not_allowed" };
    case "github_rate_limited":
      return { kind: "rate_limited" };
    case "github_installation_missing":
      return { kind: "installation_missing" };
    case "github_merge_blocked":
    case "github_forbidden":
    case "github_request_failed":
    case "pull_request_provider_unsupported":
      return err instanceof Error && err.message ? { kind: "github", message: err.message } : { kind: "unknown" };
    default:
      return { kind: "unknown" };
  }
}
