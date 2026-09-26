/**
 * A run's code change (MUL-7651): what one run changed in one repository,
 * captured by the daemon when the run ended.
 *
 * `scope: "run"` is the run's own change. `scope: "branch"` is the delivered
 * branch's whole line of work as of that run, used for the issue-wide view
 * when no pull request can supply it; the daemon leaves it out when it would
 * equal the run row.
 */
export type CodeChangeScope = "run" | "branch";

/** Unknown server values degrade to "modified" at the display layer. */
export type CodeChangeFileStatus =
  | "added"
  | "modified"
  | "deleted"
  | "renamed"
  | "copied"
  | "type_changed";

export interface CodeChangeFile {
  path: string;
  old_path?: string;
  status: CodeChangeFileStatus;
  additions: number;
  deletions: number;
  binary?: boolean;
}

export interface TaskCodeChange {
  id: string;
  issue_id: string;
  task_id: string;
  agent_id: string;
  scope: CodeChangeScope;
  source: "local_worktree" | "repo_checkout";
  /** Stable across runs on the same repository. */
  repo_key: string;
  /** owner/name for a hosted remote, the directory name otherwise. */
  repo_label: string;
  /** origin's URL without credentials, or "". */
  repo_url: string;
  branch: string;
  /** The ref a checkout's branch is measured against ("origin/main"), or "". */
  base_ref: string;
  base_commit: string;
  head_commit: string;
  file_count: number;
  additions: number;
  deletions: number;
  files_truncated: boolean;
  patch_available: boolean;
  patch_size: number;
  /** Why there is no patch: "too_large" | "unavailable". */
  patch_omitted: string | null;
  created_at: string;
}

export interface TaskCodeChangeDetail extends TaskCodeChange {
  files: CodeChangeFile[];
  patch: string | null;
}

export interface ListTaskCodeChangesResponse {
  code_changes: TaskCodeChange[];
}

/** A linked GitHub pull request's changes, read through the GitHub App. */
export interface PullRequestDiff {
  pull_request_id: string;
  head_sha: string;
  file_count: number;
  additions: number;
  deletions: number;
  files: CodeChangeFile[];
  files_truncated: boolean;
  patch: string | null;
  patch_omitted: string | null;
}
