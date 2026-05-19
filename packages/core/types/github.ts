export type GitHubPullRequestState = "open" | "closed" | "merged" | "draft";

/** Aggregated CI status for a PR's current head SHA, computed server-side from
 * the latest check_suite per app. `null` when no completed suite has been seen
 * yet (e.g. PR just opened, or repository has no CI configured). */
export type GitHubPullRequestChecksConclusion = "passed" | "failed" | "pending";

/** Raw mirror of GitHub's `mergeable_state`. The UI only surfaces `clean` and
 * `dirty`; the other values (`blocked`, `behind`, `unstable`, `unknown`,
 * `has_hooks`, `draft`) round-trip but render as unknown to avoid asserting
 * "conflicts" for blocking reasons that aren't actual conflicts. */
export type GitHubMergeableState = string;

export interface GitHubInstallation {
  id: string;
  workspace_id: string;
  installation_id: number;
  account_login: string;
  account_type: "User" | "Organization";
  account_avatar_url: string | null;
  created_at: string;
}

export interface GitHubPullRequest {
  id: string;
  workspace_id: string;
  repo_owner: string;
  repo_name: string;
  number: number;
  title: string;
  state: GitHubPullRequestState;
  html_url: string;
  branch: string | null;
  author_login: string | null;
  author_avatar_url: string | null;
  merged_at: string | null;
  closed_at: string | null;
  pr_created_at: string;
  pr_updated_at: string;
  /** Optional; older backends omit this field. */
  mergeable_state?: GitHubMergeableState | null;
  /** Optional; older backends omit this field. */
  checks_conclusion?: GitHubPullRequestChecksConclusion | null;
  /** Per-suite counts that feed the segmented progress bar. Older backends
   * omit these; treat absence as 0 (the card renders only when sum > 0). */
  checks_passed?: number;
  checks_failed?: number;
  checks_pending?: number;
  /** Diff stats from GitHub's `pull_request` payload. Older backends omit
   * these fields; we treat 0/0/0 as "unknown" and hide the stats row. */
  additions?: number;
  deletions?: number;
  changed_files?: number;
}

export interface ListGitHubInstallationsResponse {
  installations: GitHubInstallation[];
  /** Whether the deployment has GitHub App credentials configured. When false, the Connect button is hidden / disabled. */
  configured: boolean;
}

export interface GitHubConnectResponse {
  /** The GitHub App install URL the browser should open. Empty when `configured` is false. */
  url?: string;
  configured: boolean;
}
