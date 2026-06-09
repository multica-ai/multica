"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Ban, CheckCircle2, ChevronRight, Loader2, RotateCcw, Square, XCircle } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import type { AgentTask, TaskFailureReason } from "@multica/core/types";
import { useTimeAgo } from "../../i18n";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "../../common/actor-avatar";
import { formatDuration } from "../../agents/components/agent-activity-hover-content";
import { TranscriptButton } from "../../common/task-transcript";
import { failureReasonLabel } from "../../agents/components/tabs/task-failure";
import { useT } from "../../i18n";
import { TerminateTaskConfirmDialog } from "./terminate-task-confirm-dialog";

// Right-panel section that lists every agent run for this issue. Active
// runs sit at the top (always visible when present); past runs (terminal
// statuses) collapse behind a "Show past runs (N)" toggle.
//
// Replaces:
//   - the click-to-expand timeline that used to live inside the in-body live
//     card (the live "agent is working" signal now lives in the header via
//     IssueAgentHeaderChip)
//   - the standalone <TaskRunHistory> below the main content
//
// Row layout — simple left/right flex:
//   1. Agent avatar (no status dot — agent availability is not the
//      story here; the row's right column carries the task status)
//   2. Trigger description flexes and truncates
//   3. Status is a normal shrink-0 right column; on hover it is replaced
//      in place by the action buttons (status is removed, not covered).
//      Left text keeps flex-1 so the row never shows a mid-row gap. Do
//      not use masks/padding gymnastics here.
//
// One query (`listTasksByIssue`) drives both buckets — the back-end
// returns every status, the front-end filters into active vs past on the
// client. WS task:* events for this issue trigger an invalidate so the
// list updates without polling.

interface ExecutionLogSectionProps {
  issueId: string;
}

// Past-runs sort priority: failed first (needs attention), then
// cancelled (procedural noise), then completed (the boring 'done'
// case sinks to the bottom). Within each group, newest first.
const PAST_STATUS_RANK: Record<string, number> = {
  failed: 0,
  cancelled: 1,
  completed: 2,
};

