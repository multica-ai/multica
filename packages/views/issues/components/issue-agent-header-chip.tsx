"use client";

import { memo, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { cn } from "@multica/ui/lib/utils";
import { BorderBeam } from "@multica/ui/components/common/border-beam";
import { issueTasksOptions } from "@multica/core/issues/queries";
import type { AgentTask } from "@multica/core/types";
import { TranscriptButton } from "../../common/task-transcript";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { ActiveTaskRow } from "./execution-log-section";
import { useT } from "../../i18n";
import { compareActiveIssueTasks } from "./active-task-order";
import { groupExecutionTasksByActor } from "../surface/execution-actors";

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
// event counts or prefetch task messages for a count. Clicking a row's trigger
// closes the card and takes the reader to that run's block in the timeline.

interface IssueAgentHeaderChipProps {
  issueId: string;
  onJumpToRun?: (taskId: string) => void;
}

export const IssueAgentHeaderChip = memo(function IssueAgentHeaderChip({
  issueId,
  onJumpToRun,
}: IssueAgentHeaderChipProps) {
  const { t } = useT("issues");
  // Same query options as ExecutionLogSection so both observe one cache entry.
  const { data: tasks = [] } = useQuery(issueTasksOptions(issueId));

  const { running, queued } = useMemo(() => {
    const running: AgentTask[] = [];
    const queued: AgentTask[] = [];
    // The endpoint returns history newest first. Active work instead uses
    // execution/queue order, shared with the right-panel log.
    for (const task of tasks.toSorted(compareActiveIssueTasks)) {
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

  // The row owns the trigger, this level owns the dialog — so the row's open
  // signal has to carry how it was requested, or the dialog would never learn
  // it and would always drop a keyboard reader's focus on close.
  const [openedTranscript, setOpenedTranscript] = useState<
    { task: AgentTask; fromKeyboard: boolean } | null
  >(null);
  const openedTranscriptTask = openedTranscript
    ? tasks.find((task) => task.id === openedTranscript.task.id) ??
      openedTranscript.task
    : null;

  // No active work → render nothing.
  if (running.length === 0 && queued.length === 0 && !openedTranscriptTask) return null;

  return (
    <>
      {running.length > 0 || queued.length > 0 ? (
        <ActiveChip
          issueId={issueId}
          running={running}
          queued={queued}
          onJumpToRun={onJumpToRun}
          onTranscriptOpenChange={(task, open, fromKeyboard) => {
            setOpenedTranscript(open ? { task, fromKeyboard } : null);
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
          finalFocus={openedTranscript?.fromKeyboard === true}
          onOpenChange={(open) => {
            if (!open) setOpenedTranscript(null);
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
  onJumpToRun?: (taskId: string) => void;
  onTranscriptOpenChange: (
    task: AgentTask,
    open: boolean,
    fromKeyboard: boolean,
  ) => void;
}

function ActiveChip({
  issueId,
  running,
  queued,
  onJumpToRun,
  onTranscriptOpenChange,
}: ActiveChipProps) {
  const { t } = useT("issues");
  const { getActorName, getActorInitials, getActorAvatarUrl, hasActor } =
    useActorName();
  const isKnownSquad = useMemo(
    () => (hasActor ? (id: string) => hasActor("squad", id) !== false : null),
    [hasActor],
  );
  const [open, setOpen] = useState(false);

  const activeTasks = [...running, ...queued];
  const agentIds = [...new Set(activeTasks.map((task) => task.agent_id))];
  const groupedActors = useMemo(
    () => groupExecutionTasksByActor(activeTasks, isKnownSquad),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [running, queued, isKnownSquad],
  );
  const actors = groupedActors.map(({ type, id }) => ({ type, id }));
  const hasSquadGroup = groupedActors.some((group) => group.type === "squad");
  const anyRunning = running.length > 0;
  const isSingle = groupedActors.length === 1;
  const singleActor = groupedActors[0];
  // Copy must follow the actual state: "is working" only when something is
  // truly running. With nothing running (queued / dispatched / parked on a
  // path lock) the chip reads "is queued" so a not-yet-started agent or squad
  // isn't mislabelled as working.
  const label = isSingle && singleActor
    ? t(
        ($) =>
          anyRunning ? $.agent_live.is_working : $.agent_live.is_queued,
        { name: getActorName(singleActor.type, singleActor.id) },
      )
    : t(
        ($) =>
          anyRunning
            ? $.agent_activity.hover_header
            : $.agent_activity.hover_header_queued,
        { count: groupedActors.length },
      );

  const renderTaskRow = (task: AgentTask) => (
    <ActiveTaskRow
      key={task.id}
      task={task}
      issueId={issueId}
      onJump={
        onJumpToRun
          ? () => {
              setOpen(false);
              onJumpToRun(task.id);
            }
          : undefined
      }
      onTranscriptOpenChange={(isOpen, fromKeyboard) => {
        onTranscriptOpenChange(task, isOpen, fromKeyboard === true);
      }}
    />
  );

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        openOnHover
        delay={150}
        closeDelay={200}
        render={
          <button
            type="button"
            aria-label={label}
            className={cn(
              "flex h-9 min-w-9 max-w-[11rem] items-center justify-center gap-1.5 rounded-md px-2 text-muted-foreground outline-none transition-colors hover:bg-accent/60 focus-visible:ring-2 focus-visible:ring-ring md:h-7 md:min-w-0 md:justify-start md:px-1.5",
              anyRunning && "relative bg-brand/5",
            )}
          />
        }
      >
        <AgentAvatarStack
          agentIds={agentIds}
          actors={actors}
          size="sm"
          max={3}
          opacity={anyRunning ? "full" : "half"}
        />
        <span
          className={cn(
            "hidden min-w-0 truncate text-caption md:inline",
            anyRunning ? "text-info" : "text-muted-foreground",
          )}
        >
          {label}
        </span>
        {anyRunning && <BorderBeam />}
      </PopoverTrigger>
      <PopoverContent align="end" keepMounted className="w-80">
        <div className="text-caption font-medium text-muted-foreground">
          {hasSquadGroup
            ? t(($) => $.agent_activity.hover_header_tasks, {
                count: activeTasks.length,
              })
            : t(
                ($) =>
                  anyRunning
                    ? $.agent_activity.hover_header
                    : $.agent_activity.hover_header_queued,
                { count: agentIds.length },
              )}
        </div>
        {hasSquadGroup ? (
          <div className="mt-1.5 flex flex-col gap-2">
            {groupedActors.map((group) =>
              group.type === "squad" ? (
                <div
                  key={`squad:${group.id}`}
                  className="overflow-hidden rounded-lg border border-border/80 bg-surface"
                >
                  <div className="flex items-center gap-2 border-b border-border/70 bg-muted/40 px-2.5 py-1.5">
                    <ActorAvatarBase
                      name={getActorName("squad", group.id)}
                      initials={getActorInitials("squad", group.id)}
                      avatarUrl={getActorAvatarUrl("squad", group.id)}
                      isSquad
                      size="sm"
                    />
                    <span className="min-w-0 flex-1 truncate text-caption font-semibold text-foreground">
                      {getActorName("squad", group.id)}
                    </span>
                    <span className="shrink-0 text-micro tabular-nums text-muted-foreground">
                      {t(($) => $.agent_activity.tasks_count, {
                        count: group.tasks.length,
                      })}
                    </span>
                  </div>
                  <div className="flex flex-col gap-0.5 p-1">
                    {group.tasks.map(renderTaskRow)}
                  </div>
                </div>
              ) : (
                <div key={`agent:${group.id}`} className="flex flex-col gap-0.5">
                  {group.tasks.map(renderTaskRow)}
                </div>
              ),
            )}
          </div>
        ) : (
          <div className="flex flex-col gap-0.5">
            {activeTasks.map(renderTaskRow)}
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}
