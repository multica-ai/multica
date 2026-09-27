"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Loader2, Maximize2, Square } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { issueTasksOptions } from "@multica/core/issues/queries";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";
import type { AgentTask } from "@multica/core/types";
import { useLocale, useTimeAgo } from "../../i18n";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "../../common/actor-avatar";
import { formatDuration } from "../../agents/components/agent-activity-hover-content";
import { TranscriptButton } from "../../common/task-transcript";
import { cancellationActorLabel, cancelReasonLabel, failureReasonLabel } from "../../agents/components/tabs/task-failure";
import { useT } from "../../i18n";
import { compareActiveIssueTasks } from "./active-task-order";
import { formatDuration as formatAgentTime } from "../../dashboard/utils";
import {
  formatTokens,
  formatUsd,
  summarizeTaskUsage,
  summarizeTaskUsageAcross,
} from "../../runtimes/utils";
import { TerminateTaskConfirmDialog } from "./terminate-task-confirm-dialog";
import { IssueRunsDialog, RunTriggerLabel } from "./issue-runs-dialog";
import { buildRunTimeline, type TimelineRun } from "./issue-run-timeline";
import { canRetryRun, RetryRunButton } from "./retry-run-button";
import { TaskStatusIcon } from "./task-status-icon";
import { useStatusLabel, useTriggerText } from "./task-run-labels";
import { WakeupRunLabel } from "./wakeup-source-chip";

// Right-panel section that lists every agent run for this issue. Active
// runs sit at the top (always visible when present); below them a spend strip
// (one bar per past run, oldest to newest, height = cost) and the latest few
// past runs. The full history lives in the Runs dialog the header total, the
// strip and the "Open timeline" row open — 21 rows do not belong in a 320px
// column, and the dialog can lay them out in time.
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
  /** Shown in the Runs dialog's subtitle so it names the issue it lays out. */
  identifier?: string;
  issueTitle?: string;
}

// How many past runs the sidebar lists before deferring to the Runs dialog.
const LATEST_PAST_RUNS = 3;

// Past-runs sort priority: newest first by timestamp. When two runs
// share the same timestamp, failed ranks above cancelled, which ranks
// above completed.
const PAST_STATUS_RANK: Record<string, number> = {
  failed: 0,
  cancelled: 1,
  completed: 2,
};

