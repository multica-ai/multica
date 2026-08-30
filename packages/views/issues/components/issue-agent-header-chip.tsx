"use client";

import { memo, useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, History } from "lucide-react";
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
import { TranscriptButton } from "../../common/task-transcript";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import {
  ActiveTaskRow,
  isActiveTask,
  PastTaskRow,
  sortPastTasks,
} from "./execution-log-section";
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
  const { t } = useT("issues");
  // Same query options as ExecutionLogSection so both observe one cache entry.
  const { data: tasks = [] } = useQuery({
    queryKey: issueKeys.tasks(issueId),
    queryFn: () => api.listTasksByIssue(issueId),
    staleTime: 30_000,
    refetchOnWindowFocus: true,
  });

  const { running, queued, past } = useMemo(() => {
    const running: AgentTask[] = [];
    const queued: AgentTask[] = [];
    // The list is already issue-scoped by the endpoint, so only the status
    // split matters here.
    for (const task of tasks) {
      if (task.status === "running") running.push(task);
      else if (isActiveTask(task)) queued.push(task);
    }
    return { running, queued, past: sortPastTasks(tasks) };
  }, [tasks]);

  const [openedTranscriptTaskSnapshot, setOpenedTranscriptTaskSnapshot] =
    useState<AgentTask | null>(null);
  const openedTranscriptTask = openedTranscriptTaskSnapshot
    ? tasks.find((task) => task.id === openedTranscriptTaskSnapshot.id) ??
      openedTranscriptTaskSnapshot
    : null;

  const hasActive = running.length > 0 || queued.length > 0;

  // No active work and no historical context → render nothing. A terminal run
  // is still a useful header affordance because it keeps the run list nearby.
  if (!hasActive && past.length === 0 && !openedTranscriptTask) return null;

  return (
    <>
      {hasActive || past.length > 0 ? (
        <ActiveChip
          issueId={issueId}
          running={running}
          queued={queued}
          past={past}
          onTranscriptOpenChange={(task, open) => {
            setOpenedTranscriptTaskSnapshot(open ? task : null);
          }}
        />
      ) : null}
      {openedTranscriptTask ? (
        <TranscriptButton
          task={openedTranscriptTask}
          agentName=""
          isLive={openedTranscriptTask.status === "running"}
          title={t(($) => $.execution_log.transcript_tooltip)}
          renderButton={false}
          open
          onOpenChange={(open) => {
            if (!open) setOpenedTranscriptTaskSnapshot(null);
          }}
        />
      ) : null}
    </>
  );
});

interface ActiveChipProps {
  issueId: string;
  running: AgentTask[];
  queued: AgentTask[];
  past: AgentTask[];
  onTranscriptOpenChange: (task: AgentTask, open: boolean) => void;
}

function ActiveChip({
  issueId,
  running,
  queued,
  past,
  onTranscriptOpenChange,
}: ActiveChipProps) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();

  const activeTasks = [...running, ...queued];
  const hasActive = activeTasks.length > 0;
  const agentIds = [...new Set(activeTasks.map((task) => task.agent_id))];
  const anyRunning = running.length > 0;
  const isSingle = agentIds.length === 1;
  const historyLabel = t(($) => $.execution_log.history);
  // Copy follows the actual state. With no active task the same control is a
  // calm historical-runs entry, rather than pretending a terminal task is
  // still working.
  const label = !hasActive
    ? historyLabel
    : isSingle
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

  // The history list opens immediately when it is the only thing the chip can
  // show. When live work appears, put history back behind its sibling row so
  // the active signal remains the first thing in the popover.
  const [showPast, setShowPast] = useState(() => !hasActive);
  useEffect(() => {
    setShowPast(!hasActive);
  }, [hasActive]);

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
          {hasActive ? (
            <AgentAvatarStack
              agentIds={agentIds}
              size="sm"
              max={3}
              opacity={anyRunning ? "full" : "half"}
            />
          ) : (
            <History className="size-3.5 shrink-0" aria-hidden="true" />
          )}
          <span
            className={`min-w-0 truncate text-caption ${anyRunning ? "text-info" : "text-muted-foreground"}`}
          >
            {label}
          </span>
        </PopoverTrigger>
        <PopoverContent align="end" keepMounted className="w-80">
          <div className="text-caption font-medium text-muted-foreground">
            {hasActive
              ? t(
                  ($) =>
                    anyRunning
                      ? $.agent_activity.hover_header
                      : $.agent_activity.hover_header_queued,
                  { count: agentIds.length },
                )
              : historyLabel}
          </div>
          <div className="flex flex-col gap-0.5">
            {activeTasks.map((task) => (
              <ActiveTaskRow
                key={task.id}
                task={task}
                issueId={issueId}
                onTranscriptOpenChange={(open) => {
                  onTranscriptOpenChange(task, open);
                }}
              />
            ))}
            {past.length > 0 && (
              <>
                {activeTasks.length > 0 && <div className="my-1.5 border-t border-border/60" />}
                <button
                  type="button"
                  aria-expanded={showPast}
                  aria-label={historyLabel}
                  onClick={() => setShowPast((open) => !open)}
                  className="flex w-full items-center gap-1 rounded px-1 py-1 text-caption text-muted-foreground transition-colors hover:bg-accent/40 hover:text-foreground"
                >
                  <ChevronRight
                    className={`!size-3 shrink-0 stroke-[2.5] transition-transform ${showPast ? "rotate-90" : ""}`}
                  />
                  <History className="size-3.5 shrink-0" aria-hidden="true" />
                  <span className="min-w-0 truncate">{historyLabel}</span>
                  <span className="ml-auto shrink-0 tabular-nums">{past.length}</span>
                </button>
                {showPast && (
                  <div
                    className="mt-0.5 space-y-0.5 border-l border-border/60 pl-3"
                    role="group"
                    aria-label={historyLabel}
                  >
                    {past.map((task) => (
                      <PastTaskRow key={task.id} task={task} issueId={issueId} />
                    ))}
                  </div>
                )}
              </>
            )}
          </div>
        </PopoverContent>
      </Popover>
      {/* Separator from the action buttons — the chip is a status segment,
          not another button, so a hairline keeps the two groups legible. */}
      <span className="h-4 w-px bg-border" aria-hidden="true" />
    </div>
  );
}
