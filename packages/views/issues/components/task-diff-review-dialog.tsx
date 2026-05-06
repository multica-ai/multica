"use client";

import { useEffect, useState } from "react";
import { AlertTriangle, FolderSearch, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import type {
  AgentTask,
  TaskWorktreeMetadata,
} from "@multica/core/types/agent";
import type {
  TaskChangeActions,
  TaskChangeApplyResult,
  TaskChangeInput,
  TaskChangePreview,
} from "./task-change-actions";

interface TaskDiffReviewDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  task: AgentTask;
  worktree: TaskWorktreeMetadata;
  actions: TaskChangeActions;
}

// Dialog flow:
//   1. Pick checkout — calls actions.pickCheckoutDirectory(). User cancel
//      keeps dialog open on the picker step.
//   2. Preview — auto-runs once a checkout is picked. Shows changed/deleted
//      files, the unified diff body (scrollable), and warnings when the
//      target is dirty or the repo URL doesn't match.
//   3. Apply — destructive button. Success toasts and closes; failure shows
//      reason + detail and keeps the dialog open.
//
// All buttons disable while operations are in flight.
export function TaskDiffReviewDialog({
  open,
  onOpenChange,
  task: _task,
  worktree,
  actions,
}: TaskDiffReviewDialogProps) {
  const [target, setTarget] = useState<string | null>(null);
  const [picking, setPicking] = useState(false);
  const [preview, setPreview] = useState<TaskChangePreview | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [applying, setApplying] = useState(false);
  const [applyResult, setApplyResult] = useState<TaskChangeApplyResult | null>(
    null,
  );
  const defaultBranch = worktree.branch_name;
  const [branchName, setBranchName] = useState(defaultBranch);

  const baseRef = worktree.base_ref || worktree.requested_ref || "main";

  // Re-run preview when target changes. If the user picks a different
  // checkout we want a fresh diff, not the stale one.
  useEffect(() => {
    if (!target) return;
    let cancelled = false;
    setPreviewing(true);
    setPreviewError(null);
    setPreview(null);
    actions
      .previewApplyTaskDiff(buildInput(worktree, target, branchName, baseRef))
      .then((res) => {
        if (cancelled) return;
        setPreview(res);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setPreviewError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (!cancelled) setPreviewing(false);
      });
    return () => {
      cancelled = true;
    };
    // We deliberately exclude `branchName` — changing the new branch name
    // doesn't affect the diff, only the eventual apply step.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [target]);

  const handlePick = async () => {
    if (picking) return;
    setPicking(true);
    try {
      const res = await actions.pickCheckoutDirectory();
      if (res.ok && res.path) {
        setTarget(res.path);
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to pick directory");
    } finally {
      setPicking(false);
    }
  };

  const handleApply = async () => {
    if (!target || applying) return;
    setApplying(true);
    setApplyResult(null);
    try {
      const res = await actions.applyTaskDiff(
        buildInput(worktree, target, branchName, baseRef),
      );
      setApplyResult(res);
      if (res.ok) {
        toast.success(
          res.createdBranch
            ? `Applied changes on new branch ${res.branchName}`
            : `Applied changes on ${res.branchName}`,
        );
        onOpenChange(false);
      }
    } catch (err) {
      const detail = err instanceof Error ? err.message : String(err);
      setApplyResult({ ok: false, reason: "git_failure", detail });
    } finally {
      setApplying(false);
    }
  };

  const inFlight = picking || previewing || applying;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[80vh] max-w-2xl flex-col gap-3 sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Review &amp; apply task changes</DialogTitle>
        </DialogHeader>

        <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-hidden text-xs">
          <div className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
            <span className="text-muted-foreground">Worktree</span>
            <span className="break-all font-mono">{worktree.path}</span>
            <span className="text-muted-foreground">Source branch</span>
            <span className="break-all font-mono">{worktree.branch_name}</span>
            <span className="text-muted-foreground">Base ref</span>
            <span className="break-all font-mono">{baseRef}</span>
            <span className="text-muted-foreground">Repo</span>
            <span className="break-all font-mono">{worktree.repo_url}</span>
          </div>

          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={handlePick}
              disabled={picking}
            >
              {picking ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <FolderSearch className="h-3.5 w-3.5" />
              )}
              {target ? "Change checkout..." : "Pick target checkout..."}
            </Button>
            {target && (
              <span className="min-w-0 flex-1 truncate font-mono text-muted-foreground">
                {target}
              </span>
            )}
          </div>

          {target && (
            <div className="flex flex-col gap-1">
              <Label
                htmlFor="task-diff-branch-name"
                className="text-xs text-muted-foreground"
              >
                New branch on target
              </Label>
              <Input
                id="task-diff-branch-name"
                value={branchName}
                onChange={(e) => setBranchName(e.target.value)}
                disabled={applying}
                className="h-7 text-xs"
              />
            </div>
          )}

          {previewing && (
            <div className="flex items-center gap-2 text-muted-foreground">
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
              Computing diff...
            </div>
          )}

          {previewError && (
            <div className="rounded border border-destructive/40 bg-destructive/10 p-2 text-destructive">
              {previewError}
            </div>
          )}

          {preview && (
            <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-hidden">
              {!preview.repoMatches && (
                <Warning>
                  Repo URL on target ({preview.targetRepoURL || "<unknown>"})
                  does not match the task's repo. Apply will be refused.
                </Warning>
              )}
              {!preview.targetClean && (
                <Warning>
                  Target checkout has uncommitted changes. Apply will be refused
                  until the working tree is clean.
                </Warning>
              )}

              <div className="grid grid-cols-2 gap-3 text-xs">
                <FileList
                  label={`Changed (${preview.changedFiles.length})`}
                  files={preview.changedFiles}
                  emptyText="No file changes"
                />
                <FileList
                  label={`Deleted (${preview.deletedFiles.length})`}
                  files={preview.deletedFiles}
                  emptyText="No deletions"
                  destructive
                />
              </div>

              <div className="min-h-0 flex-1 overflow-auto rounded border border-border bg-muted/40 p-2">
                <pre className="whitespace-pre font-mono text-[11px] leading-tight text-foreground/90">
                  {preview.diff || "No diff produced."}
                </pre>
              </div>
            </div>
          )}

          {applyResult && !applyResult.ok && (
            <div className="rounded border border-destructive/40 bg-destructive/10 p-2 text-destructive">
              <div className="font-medium">
                Apply failed: {applyResult.reason.replace(/_/g, " ")}
              </div>
              <div className="mt-1 break-words text-destructive/90">
                {applyResult.detail}
              </div>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={inFlight}
          >
            Cancel
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={handleApply}
            disabled={!preview || applying || inFlight}
          >
            {applying ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : null}
            Apply diff
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function buildInput(
  worktree: TaskWorktreeMetadata,
  target: string,
  newBranchName: string,
  baseRef: string,
): TaskChangeInput {
  return {
    taskWorktreePath: worktree.path,
    taskBranchName: worktree.branch_name,
    baseRef,
    taskRepoURL: worktree.repo_url,
    targetCheckout: target,
    newBranchName,
  };
}

function Warning({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex items-start gap-2 rounded border border-warning/40 bg-warning/10 p-2 text-warning">
      <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
      <span>{children}</span>
    </div>
  );
}

function FileList({
  label,
  files,
  emptyText,
  destructive = false,
}: {
  label: string;
  files: string[];
  emptyText: string;
  destructive?: boolean;
}) {
  return (
    <div className="flex min-h-0 flex-col gap-1">
      <span className="text-muted-foreground">{label}</span>
      {files.length === 0 ? (
        <span className="text-muted-foreground/70">{emptyText}</span>
      ) : (
        <ul
          className={`max-h-32 overflow-y-auto rounded border border-border/60 bg-background p-1 font-mono text-[11px] ${
            destructive ? "text-destructive" : ""
          }`}
        >
          {files.map((f) => (
            <li key={f} className="truncate">
              {f}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