export function ExecutionLogSection({ issueId, identifier, issueTitle }: ExecutionLogSectionProps) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(true);
  const [runsOpen, setRunsOpen] = useState(false);
  const pricings = useCustomPricingStore((s) => s.pricings);

  // Cache key registered in `issueKeys.tasks` (packages/core/issues/queries.ts)
  // so the global useRealtimeSync `task:` prefix path invalidates it via
  // a `["issues", "tasks"]` prefix-match — no local WS subscriptions
  // needed, and the cache stays fresh even when this component isn't
  // mounted (e.g. user cancels from agent-side, then navigates here).
  const { data: tasks = [] } = useQuery(issueTasksOptions(issueId));

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
      ).toSorted(compareActiveIssueTasks),
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

  // Sidebar-only figures: the strip's bars and the agent-time line. Priced
  // with the same helpers as the dialog, and re-derived on a saved custom rate
  // for the same reason IssueRunsTotal subscribes.
  const timeline = useMemo(
    () => buildRunTimeline(tasks, Date.now()),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `pricings` re-prices on a saved custom rate
    [tasks, pricings],
  );
  const pastRuns = useMemo(() => timeline.runs.filter((run) => !run.active), [timeline]);

  if (activeTasks.length === 0 && pastTasks.length === 0) return null;

  const latest = pastTasks.slice(0, LATEST_PAST_RUNS);
  const hiddenCount = pastTasks.length - latest.length;
  const openRuns = () => setRunsOpen(true);

  return (
    // `@container/execution-log`: the header's three items only fit side by
    // side above a certain width, and the width that decides it is the
    // sidebar's — a resizable 260–420px panel — not the viewport's. See
    // IssueRunsTotal for the tier this container drives.
    <div className="@container/execution-log">
      {/* Header is two independent targets, not one: the label + chevron
          collapse the section, the total on the right opens the Runs dialog.
          Nesting a button inside a button is invalid HTML, so they are
          siblings in a flex row rather than a button wrapping a button. */}
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
        <IssueRunsTotal
          tasks={tasks}
          alone={activeTasks.length === 0}
          onOpen={openRuns}
        />
      </div>
      {open && (
        <div className="space-y-0.5 pl-2">
          {activeTasks.map((task) => (
            <ActiveTaskRow key={task.id} task={task} issueId={issueId} />
          ))}
          {activeTasks.length > 0 && pastRuns.length > 0 && (
            <div className="my-1.5 border-t border-border/60" />
          )}

          {pastRuns.length >= 2 && <RunSpendStrip runs={pastRuns} onOpen={openRuns} />}
          {timeline.agentMs > 0 && (
            <p className="truncate px-1 pb-1 text-caption text-muted-foreground">
              {t(($) => $.execution_log.agent_time, {
                duration: formatAgentTime(timeline.agentMs / 1000, "0s"),
              })}
              <span className="text-faint-foreground"> · </span>
              {t(($) => $.execution_log.elapsed, {
                duration: formatAgentTime(timeline.elapsedMs / 1000, "0s"),
              })}
            </p>
          )}

          {latest.length > 0 && (
            <>
              <div className="px-1 pt-2 pb-0.5 text-micro text-muted-foreground">
                {t(($) => $.execution_log.latest)}
              </div>
              {latest.map((task) => (
                <PastRow key={task.id} task={task} issueId={issueId} />
              ))}
            </>
          )}
          <button
            type="button"
            onClick={openRuns}
            className="flex w-full items-center gap-1.5 rounded-xs px-1 py-1.5 text-caption transition-colors hover:bg-accent/40"
          >
            <Maximize2 aria-hidden className="size-3 shrink-0 text-muted-foreground" />
            <span className="truncate">{t(($) => $.execution_log.open_timeline)}</span>
            {hiddenCount > 0 && (
              <span className="ml-auto shrink-0 text-muted-foreground tabular-nums">
                {t(($) => $.execution_log.more_runs, { count: hiddenCount })}
              </span>
            )}
          </button>
        </div>
      )}
      <IssueRunsDialog
        open={runsOpen}
        onOpenChange={setRunsOpen}
        issueId={issueId}
        identifier={identifier ?? ""}
        issueTitle={issueTitle}
        tasks={tasks}
      />
    </div>
  );
}

// ─── Spend strip ───────────────────────────────────────────────────────────

// The most bars the strip draws; older runs stay in the Runs dialog. Past this
// a 260px column cannot keep a gap between bars.
const STRIP_MAX_RUNS = 48;
const STRIP_HEIGHT = 36;

// One bar per past run, oldest to newest, height = that run's cost — the
// issue's rhythm at a glance: how busy it has been, which runs were expensive,
// and where one failed. The whole strip is one button into the Runs dialog;
// each bar carries its own hover summary.
function RunSpendStrip({ runs, onOpen }: { runs: TimelineRun[]; onOpen: () => void }) {
  const { t } = useT("issues");
  const locale = useLocale();
  const shown = runs.slice(-STRIP_MAX_RUNS);
  const maxCost = shown.reduce((m, run) => Math.max(m, run.usage?.cost ?? 0), 0);
  const dayOf = (ms: number) => new Date(ms).toDateString();
  const edgeLabel = (ms: number) =>
    dayOf(ms) === new Date().toDateString()
      ? t(($) => $.runs_timeline.day_today)
      : new Intl.DateTimeFormat(locale, { month: "short", day: "numeric" }).format(ms);
  const first = shown[0];
  const last = shown[shown.length - 1];

  return (
    <div className="px-1 pb-1">
      <button
        type="button"
        onClick={onOpen}
        aria-label={t(($) => $.execution_log.strip_aria)}
        className="flex w-full items-end gap-[3px] rounded-xs"
        style={{ height: STRIP_HEIGHT }}
      >
        {shown.map((run) => (
          <StripBar key={run.task.id} run={run} maxCost={maxCost} />
        ))}
      </button>
      {/* Day boundaries, on the same flex grid as the bars so each tick sits
          exactly between the last run of one day and the first of the next. */}
      <div aria-hidden className="flex h-1.5 gap-[3px] border-t border-border">
        {shown.map((run, i) => (
          <span key={run.task.id} className="relative max-w-3 flex-1">
            {i > 0 && dayOf(run.startMs) !== dayOf(shown[i - 1]!.startMs) && (
              <span className="absolute top-0 -left-[2px] h-1.5 w-px bg-faint-foreground" />
            )}
          </span>
        ))}
      </div>
      {first && last && (
        <div aria-hidden className="flex justify-between text-micro text-muted-foreground">
          <span>{edgeLabel(first.startMs)}</span>
          {dayOf(first.startMs) !== dayOf(last.startMs) && <span>{edgeLabel(last.startMs)}</span>}
        </div>
      )}
    </div>
  );
}

