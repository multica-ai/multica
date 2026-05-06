// task-change-types.ts
//
// Pure data types describing the task-change preview/apply contract between
// main and renderer. Lives in `shared/` so both sides — and the preload
// bridge type declarations — can import from here without pulling in any
// main-process dependencies (`child_process`, `fs`, `electron`). The
// authoritative implementation lives in
// `apps/desktop/src/main/task-change-service.ts`, which re-exports these
// types for ergonomic use inside the service.

export type TaskChangeInput = {
  taskWorktreePath: string;
  taskBranchName: string;
  /** Branch from the worktree to diff against — usually the resolved base ref. */
  baseRef: string;
  taskRepoURL: string;
  /** User-side directory we apply into. */
  targetCheckout: string;
  /** Branch name to create on target if not present. */
  newBranchName: string;
};

export type TaskChangePreview = {
  changedFiles: string[];
  deletedFiles: string[];
  /** Full unified diff body — for the review dialog. */
  diff: string;
  /** True when no uncommitted changes exist on target. */
  targetClean: boolean;
  /** Repo URL on target (from `git remote get-url origin`). */
  targetRepoURL: string;
  /** Repo URL match status. */
  repoMatches: boolean;
};

export type TaskChangeApplyResult =
  | { ok: true; createdBranch: boolean; branchName: string }
  | {
      ok: false;
      reason:
        | "target_dirty"
        | "repo_mismatch"
        | "conflict"
        | "no_changes"
        | "git_failure";
      detail: string;
    };