export function ExecutionLogSection({ issueId }: ExecutionLogSectionProps) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(true);
  const [showPast, setShowPast] = useState(false);

  // Cache key registered in `issueKeys.tasks` (packages/core/issues/queries.ts)
  // so the global useRealtimeSync `task:` prefix path invalidates it via
  // a `["issues", "tasks"]` prefix-match — no local WS subscriptions
  // needed, and the cache stays fresh even when this component isn't
  // mounted (e.g. user cancels from agent-side, then navigates here).
  const { data: tasks = [] } = useQuery({
    queryKey: issueKeys.tasks(issueId),
    queryFn: () => api.listTasksByIssue(issueId),
    staleTime: 30_000,
    refetchOnWindowFocus: true,
  });

  const activeTasks = useMemo(
    () =>
      tasks.filter(
        (t) =>
          t.status === "queued" ||
          t.status === "dispatched" ||
          // Daemon-parked task on a busy local_directory — still active
          // (waiting on a path lock), not terminal. Surfacing it here is
          // what tells the user the agent is alive and will resume.
          t.status === "waiting_local_directory" ||
          t.status === "running",
      ),
    [tasks],
  );

  const pastTasks = useMemo(() => {
    const past = tasks.filter(
      (t) =>
        t.status === "completed" ||
        t.status === "failed" ||
        t.status === "cancelled",
    );
    // Stable sort: failed first, cancelled second, completed last.
    // Within group: newest completed_at first (fall back to created_at
    // for malformed rows missing completed_at).
    return past.toSorted((a, b) => {
      const rankDiff =
        (PAST_STATUS_RANK[a.status] ?? 99) -
        (PAST_STATUS_RANK[b.status] ?? 99);
      if (rankDiff !== 0) return rankDiff;
      const at = a.completed_at ?? a.created_at;
      const bt = b.completed_at ?? b.created_at;
      return new Date(bt).getTime() - new Date(at).getTime();
    });
  }, [tasks]);

  if (activeTasks.length === 0 && pastTasks.length === 0) return null;

  return (
    <div>
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${
          open ? "" : "text-muted-foreground hover:text-foreground"
        }`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.execution_log.section)}
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${
            open ? "rotate-90" : ""
          }`}
        />
        {activeTasks.length > 0 && (
          <span className="ml-auto inline-flex items-center gap-1 text-info">
            <span className="h-1.5 w-1.5 rounded-full bg-info animate-pulse" />
            <span className="font-mono tabular-nums">{activeTasks.length}</span>
          </span>
        )}
      </button>
      {open && (
        <div className="space-y-0.5 pl-2">
          {activeTasks.map((task) => (
            <ActiveTaskRow key={task.id} task={task} issueId={issueId} />
          ))}

          {pastTasks.length > 0 && (
            <>
              {activeTasks.length > 0 && (
                <div className="my-1.5 border-t border-border/60" />
              )}
              <button
                type="button"
                onClick={() => setShowPast(!showPast)}
                className="flex w-full items-center gap-1 rounded px-1 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent/40 hover:text-foreground"
              >
                <ChevronRight
                  className={`!size-3 shrink-0 stroke-[2.5] transition-transform ${
                    showPast ? "rotate-90" : ""
                  }`}
                />
                {showPast
                  ? t(($) => $.execution_log.hide_past, { count: pastTasks.length })
                  : t(($) => $.execution_log.show_past, { count: pastTasks.length })}
              </button>
              {showPast && (
                <div className="mt-0.5 space-y-0.5">
                  {pastTasks.map((task) => (
                    <PastRow key={task.id} task={task} issueId={issueId} />
                  ))}
                </div>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}

// ─── Trigger description ────────────────────────────────────────────────────

// Primary source: the canonical snapshot taken at task creation time
// (comment text / autopilot title). Survives source edits/deletes and
// is information-dense — far better than a structural label.
//
// Retry tasks inherit the parent's trigger_summary on the DB side (so the
// snapshot survives across attempts), but a row that just shows the
// inherited summary is indistinguishable from its parent. We prepend
// "Retry #N" when parent_task_id is set so retries are scannable as
// retries even when their summary is inherited.
//
// Fallback chain for legacy tasks created before the snapshot field
// shipped, OR for sources we don't snapshot (direct assignment / chat):
// degrade to a short structural label by trigger source. New tasks
// (post-061 migration) almost always hit the snapshot path.

// ─── Row visual config ─────────────────────────────────────────────────────

const STATUS_TONE: Record<AgentTask["status"], string> = {
  queued: "text-warning",
  dispatched: "text-warning",
  // Same tone as queued/dispatched — visually "stopped" so users see the
  // task is parked, but distinguished by the status label.
  waiting_local_directory: "text-warning",
  running: "text-info",
  completed: "text-success",
  failed: "text-destructive",
  cancelled: "text-muted-foreground",
};

// ─── Active row ────────────────────────────────────────────────────────────

import { stripMentionMarkdown } from "../utils/strip-mention-markdown";

function useTriggerText(task: AgentTask): string {
  const { t } = useT("issues");
  const isRetry = !!task.parent_task_id;
  const retryPrefix = isRetry
    ? task.attempt && task.attempt > 1
      ? t(($) => $.execution_log.trigger_retry_attempt_prefix, { attempt: task.attempt })
      : t(($) => $.execution_log.trigger_retry_prefix)
    : "";

  if (task.trigger_summary) return retryPrefix + stripMentionMarkdown(task.trigger_summary);
  if (isRetry) {
    return task.attempt && task.attempt > 1
      ? t(($) => $.execution_log.trigger_retry_attempt, { attempt: task.attempt })
      : t(($) => $.execution_log.trigger_retry);
  }
  if (task.autopilot_run_id) return t(($) => $.execution_log.trigger_autopilot);
  if (task.trigger_comment_id) return t(($) => $.execution_log.trigger_comment);
  return t(($) => $.execution_log.trigger_initial);
}

function useStatusLabel(status: AgentTask["status"]): string {
  const { t } = useT("issues");
  switch (status) {
    case "queued": return t(($) => $.execution_log.status_queued);
    case "dispatched": return t(($) => $.execution_log.status_dispatched);
    case "waiting_local_directory":
      return t(($) => $.execution_log.status_waiting_local_directory);
    case "running": return t(($) => $.execution_log.status_running);
    case "completed": return t(($) => $.execution_log.status_completed);
    case "failed": return t(($) => $.execution_log.status_failed);
    case "cancelled": return t(($) => $.execution_log.status_cancelled);
  }
}

// One active (running / queued / dispatched / parked) task row. Running rows
// keep status to a single live elapsed timer; transcript and stop stay available
// as hover actions. Transcript content lazy-loads on click via TranscriptButton,
// so the row no longer fetches task messages just to render a count.
export function ActiveTaskRow({
  task,
  issueId,
}: {
  task: AgentTask;
  issueId: string;
}) {
  const { t } = useT("issues");
  const [cancelling, setCancelling] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const tone = STATUS_TONE[task.status];
  const label = useStatusLabel(task.status);
  const trigger = useTriggerText(task);

  // Running rows show a live-ticking elapsed timer (the ticking digits carry
  // "alive", the duration carries "how long"). Only running rows tick.
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (task.status !== "running") return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [task.status]);
  const elapsed =
    task.status === "running"
      ? formatDuration(
          task.started_at ?? task.dispatched_at ?? task.created_at,
          now,
        )
      : "";

  // Transcript only meaningful once messages exist — pure-queued and
  // waiting_local_directory tasks haven't streamed any agent output yet.
  const showTranscript =
    task.status !== "queued" && task.status !== "waiting_local_directory";

  const handleCancel = async () => {
    if (cancelling) return;
    setCancelling(true);
    try {
      await api.cancelTask(issueId, task.id);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.execution_log.cancel_failed));
      setCancelling(false);
    }
  };

  const requestCancel = () => {
    if (cancelling) return;
    setConfirmOpen(true);
  };

  return (
    <RowShell task={task}>
      <TriggerText text={trigger} />
      <RowStatus title={label}>
        {task.status === "running" ? (
          <>
            <span className="text-info tabular-nums">{elapsed}</span>
            <span className="sr-only">{label}</span>
          </>
        ) : (
          <span className={`${tone} min-w-0 truncate`}>{label}</span>
        )}
      </RowStatus>
      <RowActions>
        {showTranscript && (
          <TranscriptButton
            task={task}
            agentName=""
            isLive={task.status === "running"}
            title={t(($) => $.execution_log.transcript_tooltip)}
          />
        )}
        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type="button"
                onClick={requestCancel}
                disabled={cancelling}
                aria-label={t(($) => $.execution_log.cancel_task_aria)}
              />
            }
            className="flex items-center justify-center rounded p-1 text-destructive transition-colors hover:bg-destructive/10 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {cancelling ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Square className="h-3.5 w-3.5" />
            )}
          </TooltipTrigger>
          <TooltipContent>{t(($) => $.execution_log.cancel_task_tooltip)}</TooltipContent>
        </Tooltip>
      </RowActions>
      <TerminateTaskConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        onConfirm={() => void handleCancel()}
        showRunningNote={
          task.status === "running" ||
          task.status === "dispatched" ||
          task.status === "waiting_local_directory"
        }
      />
    </RowShell>
  );
}