function StripBar({ run, maxCost }: { run: TimelineRun; maxCost: number }) {
  const { t } = useT("issues");
  const trigger = useTriggerText(run.task);
  const status = useStatusLabel(run.task.status);
  const cost = run.usage?.cost;
  // A run without usage is not a zero-height bar — that would read as "free".
  // It gets a dot on the baseline instead, coloured by how it ended.
  const bar =
    cost != null ? (
      <span
        className="block w-full rounded-t-xs bg-chart-2 transition-colors group-hover/strip-bar:bg-chart-1"
        style={{ height: Math.max(3, maxCost > 0 ? (cost / maxCost) * STRIP_HEIGHT : 3) }}
      />
    ) : (
      <span
        className={`mx-auto block size-1 rounded-full ${
          run.task.status === "failed" ? "bg-destructive" : "bg-faint-foreground"
        }`}
      />
    );
  return (
    <Tooltip>
      <TooltipTrigger
        render={<span />}
        className="group/strip-bar flex h-full max-w-3 min-w-0 flex-1 items-end"
      >
        {bar}
      </TooltipTrigger>
      <TooltipContent className="max-w-64 flex-col items-start gap-0">
        <RunTriggerLabel task={run.task} fallback={trigger}>
          {(label) => <span className="max-w-full truncate">{label}</span>}
        </RunTriggerLabel>
        <span className="text-micro text-muted-foreground">
          {[
            cost != null ? formatUsd(cost) : t(($) => $.execution_log.strip_no_usage),
            run.durationMs != null ? formatAgentTime(run.durationMs / 1000, "0s") : null,
            run.task.status === "completed" ? null : status,
          ]
            .filter(Boolean)
            .join(" · ")}
        </span>
      </TooltipContent>
    </Tooltip>
  );
}

// ─── Issue total ───────────────────────────────────────────────────────────

