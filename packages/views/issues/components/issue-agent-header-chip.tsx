"use client";

import { memo, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { useActorName } from "@multica/core/workspace/hooks";
import { cn } from "@multica/ui/lib/utils";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import type { AgentTask } from "@multica/core/types";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { ActiveTaskRow } from "./execution-log-section";
import { useT } from "../../i18n";

// Per-issue "is an agent working on this right now?" chip for the issue
// detail header. Lives in the header (not the scrollable body) so the live
// signal stays in one fixed place and never competes with future sticky
// banners in the content column. Replaces the in-body sticky live card.
//
// Reads the same per-issue task list as the right-panel Execution log
// (shared `issueKeys.tasks(issueId)` cache), so the header chip and the log
// always agree on what is active. Both surfaces derive from one query, which
// removes the race where the old workspace-wide agent-task-snapshot refetched
// slower than this per-issue list and left the chip lagging behind the log's
// "agent is working".
//
// Collapsed display stays intentionally shallow:
//   - one running agent  → avatar + "{name} is working"
//   - multiple running   → avatar stack + "N agents working"
//   - queued only        → "{name} is queued" / "N agents queued",
//                          half-opacity avatars / muted text (no beam)
//
// Hovering the chip opens a compact Popover card with the same active rows as
// the right panel (click / keyboard still toggle it for touch and a11y). Those
// rows show necessary status/time and task entry actions, but do not render
// event counts or prefetch task messages for a count.

interface IssueAgentHeaderChipProps {
  issueId: string;
}

export const IssueAgentHeaderChip = memo(function IssueAgentHeaderChip({
  issueId,
}: IssueAgentHeaderChipProps) {
  // Same query options as ExecutionLogSection so both observe one cache entry.
  const { data: tasks = [] } = useQuery({
    queryKey: issueKeys.tasks(issueId),
    queryFn: () => api.listTasksByIssue(issueId),
    staleTime: 30_000,
    refetchOnWindowFocus: true,
  });

  const { running, queued } = useMemo(() => {
    const running: AgentTask[] = [];
    const queued: AgentTask[] = [];
    // The list is already issue-scoped by the endpoint, so only the status
    // split matters here.
    for (const task of tasks) {
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
  }, [tasks]);

  // No active work → render nothing.
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
  const { getActorName } = useActorName();

  const activeTasks = [...running, ...queued];
  const agentIds = [...new Set(activeTasks.map((task) => task.agent_id))];
  const anyRunning = running.length > 0;
  const isSingle = agentIds.length === 1;
  // Copy must follow the actual state: "is working" only when something is
  // truly running. With nothing running (queued / dispatched / parked on a
  // path lock) the chip reads "is queued" so a not-yet-started agent isn't
  // mislabelled as working.
  const label = isSingle
    ? t(
        ($) =>
          anyRunning ? $.agent_live.is_working : $.agent_live.is_queued,
        { name: getActorName("agent", agentIds[0] ?? "") },
      )
    : t(
        ($) =>
          anyRunning
            ? $.agent_activity.hover_header
            : $.agent_activity.hover_header_queued,
        { count: agentIds.length },
      );

  return (
    <div className="flex items-center gap-1">
      <Popover>
        {/* Hover opens the card so the live activity reads as a glanceable
            status surface, not a click target. In Base UI the hover config
            lives on the Trigger (a popover can have multiple triggers), not
            the Root. The trigger stays a real button, so click and keyboard
            (Enter/Space) still toggle it for touch and a11y. A short open
            delay avoids flicker when the pointer merely passes over the chip;
            the close delay keeps it open while the pointer travels across the
            hover bridge into the interactive rows. */}
        <PopoverTrigger
          openOnHover
          delay={150}
          closeDelay={200}
          render={
            <button
              type="button"
              aria-label={label}
              // While an agent is actively running, the chip wears the
              // brand border beam — a highlight sweeping around its rounded
              // edge — so a triggered run is unmistakably "alive" in the
              // header. Queued-only state stays calm (no beam) to reserve the
              // motion for work that is genuinely in flight.
              className={cn(
                "flex h-7 max-w-[11rem] items-center gap-1.5 rounded-md px-1.5 text-muted-foreground outline-none transition-colors hover:bg-accent/60 focus-visible:ring-2 focus-visible:ring-ring",
                anyRunning && "border-beam bg-brand/5",
              )}
            />
          }
        >
          <AgentAvatarStack
            agentIds={agentIds}
            size={18}
            max={3}
            opacity={anyRunning ? "full" : "half"}
          />
          <span
            className={`min-w-0 truncate text-xs ${anyRunning ? "text-info" : "text-muted-foreground"}`}
          >
            {label}
          </span>
        </PopoverTrigger>
        <PopoverContent align="end" keepMounted className="w-80">
          <div className="text-xs font-medium text-muted-foreground">
            {t(
              ($) =>
                anyRunning
                  ? $.agent_activity.hover_header
                  : $.agent_activity.hover_header_queued,
              { count: agentIds.length },
            )}
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
