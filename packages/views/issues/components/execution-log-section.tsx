"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Loader2, RotateCcw, Square } from "lucide-react";
import { toast } from "sonner";
import { api, dispatchReasonCode } from "@multica/core/api";
import { issueKeys } from "@multica/core/issues/queries";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";
import type { AgentTask } from "@multica/core/types";
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
import {
  formatTokens,
  formatUsd,
  summarizeTaskUsage,
  summarizeTaskUsageAcross,
} from "../../runtimes/utils";
import { TerminateTaskConfirmDialog } from "./terminate-task-confirm-dialog";
import { IssueUsageDialog } from "./issue-usage-dialog";
import { TaskStatusIcon } from "./task-status-icon";
import { useStatusLabel, useTriggerText } from "./task-run-labels";

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
  /** Shown in the usage dialog's subtitle so the panel names what it totals. */
  identifier?: string;
}

// Past-runs sort priority: newest first by timestamp. When two runs
// share the same timestamp, failed ranks above cancelled, which ranks
// above completed.
const PAST_STATUS_RANK: Record<string, number> = {
  failed: 0,
  cancelled: 1,
  completed: 2,
};

export function ExecutionLogSection({ issueId, identifier }: ExecutionLogSectionProps) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(true);
  const [showPast, setShowPast] = useState(false);
  const [usageOpen, setUsageOpen] = useState(false);

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
    return past.toSorted((a, b) => {
      const at = a.completed_at ?? a.created_at;
      const bt = b.completed_at ?? b.created_at;
      const timeDiff = new Date(bt).getTime() - new Date(at).getTime();
      if (timeDiff !== 0) return timeDiff;
      return (
        (PAST_STATUS_RANK[a.status] ?? 99) -
        (PAST_STATUS_RANK[b.status] ?? 99)
      );
    });
  }, [tasks]);

  if (activeTasks.length === 0 && pastTasks.length === 0) return null;

  return (
    // `@container/execution-log`: the header's three items only fit side by
    // side above a certain width, and the width that decides it is the
    // sidebar's — a resizable 260–420px panel — not the viewport's. See
    // IssueUsageTotal for the tier this container drives.
    <div className="@container/execution-log">
      {/* Header is two independent targets, not one: the label + chevron
          collapse the section, the total on the right opens the usage
          breakdown. Nesting a button inside a button is invalid HTML, so they
          are siblings in a flex row rather than a button wrapping a button. */}
      <div className="mb-2 flex w-full items-center gap-1">
        <button
          type="button"
          className={`flex min-w-0 items-center gap-1 whitespace-nowrap rounded-md px-2 py-1 text-caption font-medium transition-colors hover:bg-accent/70 ${
            open ? "" : "text-muted-foreground hover:text-foreground"
          }`}
          onClick={() => setOpen(!open)}
        >
          {/* The section label is the one item here that may shrink, so it
              carries the nowrap + ellipsis pair. Without it the squeezed
              button broke "Execution log" across two lines (MUL-5804) — a
              section heading that reflows is a layout bug, not a narrow
              column. The tier below keeps the ellipsis from ever showing at
              the panel's 260px minimum; it is the backstop for a longer
              translation, not the everyday state. */}
          <span className="truncate">{t(($) => $.execution_log.section)}</span>
          <ChevronRight
            className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${
              open ? "rotate-90" : ""
            }`}
          />
        </button>
        {activeTasks.length > 0 && (
          <span className="ml-auto inline-flex shrink-0 items-center gap-1 text-info">
            <span className="h-1.5 w-1.5 rounded-full bg-info animate-pulse" />
            <span className="font-mono text-caption tabular-nums">{activeTasks.length}</span>
          </span>
        )}
        <IssueUsageTotal
          tasks={tasks}
          alone={activeTasks.length === 0}
          onOpen={() => setUsageOpen(true)}
        />
      </div>
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
                className="flex w-full items-center gap-1 rounded px-1 py-1 text-caption text-muted-foreground transition-colors hover:bg-accent/40 hover:text-foreground"
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
      <IssueUsageDialog
        open={usageOpen}
        onOpenChange={setUsageOpen}
        identifier={identifier ?? ""}
        tasks={tasks}
      />
    </div>
  );
}

// ─── Issue total ───────────────────────────────────────────────────────────

// The issue's whole spend, as a header affordance: "2.1M · $4.92". Answers
// "what has this issue cost" without expanding anything, and is the entry
// point to the per-run breakdown.
//
// Renders nothing when no run on the issue has recorded usage — an issue whose
// runs all predate usage reporting gets its old header back rather than a
// "0 · $0.00" that would read as "this was free".
//
// Narrow sections drop the token figure and keep the cost. Something has to
// give at the narrow end — the header's full form needs ~246px next to the
// active-run chip and the sidebar's 260px minimum leaves 228px — and the token
// count is the piece whose absence costs least: the cost answers "what has
// this issue spent", and the exact token split is a click away in the dialog
// this opens. It is a figure that yields, never a figure's digits: a clipped
// "$31.1…" would read as a different number than the issue actually spent.
export function IssueUsageTotal({
  tasks,
  alone,
  onOpen,
}: {
  tasks: AgentTask[];
  alone: boolean;
  onOpen: () => void;
}) {
  const { t } = useT("issues");
  // Custom rates are read imperatively inside `estimateCost`, so a saved rate
  // change does not re-render this on its own — subscribe and make the memo
  // depend on the snapshot, or the header total keeps quoting the old price
  // until the task list refetches.
  const pricings = useCustomPricingStore((s) => s.pricings);
  const total = useMemo(
    () => summarizeTaskUsageAcross(tasks.map((task) => task.usage)),
    [tasks, pricings],
  );
  if (!total) return null;

  // Two thresholds because the header has two shapes, and the tier should cost
  // the reader a figure only where the row genuinely runs out: beside the
  // active-run chip the full form needs ~246px, alone ~218px. Written as whole
  // literal classes — Tailwind scans source text, so a composed string would
  // generate neither. `@max-…` (rather than showing at `@min-…`) is what makes
  // a host that renders this outside the section's `@container` degrade to the
  // full form instead of silently losing the tokens forever.
  const narrowTier = alone
    ? "@max-[14rem]/execution-log:hidden"
    : "@max-[16rem]/execution-log:hidden";

  return (
    <Tooltip>
      <TooltipTrigger
        render={<button type="button" onClick={onOpen} />}
        className={`flex shrink-0 items-center gap-1 rounded-md px-1.5 py-0.5 text-caption tabular-nums transition-colors hover:bg-accent/70 ${
          alone ? "ml-auto" : ""
        }`}
      >
        <span className={`font-medium ${narrowTier}`}>
          {formatTokens(total.tokens)}
        </span>
        <span className={`text-faint-foreground ${narrowTier}`}>·</span>
        <span className="text-muted-foreground">{formatUsd(total.cost)}</span>
      </TooltipTrigger>
      <TooltipContent>{t(($) => $.execution_log.usage_total_tooltip)}</TooltipContent>
    </Tooltip>
  );
}

// Trigger description and status labels live in ./task-run-labels so the
// usage dialog lists a run exactly the way this section does.

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

// One active (running / queued / dispatched / parked) task row. Running rows
// keep status to a single live elapsed timer; transcript and stop stay available
// as hover actions. Transcript content lazy-loads on click via TranscriptButton,
// so the row no longer fetches task messages just to render a count.
export function ActiveTaskRow({
  task,
  issueId,
  onTranscriptOpenChange,
}: {
  task: AgentTask;
  issueId: string;
  onTranscriptOpenChange?: (open: boolean) => void;
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

  // Deliberately no token figure on an active row: the daemon reports usage
  // once, after `runner.run` returns (server/internal/daemon/daemon.go), and
  // the write publishes no realtime event — so a running task has no usage to
  // show, and would not learn of it mid-run if it did. Rendering the branch
  // anyway would only ever be exercised by hand-written fixtures, which is a
  // test that asserts a scenario production cannot produce. Restore it in the
  // same change that adds incremental reporting + cache invalidation.
  return (
    <RowShell task={task}>
      <TriggerText text={trigger} />
      <TaskCommentCoverage task={task} />
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
            onOpenChange={onTranscriptOpenChange}
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
    task.status === "failed" ? failureReasonLabel(task.failure_reason) : null;

  // What this run cost, in the slot the relative timestamp used to hold.
  //
  // The sidebar is 288px and the row already carries an avatar, the trigger
  // text, and a status mark; a third column would come straight out of the
  // trigger, which is what people scan this list for. The list is sorted
  // newest-first, so "which run came first" is already expressed by position —
  // the exact "when" is the detail, and how much it cost is the new question.
  // The displaced timestamp moves into the row tooltip below, together with
  // the duration and model, which were previously not surfaced here at all.
  //
  // `null` (no usage recorded) renders an em dash, never 0: a run from before
  // usage reporting was not free, we simply have no figure for it.
  const usage = summarizeTaskUsage(task.usage);
  const rowTitle = [
    time,
    task.started_at && task.completed_at
      ? formatDuration(task.started_at, new Date(task.completed_at).getTime())
      : "",
    usage?.models.join(", ") ?? "",
  ]
    .filter(Boolean)
    .join(" · ");

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
      // A rerun is now re-gated on the operator's invoke permission (MUL-4525):
      // a structured 403 means the agent can't be triggered, not a transient
      // failure — localize it instead of echoing the server's generic message.
      toast.error(
        dispatchReasonCode(e) === "invocation_not_allowed"
          ? t(($) => $.execution_log.retry_blocked)
          : e instanceof Error
            ? e.message
            : t(($) => $.execution_log.retry_failed),
      );
    } finally {
      // Reset on both success and failure: the past row stays mounted
      // (its task.id is unchanged), so leaving `retrying` true on success
      // would pin the button as a permanent spinner.
      setRetrying(false);
    }
  };

  return (
    <RowShell task={task} title={rowTitle}>
      <TriggerText text={trigger} />
      <TaskCommentCoverage task={task} />
      <RowStatus title={failureLabel ?? label}>
        <TaskStatusIcon status={task.status} />
        <span className="sr-only">
          {[failureLabel ?? label, time].filter(Boolean).join(" · ")}
        </span>
        {usage ? (
          <span className="tabular-nums">{formatTokens(usage.tokens)}</span>
        ) : (
          <span className="text-faint-foreground">—</span>
        )}
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
  title,
  children,
}: {
  task: AgentTask;
  /** Carries the details the right column no longer has room for (time,
   *  duration, model). Lives on the row, not on RowStatus, because RowStatus
   *  is swapped out for the action buttons on hover — a title there would
   *  disappear at exactly the moment the pointer arrives. */
  title?: string;
  children: React.ReactNode;
}) {
  return (
    <div
      title={title || undefined}
      className="group/execution-log-row flex items-center gap-2 overflow-hidden rounded px-1 py-1.5 transition-colors hover:bg-accent/40"
    >
      {task.agent_id ? (
        <ActorAvatar
          actorType="agent"
          actorId={task.agent_id}
          size="sm"
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
  return <span className="min-w-0 flex-1 truncate text-caption text-muted-foreground">{text}</span>;
}

function supportsCommentCoverage(status: AgentTask["status"]): boolean {
  switch (status) {
    case "queued":
    case "dispatched":
    case "waiting_local_directory":
    case "running":
    case "completed":
    case "failed":
    case "cancelled":
      return true;
    default:
      return false;
  }
}

export function TaskCommentCoverage({ task }: { task: AgentTask }) {
  const { t } = useT("issues");
  if (!supportsCommentCoverage(task.status)) return null;

  // Queued rows show the planned coverage: coalesced_comment_ids deliberately
  // excludes the newest trigger. Once claimed, prefer the server's actual
  // delivery receipt. Only legacy rows where that field is absent fall back to
  // the plan; an explicit [] means the claim delivered no comments.
  const plannedCommentIds = [
    task.trigger_comment_id,
    ...(task.coalesced_comment_ids ?? []),
  ];
  const coverageIds =
    task.status !== "queued" && task.delivered_comment_ids !== undefined
      ? task.delivered_comment_ids
      : plannedCommentIds;
  const commentIds = new Set(
    coverageIds.filter((id): id is string => Boolean(id)),
  );
  if (commentIds.size <= 1) return null;

  return (
    <span className="shrink-0 whitespace-nowrap text-micro text-muted-foreground">
      {t(($) => $.execution_log.included_comments, { count: commentIds.size })}
    </span>
  );
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
      className="flex h-7 shrink-0 items-center justify-end gap-1 overflow-hidden whitespace-nowrap text-caption [@media(hover:hover)]:group-hover/execution-log-row:hidden"
    >
      {children}
    </div>
  );
}

// Action slot — visible by default for touch devices. On hover-capable
// surfaces, it replaces the status column in place on row hover.
function RowActions({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-7 items-center gap-0.5 [@media(hover:hover)]:hidden [@media(hover:hover)]:group-hover/execution-log-row:flex">
      {children}
    </div>
  );
}
