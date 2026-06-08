"use client";

import { memo, useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Clock } from "lucide-react";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { taskMessagesOptions } from "@multica/core/chat/queries";
import type { AgentTask } from "@multica/core/types";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { formatDuration } from "../../agents/components/agent-activity-hover-content";
import { ActiveTaskRow } from "./execution-log-section";
import { RunningStat } from "./running-stat";
import { useT } from "../../i18n";

// Per-issue "is an agent working on this right now?" chip for the issue
// detail header. Lives in the header (not the scrollable body) so the live
// signal stays in one fixed place and never competes with future sticky
// banners in the content column. Replaces the in-body sticky live card.
//
// Derives state from the workspace-wide agent task snapshot filtered by
// issue id — the same single source of truth that powers the board-card /
// list-row IssueAgentActivityIndicator, so the chip is always consistent
// with those surfaces and costs zero extra network.
//
// Collapsed display — the avatar stack carries how many agents, the text
// always carries one elapsed time, so a multi-agent chip reads "N heads +
// how long" rather than a redundant count next to countable heads:
//   - running     → avatar(s) + blue elapsed of the longest-running task
//   - queued only → half-opacity avatar(s) + clock + muted longest wait
//
// Click opens a Popover listing every active task with the SAME row as the
// right-panel ExecutionLogSection (ActiveTaskRow) — trigger text + status,
// with Logs/Stop revealed on row hover. The popover is `keepMounted` so the
// row's internal confirm dialog survives the popover closing on Stop click.

interface IssueAgentHeaderChipProps {
  issueId: string;
}

export const IssueAgentHeaderChip = memo(function IssueAgentHeaderChip({
  issueId,
}: IssueAgentHeaderChipProps) {
  const wsId = useWorkspaceId();
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));

  const { running, queued } = useMemo(() => {
    const running: AgentTask[] = [];
    const queued: AgentTask[] = [];
    for (const task of snapshot) {
      if (task.issue_id !== issueId) continue;
      if (task.status === "running") running.push(task);
      else if (
        task.status === "queued" ||
        task.status === "dispatched" ||
        // Daemon-parked on a busy local_directory — still active, just
        // waiting on a path lock. Belongs in the live chip, not dropped.
        task.status === "waiting_local_directory"
      )
        queued.push(task);
      // Terminal statuses are the execution log's story, not the live chip's.
    }
    return { running, queued };
  }, [snapshot, issueId]);

  // No active work → render nothing (and crucially, no `now` ticker). The
  // 1s elapsed tick only runs while the ActiveChip is mounted.
  if (running.length === 0 && queued.length === 0) return null;

  return <ActiveChip issueId={issueId} running={running} queued={queued} />;
});

interface ActiveChipProps {
  issueId: string;
  running: AgentTask[];
  queued: AgentTask[];
}

function ActiveChip({ issueId, running, queued }: ActiveChipProps) {
  const { t } = useT("issues");

  // Tick once per second so the collapsed single-agent elapsed stays live.
  // Only mounted while there's active work, so it costs nothing at rest.
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  const activeTasks = [...running, ...queued];
  const agentIds = [...new Set(activeTasks.map((task) => task.agent_id))];
  const anyRunning = running.length > 0;
  const isSingle = agentIds.length === 1;

  // Single-agent event count comes from the shared per-task message cache —
  // the same entry the popover row and Logs read, kept live by
  // useRealtimeSync. Only the single-running case shows a number, so we only
  // query that one task; multi shows "N working".
  const singleRunningTaskId =
    isSingle && anyRunning ? (running[0]?.id ?? "") : "";
  const { data: singleMsgs } = useQuery({
    ...taskMessagesOptions(singleRunningTaskId),
    enabled: singleRunningTaskId !== "",
  });

  // One elapsed time = how long work has been going on this issue. When
  // anything is running, anchor on the longest-running task (earliest start =
  // the "is something stuck?" signal); when all queued, the longest wait.
  // The relevant bucket is always non-empty (ActiveChip only mounts with ≥1
  // active task), so the reduce has a safe seed.
  const anchorOf = (task: AgentTask) =>
    task.status === "running"
      ? (task.started_at ?? task.dispatched_at ?? task.created_at)
      : task.created_at;
  const bucket = anyRunning ? running : queued;
  const leadFrom = bucket
    .map(anchorOf)
    .reduce((a, b) => (new Date(b).getTime() < new Date(a).getTime() ? b : a));
  const elapsed = formatDuration(leadFrom, now);

  return (
    <div className="flex items-center gap-1">
      <Popover>
        <PopoverTrigger
          render={
            <button
              type="button"
              aria-label={t(($) => $.agent_activity.hover_header, {
                count: activeTasks.length,
              })}
              className="flex h-7 items-center gap-1.5 rounded-md px-1.5 text-muted-foreground outline-none transition-colors hover:bg-accent/60 focus-visible:ring-2 focus-visible:ring-ring"
            />
          }
        >
          <AgentAvatarStack
            agentIds={agentIds}
            size={18}
            max={3}
            opacity={anyRunning ? "full" : "half"}
          />
          {!isSingle ? (
            // Multiple agents → "N working" (matches the workspace chip);
            // per-agent time/events live in the popover rows below.
            <span
              className={`text-xs ${anyRunning ? "text-info" : "text-muted-foreground"}`}
            >
              {agentIds.length} {t(($) => $.agent_activity.chip_label)}
            </span>
          ) : anyRunning ? (
            // Single running → events (primary), elapsed in muted parens.
            <RunningStat eventCount={singleMsgs?.length ?? 0} elapsed={elapsed} />
          ) : (
            // Single queued/parked → clock + wait time.
            <span className="flex items-center gap-1 text-xs text-muted-foreground">
              <Clock className="h-3 w-3 shrink-0" />
              <span className="tabular-nums">{elapsed}</span>
            </span>
          )}
        </PopoverTrigger>
        <PopoverContent align="end" keepMounted className="w-80">
          <div className="text-xs font-medium text-muted-foreground">
            {t(($) => $.agent_activity.hover_header, {
              count: activeTasks.length,
            })}
          </div>
          <div className="flex flex-col gap-0.5">
            {activeTasks.map((task) => (
              <ActiveTaskRow key={task.id} task={task} issueId={issueId} />
            ))}
          </div>
        </PopoverContent>
      </Popover>
      {/* Separator from the action buttons — the chip is a status segment,
          not another button, so a hairline keeps the two groups legible. */}
      <span className="h-4 w-px bg-border" aria-hidden="true" />
    </div>
  );
}
