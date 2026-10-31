// @vitest-environment node
import { describe, expect, it } from "vitest";
import { ApiError } from "../api";
import type { GitHubPullRequest } from "../types";
import {
  classifyGitHubActionError,
  primaryPullRequestAction,
  pullRequestMenuActions,
} from "./pull-request-actions";
import { derivePullRequestVerdict } from "./pull-request-status";

function pr(overrides: Partial<GitHubPullRequest> = {}): GitHubPullRequest {
  return {
    id: "pr-1",
    provider: "github",
    workspace_id: "ws-1",
    repo_owner: "acme",
    repo_name: "app",
    number: 7,
    title: "feat: thing",
    state: "open",
    html_url: "https://github.com/acme/app/pull/7",
    branch: "agent/lambda/thing",
    author_login: "jiayuan",
    author_avatar_url: null,
    merged_at: null,
    closed_at: null,
    pr_created_at: "2026-10-01T00:00:00Z",
    pr_updated_at: "2026-10-01T00:00:00Z",
    snapshot_available: true,
    mergeable: "mergeable",
    merge_state_status: "clean",
    checks_rollup: "success",
    checks_total: 3,
    checks_passed: 3,
    checks_failed: 0,
    checks_running: 0,
    failed_check_names: [],
    ...overrides,
  } as GitHubPullRequest;
}

const failing: Partial<GitHubPullRequest> = { checks_rollup: "failure", checks_failed: 1, checks_passed: 2, failed_check_names: ["lint"] };
const running = { checks_rollup: "pending", checks_running: 2, checks_passed: 1, merge_state_status: "blocked" } as const;
const awaitingReview = { merge_state_status: "blocked", review_decision: "review_required" } as const;

describe("awaiting_review verdict", () => {
  it("names a blocked PR that is waiting for a required review", () => {
    expect(derivePullRequestVerdict(pr(awaitingReview)).kind).toBe("awaiting_review");
  });

  it("keeps other blocked reasons as blocked", () => {
    expect(derivePullRequestVerdict(pr({ merge_state_status: "blocked" })).kind).toBe("blocked");
    expect(derivePullRequestVerdict(pr({ merge_state_status: "blocked", review_decision: "approved" })).kind).toBe("blocked");
  });
});

describe("primaryPullRequestAction", () => {
  it.each([
    ["ready", {}, "merge"],
    ["checks running", running, "enable_auto_merge"],
    ["auto-merge already on", { ...running, auto_merge: { merge_method: "squash", enabled_by: "jiayuan" } }, "disable_auto_merge"],
    ["checks failed", failing, "hand_off_checks"],
    ["conflict", { mergeable: "conflicting", merge_state_status: "dirty" }, "hand_off_conflict"],
    ["behind", { merge_state_status: "behind" }, "update_branch"],
    ["awaiting review", awaitingReview, "approve"],
    ["draft", { state: "draft" }, "ready_for_review"],
    ["merged", { state: "merged" }, null],
    ["closed", { state: "closed" }, null],
    ["blocked for another reason", { merge_state_status: "blocked" }, null],
  ] as const)("%s → %s", (_name, overrides, expected) => {
    expect(primaryPullRequestAction(pr(overrides as Partial<GitHubPullRequest>), "someone-else")).toBe(expected);
  });

  it("asks the PR's author to request a review instead of approving their own PR", () => {
    expect(primaryPullRequestAction(pr(awaitingReview), "JiaYuan")).toBe("request_review");
  });

  it("offers approve when the viewer is not known yet", () => {
    expect(primaryPullRequestAction(pr(awaitingReview), null)).toBe("approve");
  });
});

describe("pullRequestMenuActions", () => {
  it("leaves out what the primary button does", () => {
    expect(pullRequestMenuActions(pr({ merge_state_status: "behind" }), "someone-else")).toEqual([
      "approve",
      "convert_to_draft",
      "copy_branch",
      "close",
    ]);
  });

  it("offers a rerun for failing checks and request review to the author", () => {
    expect(pullRequestMenuActions(pr(failing), "jiayuan")).toEqual([
      "request_review",
      "rerun_failed_checks",
      "convert_to_draft",
      "copy_branch",
      "close",
    ]);
  });

  it("offers a branch update only while the base is ahead", () => {
    const behindAwaitingReview = pr({ ...failing, merge_state_status: "behind" });
    expect(pullRequestMenuActions(behindAwaitingReview, "someone-else")).toContain("update_branch");
    expect(pullRequestMenuActions(pr({ merge_state_status: "clean" }), "someone-else")).not.toContain("update_branch");
  });

  it("keeps only copying the branch once the PR has ended", () => {
    expect(pullRequestMenuActions(pr({ state: "merged" }), "jiayuan")).toEqual(["copy_branch"]);
  });
});

describe("classifyGitHubActionError", () => {
  const apiError = (code: string, message = "GitHub said no") =>
    new ApiError(message, 409, "Conflict", { code, error: message });

  it.each([
    ["github_account_not_connected", "connect"],
    ["github_reauth_required", "reconnect"],
    ["github_app_permission_missing", "app_permission"],
    ["github_head_changed", "head_changed"],
    ["github_cannot_approve_own_pr", "own_pr"],
    ["github_auto_merge_not_allowed", "auto_merge_not_allowed"],
    ["github_rate_limited", "rate_limited"],
    ["github_installation_missing", "installation_missing"],
    ["github_user_auth_not_configured", "not_configured"],
  ])("%s → %s", (code, kind) => {
    expect(classifyGitHubActionError(apiError(code)).kind).toBe(kind);
  });

  it("passes GitHub's own reason through for a blocked merge", () => {
    expect(classifyGitHubActionError(apiError("github_merge_blocked", "Required status check is expected."))).toEqual({
      kind: "github",
      message: "Required status check is expected.",
    });
  });

  it("treats anything else as unknown", () => {
    expect(classifyGitHubActionError(new Error("network")).kind).toBe("unknown");
  });
});
