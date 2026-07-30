"use client";

// FIR-4073 — a run that stopped because its machine is paused, as ONE grey row
// in the issue's shared alert bar. Same chrome as FailedRunActivityRow, but the
// colour is muted instead of destructive, because nothing here is broken: the
// pause suspended the run and the unpause sweeper picks it back up on its own.
//
// No Resume / Start over. Retrying against a paused machine would fail the same
// way, so the only useful action is reading what happened — the row exists to
// say "we are waiting for the machine, and here is until when". That is exactly
// the sentence the auto-pause used to write as an issue comment.

import { useQuery } from "@tanstack/react-query";
import { PauseCircle } from "lucide-react";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { formatRunIdentity } from "../run-identity";
import type { DeadFailedRun } from "../dead-failed-runs";
import { RunSummary } from "./run-summary";
import { useRunLog } from "./use-run-log";

/**
 * "Resumes 14:05" when we know when, "resumes when the machine is unpaused"
 * when the auto-pause circuit breaker gave up and a human has to act.
 *
 * A time in the past means the sweeper has not swept yet; "any moment now" is
 * truthful there and avoids showing a stale clock time as if it were future.
 */
export function pausedRunLabel(unpauseAt: string | undefined, now: Date): string {
  if (!unpauseAt) return "Paused — needs a human to resume";
  const at = new Date(unpauseAt);
  if (Number.isNaN(at.getTime())) return "Paused";
  if (at.getTime() <= now.getTime()) return "Paused — resuming any moment";
  const time = at.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  return `Paused — resumes ${time}`;
}

export function PausedRunActivityRow({
  issueId,
  run,
}: {
  issueId: string;
  run: DeadFailedRun;
}) {
  const { getAgentName } = useActorName();

  // Same cache the right-panel Runs list fills — see FailedRunActivityRow.
  const { data: tasks = [] } = useQuery({
    queryKey: issueKeys.tasks(issueId),
    queryFn: () => api.listTasksByIssue(issueId),
    staleTime: 30_000,
  });
  const task = tasks.find((t) => t.id === run.task_id);
  const agentName = run.agent_id ? getAgentName(run.agent_id) : "";
  const runLog = useRunLog(task, agentName);
  const now = new Date();
  const identity = formatRunIdentity(
    {
      agentName,
      completedAt: run.completed_at,
      attempt: run.attempt,
      maxAttempts: run.max_attempts,
      runtimeName: run.runtime_name,
    },
    now,
  );

  return (
    <div
      className="flex items-center gap-2 overflow-hidden px-3 py-2 text-muted-foreground"
      data-testid="paused-run-row"
    >
      <PauseCircle aria-hidden="true" className="h-3.5 w-3.5 shrink-0" />
      <RunSummary
        headline={pausedRunLabel(run.unpause_at, now)}
        identity={identity}
        onOpen={runLog.available ? runLog.open : undefined}
        loading={runLog.loading}
      />
      {runLog.dialog}
    </div>
  );
}