// The issue's run count and whole spend, as a header affordance: "21 runs ·
// $166". Answers "what has this issue cost" without expanding anything, and is
// the entry point to the Runs dialog.
//
// The cost is left out when no run on the issue has recorded usage — an issue
// whose runs all predate usage reporting shows its run count rather than a
// "$0.00" that would read as "this was free".
//
// Narrow sections drop the run count and keep the cost. Something has to give
// at the narrow end — the header's full form needs ~246px next to the
// active-run chip and the sidebar's 260px minimum leaves 228px — and the count
// is the piece whose absence costs least: the cost answers "what has this
// issue spent", and the runs are listed right below. It is a figure that
// yields, never a figure's digits: a clipped "$31.1…" would read as a
// different number than the issue actually spent.
export function IssueRunsTotal({
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
    // eslint-disable-next-line react-hooks/exhaustive-deps -- see above
    [tasks, pricings],
  );
  const runCount = tasks.filter((task) => task.status !== "deferred").length;
  if (runCount === 0) return null;

  // Two thresholds because the header has two shapes, and the tier should cost
  // the reader a figure only where the row genuinely runs out: beside the
  // active-run chip the full form needs ~246px, alone ~218px. Written as whole
  // literal classes — Tailwind scans source text, so a composed string would
  // generate neither. `@max-…` (rather than showing at `@min-…`) is what makes
  // a host that renders this outside the section's `@container` degrade to the
  // full form instead of silently losing the count forever. With no cost to
  // keep, the count is the whole affordance and never tiers away.
  const narrowTier = !total
    ? ""
    : alone
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
        <span className={`text-muted-foreground ${narrowTier}`}>
          {t(($) => $.execution_log.summary_runs, { count: runCount })}
        </span>
        {total && (
          <>
            <span className={`text-faint-foreground ${narrowTier}`}>·</span>
            <span className="font-medium">{formatUsd(total.cost)}</span>
          </>
        )}
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
  deferred: "text-warning",
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
  onTranscriptOpenChange?: (open: boolean, fromKeyboard?: boolean) => void;
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
      {task.wakeup_id ? (
        <WakeupRunLabel task={task} fallback={trigger} render={(label) => <TriggerText text={label} />} />
      ) : (
        <TriggerText text={trigger} />
      )}
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
            className="flex items-center justify-center rounded-xs p-1 text-destructive transition-colors hover:bg-destructive/10 disabled:cursor-not-allowed disabled:opacity-50"
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
  const { t: tAgents } = useT("agents");
  const timeAgo = useTimeAgo();
  const label = useStatusLabel(task.status);
  const trigger = useTriggerText(task);
  const time = task.completed_at ? timeAgo(task.completed_at) : "—";
  // A failed run always explains itself. A cancelled one only when the SERVER
  // cancelled it for a persisted reason (worktree claim gate, preserved-work
  // delivery). Actor provenance is rendered independently below.
  const failureLabel =
    task.status === "failed"
      ? failureReasonLabel(task.failure_reason, tAgents)
      : cancelReasonLabel(task, tAgents);
  const cancellationLabel = cancellationActorLabel(task, tAgents);
  // Hovering the status mark reveals the localized reason, never the raw
  // `task.error`. That field is operator-facing English prose the daemon and
  // server write for classification and logs (#7411) — pasting it into a
  // tooltip made every non-English workspace read English at the exact moment
  // something broke, and dragged absolute worktree paths and machine names
  // into hover text and screenshots. The full diagnostic stays one click away
  // in the transcript's Run details.
  const statusTitle = cancellationLabel
    ? [cancellationLabel, failureLabel].filter(Boolean).join(" · ")
    : failureLabel ?? label;

  // What this run cost, in the slot the relative timestamp used to hold.
  //
  // The sidebar is 288px and the row already carries an avatar, the trigger
  // text, and a status mark; a third column would come straight out of the
  // trigger, which is what people scan this list for. The list is sorted
  // newest-first, so "which run came first" is already expressed by position —
  // the exact "when" is the detail, and how much it cost is the new question.
  // The displaced timestamp moves into the row tooltip below, together with
  // the duration, token count and model — the split lives in the Runs dialog.
  //
  // `null` (no usage recorded) renders an em dash, never $0: a run from before
  // usage reporting was not free, we simply have no figure for it.
  const usage = summarizeTaskUsage(task.usage);
  const rowTitle = [
    time,
    task.started_at && task.completed_at
      ? formatDuration(task.started_at, new Date(task.completed_at).getTime())
      : "",
    usage ? formatTokens(usage.tokens) : "",
    usage?.models.join(", ") ?? "",
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <RowShell task={task} title={rowTitle}>
      {task.wakeup_id ? (
        <WakeupRunLabel task={task} fallback={trigger} render={(label) => <TriggerText text={label} />} />
      ) : (
        <TriggerText text={trigger} />
      )}
      <TaskCommentCoverage task={task} />
      <RowStatus title={statusTitle}>
        <TaskStatusIcon status={task.status} />
        <span className="sr-only">
          {[statusTitle, time].filter(Boolean).join(" · ")}
        </span>
        {usage ? (
          <span className="tabular-nums">{formatUsd(usage.cost)}</span>
        ) : (
          <span className="text-faint-foreground">—</span>
        )}
      </RowStatus>
      <RowActions>
        <TranscriptButton task={task} agentName="" title={t(($) => $.execution_log.transcript_tooltip)} />
        {canRetryRun(task) && <RetryRunButton task={task} issueId={issueId} />}
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
      className="group/execution-log-row flex items-center gap-2 overflow-hidden rounded-xs px-1 py-1.5 transition-colors hover:bg-accent/40"
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