// ─── Past row ──────────────────────────────────────────────────────────────

function PastRow({ task, issueId }: { task: AgentTask; issueId: string }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const [retrying, setRetrying] = useState(false);
  const label = useStatusLabel(task.status);
  const trigger = useTriggerText(task);
  const time = task.completed_at ? timeAgo(task.completed_at) : "—";
  const failureLabel =
    task.status === "failed" && task.failure_reason
      ? failureReasonLabel[task.failure_reason as TaskFailureReason]
      : null;

  // Retry only makes sense for terminal-but-not-success rows. Passing
  // task.id targets this specific row's agent — without it, the rerun
  // endpoint would fall back to the issue's current assignee and the
  // wrong agent would fire on rows whose agent has since been displaced
  // (e.g. reassignment, squad worker, or a one-off @-mention agent).
  const canRetry = task.status === "failed" || task.status === "cancelled";

  const handleRetry = async () => {
    if (retrying) return;
    setRetrying(true);
    try {
      await api.rerunIssue(issueId, task.id);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.execution_log.retry_failed));
    } finally {
      // Reset on both success and failure: the past row stays mounted
      // (its task.id is unchanged), so leaving `retrying` true on success
      // would pin the button as a permanent spinner.
      setRetrying(false);
    }
  };

  return (
    <RowShell task={task}>
      <TriggerText text={trigger} />
      <RowStatus title={failureLabel ?? label}>
        <TaskStatusIcon status={task.status} />
        <span className="sr-only">{failureLabel ?? label}</span>
        <span className="text-muted-foreground">{time}</span>
      </RowStatus>
      <RowActions>
        <TranscriptButton task={task} agentName="" title={t(($) => $.execution_log.transcript_tooltip)} />
        {canRetry && (
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  onClick={handleRetry}
                  disabled={retrying}
                  aria-label={t(($) => $.execution_log.retry_task_aria)}
                />
              }
              className="flex items-center justify-center rounded p-1 text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
            >
              {retrying ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <RotateCcw className="h-3.5 w-3.5" />
              )}
            </TooltipTrigger>
            <TooltipContent>{t(($) => $.execution_log.retry_task_tooltip)}</TooltipContent>
          </Tooltip>
        )}
      </RowActions>
    </RowShell>
  );
}

