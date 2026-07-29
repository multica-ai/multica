"use client";

// FIR-4073 — the two ways forward from a failed run: continue the same
// conversation, or start over with a blank one. Owned here rather than inside
// the alert row because the same pair is now offered in the run log modal, and
// two copies of the rerun call would drift the moment the API changes.
//
// Deliberately free of any @multica/views import: the run log modal
// (agent-transcript-dialog) imports this file directly, and an edge back into
// @multica/views closes the import cycle that blanks that dialog — the same
// trap already documented on its RunFailureCard import.

import { useState } from "react";
import { Loader2, Play, RotateCcw } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import { cn } from "@multica/ui/lib/utils";
import { ISSUE_FAILED_RUNS_KEY, useIssueFailedRuns } from "../dead-failed-runs";

export type RerunMode = "resume" | "fresh";

/**
 * Starts the run again and refreshes every surface that reads run state: the
 * failed-run alert (which clears itself, because the new run is now the issue's
 * newest) and the Runs list in the right panel.
 */
export function useRerunFailedRun(issueId: string) {
  const [busy, setBusy] = useState<RerunMode | null>(null);
  const queryClient = useQueryClient();

  async function rerun(taskId: string, mode: RerunMode) {
    if (busy) return;
    setBusy(mode);
    try {
      await api.rerunIssue(issueId, taskId, mode === "resume");
      toast.success(
        mode === "resume" ? "Continuing where it stopped" : "Started over",
      );
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ISSUE_FAILED_RUNS_KEY(issueId) }),
        queryClient.invalidateQueries({ queryKey: issueKeys.tasks(issueId) }),
      ]);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Could not start the run");
    } finally {
      setBusy(null);
    }
  }

  return { rerun, busy };
}

interface RunRetryActionsProps {
  issueId: string;
  taskId: string;
  /** False when the daemon cannot pick the conversation back up. */
  resumePossible: boolean;
  /** Plain-words explanation shown as the disabled Resume button's title. */
  blockedReason?: string;
  className?: string;
}

/**
 * Resume + Start over, sized to sit in a one-line row (the failed-run alert)
 * or in a dialog header. Resume is disabled — not hidden — when the daemon
 * cannot reattach, so the user learns why instead of silently getting a blank
 * session.
 */
export function RunRetryActions({
  issueId,
  taskId,
  resumePossible,
  blockedReason,
  className,
}: RunRetryActionsProps) {
  const { rerun, busy } = useRerunFailedRun(issueId);

  return (
    <div className={cn("flex items-center gap-1", className)}>
      <button
        type="button"
        disabled={!resumePossible || busy !== null}
        onClick={() => void rerun(taskId, "resume")}
        title={
          resumePossible
            ? "Continue the same conversation"
            : (blockedReason ?? "This run cannot be continued")
        }
        className="inline-flex items-center gap-1 rounded border border-border bg-background px-2 py-0.5 text-xs font-medium transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
      >
        {busy === "resume" ? (
          <Loader2 className="h-3 w-3 animate-spin" />
        ) : (
          <Play className="h-3 w-3" />
        )}
        Resume
      </button>
      <button
        type="button"
        disabled={busy !== null}
        onClick={() => void rerun(taskId, "fresh")}
        title="Start over with a blank conversation"
        className="inline-flex items-center gap-1 rounded border border-border bg-background px-2 py-0.5 text-xs font-medium transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-50"
      >
        {busy === "fresh" ? (
          <Loader2 className="h-3 w-3 animate-spin" />
        ) : (
          <RotateCcw className="h-3 w-3" />
        )}
        Start over
      </button>
    </div>
  );
}

/**
 * The same pair inside the run log modal, where the caller only has the task.
 * Renders nothing for a run that did not fail — there is nothing to retry.
 */
export function TranscriptRunRetryActions({
  task,
}: {
  task: { id: string; issue_id: string; status: string };
}) {
  const failed = task.status === "failed" && !!task.issue_id;
  const runs = useIssueFailedRuns(task.issue_id, failed);
  if (!failed) return null;

  const run = runs.find((r) => r.task_id === task.id);
  return (
    <RunRetryActions
      issueId={task.issue_id}
      taskId={task.id}
      // A failure older than the alert window is no longer in this list, so we
      // cannot prove resume is impossible — offer it rather than dead-end it.
      resumePossible={run ? run.resume_possible === true : true}
      blockedReason={run?.blocked_reason}
    />
  );
}
