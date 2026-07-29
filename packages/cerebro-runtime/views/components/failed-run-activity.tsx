"use client";

// FIR-4073 — a failed run as ONE row in the issue's shared alert bar, instead
// of a second red banner sitting under it. Same chrome as the wakeup row
// (WakeupActivityRow), so "an agent is working", "a run is scheduled" and "the
// last run failed" all read as rows of one bar and collapse together once
// there is more than one, whatever their type.
//
// The row is deliberately thin: it says what broke and offers the three things
// you can do about it — open the run log, continue, or start over. The "why"
// (failure reason + last step + output tail) now lives in the run log modal,
// which is also where the same Resume / Start over pair is offered.

import { useQuery } from "@tanstack/react-query";
import { AlertCircle } from "lucide-react";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { TranscriptButton } from "@multica/views/common/task-transcript";
import { resolveFailureReasonLabel } from "../failure-reason-label";
import type { DeadFailedRun } from "../dead-failed-runs";
import { RunRetryActions } from "./run-retry-actions";

export function FailedRunActivityRow({
  issueId,
  run,
}: {
  issueId: string;
  run: DeadFailedRun;
}) {
  const { getAgentName } = useActorName();

  // Same cache the right-panel Runs list fills, so opening the log from the
  // alert costs nothing extra on an issue the user has already opened.
  const { data: tasks = [] } = useQuery({
    queryKey: issueKeys.tasks(issueId),
    queryFn: () => api.listTasksByIssue(issueId),
    staleTime: 30_000,
  });
  const task = tasks.find((t) => t.id === run.task_id);
  const label = resolveFailureReasonLabel(run.failure_reason) ?? "Run failed";

  return (
    <div
      className="flex items-center gap-2 px-3 py-2 text-muted-foreground"
      data-testid="failed-run-row"
    >
      <AlertCircle aria-hidden="true" className="h-3.5 w-3.5 shrink-0 text-destructive" />
      <div className="flex min-w-0 items-center gap-1.5 text-xs">
        <span className="shrink-0 font-medium text-destructive">{label}</span>
        {run.runtime_name ? (
          <span className="hidden min-w-0 truncate sm:inline">{run.runtime_name}</span>
        ) : null}
      </div>
      <div className="ml-auto flex shrink-0 items-center gap-1">
        {task ? (
          <TranscriptButton
            task={task}
            agentName={run.agent_id ? getAgentName(run.agent_id) : ""}
            title="Open run log"
          />
        ) : null}
        <RunRetryActions
          issueId={issueId}
          taskId={run.task_id}
          resumePossible={run.resume_possible === true}
          blockedReason={run.blocked_reason}
        />
      </div>
    </div>
  );
}