// ─── Shared row chrome ─────────────────────────────────────────────────────

function RowShell({
  task,
  children,
}: {
  task: AgentTask;
  children: React.ReactNode;
}) {
  return (
    <div className="group/execution-log-row flex items-center gap-2 overflow-hidden rounded px-1 py-1.5 transition-colors hover:bg-accent/40">
      {task.agent_id ? (
        <ActorAvatar
          actorType="agent"
          actorId={task.agent_id}
          size={20}
          enableHoverCard
        />
      ) : (
        <span className="inline-block h-5 w-5 shrink-0 rounded-full bg-muted" />
      )}
      {children}
    </div>
  );
}

function TriggerText({ text }: { text: string }) {
  return <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">{text}</span>;
}

function RowStatus({
  children,
  title,
}: {
  children: React.ReactNode;
  title?: string;
}) {
  return (
    <div
      title={title}
      className="flex h-7 shrink-0 items-center justify-end gap-1 overflow-hidden whitespace-nowrap text-xs group-hover/execution-log-row:hidden"
    >
      {children}
    </div>
  );
}

function TaskStatusIcon({ status }: { status: AgentTask["status"] }) {
  switch (status) {
    case "completed":
      return <CheckCircle2 aria-hidden="true" className="h-3.5 w-3.5 text-success" />;
    case "failed":
      return <XCircle aria-hidden="true" className="h-3.5 w-3.5 text-destructive" />;
    case "cancelled":
      return <Ban aria-hidden="true" className="h-3.5 w-3.5 text-muted-foreground" />;
    default:
      return null;
  }
}

// Action slot — hidden by default, replaces the status column in place on
// hover. No absolute/gradient needed: the status is removed (not covered),
// so nothing shows through underneath.
function RowActions({ children }: { children: React.ReactNode }) {
  return (
    <div className="hidden h-7 items-center gap-0.5 group-hover/execution-log-row:flex">
      {children}
    </div>
  );
}
