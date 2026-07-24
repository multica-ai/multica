export type GitHubPullRequestState = "open" | "closed" | "merged" | "draft";

/** Aggregated CI status for a PR's current head SHA, computed server-side from
 * the latest check_suite per app. `null` when no completed suite has been seen
 * yet (e.g. PR just opened, or repository has no CI configured).
 *
 * Legacy compat field kept for backend drift; the current PR card derives CI
 * status from `checks_rollup` + counts instead. */
export type GitHubPullRequestChecksConclusion = "passed" | "failed" | "pending";

/** Raw mirror of GitHub's legacy `mergeable_state`. Superseded by the
 * `mergeable` + `merge_state_status` snapshot pair; kept optional for backend
 * drift only. */
export type GitHubMergeableState = string;

/** GitHub's `mergeable` verdict — answers ONLY "is there a conflict". `unknown`
 * is a normal transient value (GitHub computes it lazily); it must render as
 * neither "conflict" nor "ready". */
export type GitHubPullRequestMergeable = "mergeable" | "conflicting" | "unknown";

/** GitHub's `mergeStateStatus`. "Ready to merge" is asserted ONLY from `clean`
 * (which folds in required checks + branch protection); the other values are
 * surfaced faithfully and never inferred into a ready/mergeable claim. */
export type GitHubPullRequestMergeStateStatus =
  | "clean"
  | "dirty"
  | "blocked"
  | "behind"
  | "unstable"
  | "draft"
  | "has_hooks"
  | "unknown";

/** GitHub's overall CI rollup verdict (`statusCheckRollup.state`). `null`/absent
 * means NO checks have been reported yet — it must never render as passed. */
export type GitHubPullRequestChecksRollup =
  | "success"
  | "failure"
  | "pending"
  | "error"
  | "expected";

export interface GitHubInstallation {
  id: string;
  workspace_id: string;
  /** GitHub's numeric installation id — the management handle used by the
   * connect / disconnect flows. Omitted when the caller cannot manage
   * integrations (see `ListGitHubInstallationsResponse.can_manage`). */
  installation_id?: number;
  account_login: string;
  account_type: "User" | "Organization";
  account_avatar_url: string | null;
  created_at: string;
  /** Display name of the workspace member who connected this installation.
   * Optional because older backends and minimum-visibility deployments may
   * omit it; the UI renders the "connected by" line only when present. */
  connected_by?: string;
}

export interface GitHubPullRequest {
  id: string;
  /** Source provider. Older GitHub-only backends omit it. */
  provider?: "github" | "forgejo" | "gitea" | "gitlab";
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
  /** Conflict verdict from the GitHub API snapshot. Answers ONLY
   * "is there a conflict"; older backends omit it. */
  mergeable?: GitHubPullRequestMergeable | null;
  /** GitHub's `mergeStateStatus` from the snapshot. Source of the "Ready to
   * merge" claim (only when `clean`); older backends omit it. */
  merge_state_status?: GitHubPullRequestMergeStateStatus | null;
  /** GitHub's overall CI rollup verdict from the snapshot. `null` means no
   * checks only when `snapshot_available === true`; absence alone is not a
   * positive or "no checks" verdict. */
  checks_rollup?: GitHubPullRequestChecksRollup | null;
  /** True only when the GitHub API snapshot feature is enabled and the stored
   * snapshot belongs to this PR's current head. False means the CI/merge
   * snapshot region must be hidden; omitted preserves legacy provider output. */
  snapshot_available?: boolean;
  /** Check counts from the snapshot. Older backends omit these; treat absence
   * as 0. `checks_total` is 0 when no checks have been reported. */
  checks_total?: number;
  checks_passed?: number;
  checks_failed?: number;
  checks_running?: number;
  /** Names of the currently failing checks, for the "…failed · a, b" summary.
   * Older backends omit it; treat absence as an empty list. */
  failed_check_names?: string[];
  /** True when the shown snapshot is stale (GitHub outage / revoked key). The
   * card greys out both status elements and shows the snapshot age. */
  snapshot_stale?: boolean;
  /** RFC3339 timestamp of when the snapshot was fetched, for the stale hint. */
  snapshot_fetched_at?: string | null;
  /** Legacy mirror of GitHub's `mergeable_state`. Optional; superseded by
   * `mergeable` + `merge_state_status`. */
  mergeable_state?: GitHubMergeableState | null;
  /** Legacy aggregated CI conclusion. Optional; superseded by `checks_rollup`. */
  checks_conclusion?: GitHubPullRequestChecksConclusion | null;
  /** Legacy pending-suite count. Optional; superseded by `checks_running`. */
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
  /** Whether the caller can connect / disconnect installations. Non-admin
   * members get `false` along with installations that omit `installation_id`.
   * Older backends predating MUL-2413 omit the field; treat absence as
   * `false` for read-only safety. */
  can_manage?: boolean;
}

export interface GitHubConnectResponse {
  /** The GitHub App install URL the browser should open. Empty when `configured` is false. */
  url?: string;
  configured: boolean;
}
