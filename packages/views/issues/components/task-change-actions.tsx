"use client";

import { useState } from "react";
import { FolderOpen, GitPullRequest } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { toast } from "sonner";
import type {
  AgentTask,
  TaskWorktreeMetadata,
} from "@multica/core/types/agent";
import { TaskDiffReviewDialog } from "./task-diff-review-dialog";

// ---------------------------------------------------------------------------
// Cross-platform contract
//
// `packages/views/` cannot import from `apps/desktop/`, but it still needs to
// drive the diff-preview / diff-apply flow that lives behind the desktop
// preload bridge. The adapter type below mirrors `window.taskChangeAPI`'s
// surface 1:1 — same method names, same JSON shapes — so the desktop side can
// pass the bridge through directly while the views package stays platform-
// neutral. Web doesn't supply an adapter; the apply UI is hidden at the
// hosting site (ExecutionLogSection) when `actions` is `undefined`.
//
// Field names + JSON shapes are kept in sync with
// `apps/desktop/src/shared/task-change-types.ts`. If those drift, both
// sides break in lockstep.
// ---------------------------------------------------------------------------

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

export type TaskChangeActions = {
  pickCheckoutDirectory: () => Promise<{ ok: boolean; path?: string }>;
  previewApplyTaskDiff: (input: TaskChangeInput) => Promise<TaskChangePreview>;
  applyTaskDiff: (input: TaskChangeInput) => Promise<TaskChangeApplyResult>;
  openPath: (target: string) => Promise<{ ok: boolean; error?: string }>;
};

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

interface TaskChangeActionsRowProps {
  task: AgentTask;
  worktree: TaskWorktreeMetadata;
  actions: TaskChangeActions;
}

/**
 * Two-button affordance attached to a completed task row in the execution
 * log. "Open worktree" reveals the agent's checkout in the OS file
 * manager; "Review & apply" opens the diff dialog so the user can move
 * the changes onto their own checkout.
 */
export function TaskChangeActionsRow({
  task,
  worktree,
  actions,
}: TaskChangeActionsRowProps) {
  const [reviewOpen, setReviewOpen] = useState(false);
  const [opening, setOpening] = useState(false);

  if (task.status !== "completed") return null;

  const handleOpenWorktree = async () => {
    if (opening) return;
    setOpening(true);
    try {
      const res = await actions.openPath(worktree.path);
      if (!res.ok) {
        toast.error(res.error ?? "Failed to open worktree");
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to open worktree");
    } finally {
      setOpening(false);
    }
  };

  return (
    <div className="ml-7 flex items-center gap-1 pb-1.5 pt-0.5">
      <span className="mr-1 truncate font-mono text-[11px] text-muted-foreground">
        {worktree.branch_name}
      </span>
      <Tooltip>
        <TooltipTrigger
          render={
            <button
              type="button"
              onClick={handleOpenWorktree}
              disabled={opening}
              aria-label="Open worktree"
            />
          }
          className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground disabled:opacity-50"
        >
          <FolderOpen className="h-3 w-3" />
          <span>Open worktree</span>
        </TooltipTrigger>
        <TooltipContent>{worktree.path}</TooltipContent>
      </Tooltip>
      <button
        type="button"
        onClick={() => setReviewOpen(true)}
        aria-label="Review & apply"
        className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground"
      >
        <GitPullRequest className="h-3 w-3" />
        <span>Review &amp; apply</span>
      </button>
      {reviewOpen && (
        <TaskDiffReviewDialog
          open={reviewOpen}
          onOpenChange={setReviewOpen}
          task={task}
          worktree={worktree}
          actions={actions}
        />
      )}
    </div>
  );
}
