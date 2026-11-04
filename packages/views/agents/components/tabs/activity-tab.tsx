"use client";

import type { ReactNode } from "react";
import { useMemo, useState } from "react";
import {
  ArrowUpRight,
  CircleHelp,
  Hash,
  MessageSquare,
  Workflow,
  X,
} from "@multica/ui/icons";
import { toast } from "sonner";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { useInfiniteQuery, useQueries, useQuery } from "@tanstack/react-query";
import type { Agent, AgentTask, Issue } from "@multica/core/types";
import {
  agentTaskSnapshotOptions,
  agentTasksOptions,
  summarizeActivityWindow,
  useWorkspaceActivityMap,
} from "@multica/core/agents";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { AppLink } from "../../../navigation";
import { TranscriptButton } from "../../../common/task-transcript";
import { AttributionBadge } from "../../../issues/components/attribution-badge";
import {
  ObjectSection,
  ObjectSectionAction,
  ObjectSectionCard,
} from "../../../layout/object-page";
import { taskStatusConfig } from "../../config";
import { cancellationActorLabel, cancelReasonLabel, failureReasonLabel } from "./task-failure";
import { Sparkline } from "../sparkline";
import { useT, useTimeAgo } from "../../../i18n";

// Reveal cached rows in small batches and fetch another page only on demand.
const RECENT_INITIAL = 10;
const RECENT_PAGE = 20;
// Placeholder rows shown while the lazily-loaded per-agent task list is
// still in flight, so first paint is a skeleton rather than the "nothing
// finished yet" empty state (which reads as a wrong answer).
const RECENT_SKELETON_ROWS = 4;
// Row actions are 22px for a pointer; on touch they need a fingertip-sized
// target.
const TOUCH_ACTION_CLASS = "[@media(hover:none)]:p-2";

/**
 * The agent page's two work sections, in scan order: Now (what is it doing
 * this second?) and Recent work (what did it just finish?).
 *
 * "Now" reads the workspace task snapshot. Recent work loads bounded history
 * pages on demand; opening the page never fetches all runs.
 */
export function AgentWorkSections({
  agent,
  onViewAll,
}: {
  agent: Agent;
  /** Opens every issue assigned to this agent. */
  onViewAll: () => void;
}) {
  const wsId = useWorkspaceId();

  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));
  // `isLoading` (pending + fetching, no cached data) is true only on the
  // very first fetch. Once this cache is hydrated the section opens
  // straight into data with no skeleton flash.
  const {
    data,
    isLoading: isLoadingRecent,
    hasNextPage,
    fetchNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
  } = useInfiniteQuery(agentTasksOptions(wsId, agent.id));
  const agentTasks = useMemo(() => {
    const tasks = new Map<string, AgentTask>();
    for (const page of data?.pages ?? []) {
      for (const task of page.tasks) tasks.set(task.id, task);
    }
    return [...tasks.values()];
  }, [data]);
  const { byAgent: activityMap, loading: activityLoading } =
    useWorkspaceActivityMap(wsId);
  const weekRuns = summarizeActivityWindow(activityMap.get(agent.id), 7).totalRuns;

  const [recentDisplayLimit, setRecentDisplayLimit] = useState(RECENT_INITIAL);

  // Chat tasks are intentionally hidden across every Agent-scoped surface
  // (list / detail / activity). They have their own UI in the chat
  // experience; mixing them in here muddies "what is this agent doing
  // for the team" with "what is this agent doing in private chat".
  const isWorkflowTask = (t: AgentTask) => !t.chat_session_id;

  const activeTasks = useMemo(() => {
    const statusRank: Partial<Record<AgentTask["status"], number>> = {
      running: 0,
      dispatched: 1,
      waiting_local_directory: 2,
      queued: 3,
    };
    return snapshot
      .filter(
        (t) =>
          t.agent_id === agent.id &&
          isWorkflowTask(t) &&
          (t.status === "running" ||
            t.status === "queued" ||
            t.status === "dispatched" ||
            t.status === "waiting_local_directory"),
      )
      .sort(
        (a, b) =>
          (statusRank[a.status] ?? 99) - (statusRank[b.status] ?? 99) ||
          new Date(a.created_at).getTime() - new Date(b.created_at).getTime(),
      );
  }, [snapshot, agent.id]);

  // Most recent terminal tasks. Includes cancelled — users searching
  // "what just happened" want to see cancellations alongside completions
  // and failures. Chat sessions filtered out for the same reason as above.
  const recentTasksAll = useMemo(() => {
    return [...agentTasks]
      .filter(
        (t) =>
          isWorkflowTask(t) &&
          !!t.completed_at &&
          (t.status === "completed" ||
            t.status === "failed" ||
            t.status === "cancelled"),
      )
      .sort(
        (a, b) =>
          new Date(b.completed_at!).getTime() -
          new Date(a.completed_at!).getTime(),
      );
  }, [agentTasks]);

  const recentTasks = useMemo(
    () => recentTasksAll.slice(0, recentDisplayLimit),
    [recentTasksAll, recentDisplayLimit],
  );
  const hasMoreRecent = recentTasksAll.length > recentTasks.length || hasNextPage;

  // Resolve issue identifiers + titles for any task we'll render. Going
  // through `issueDetailOptions` is the same lookup the rest of the app
  // uses, so the cache is shared and we don't pay for a duplicate request.
  const displayedTasks = useMemo(
    () => [...activeTasks, ...recentTasks],
    [activeTasks, recentTasks],
  );
  const issueIds = useMemo(
    () =>
      Array.from(
        new Set(displayedTasks.map((t) => t.issue_id).filter((id) => id !== "")),
      ),
    [displayedTasks],
  );
  const issueQueries = useQueries({
    queries: issueIds.map((id) => issueDetailOptions(wsId, id)),
  });
  const issueMap = useMemo(() => {
    const m = new Map<string, Issue>();
    issueQueries.forEach((q, i) => {
      const id = issueIds[i]!;
      if (q.data) m.set(id, q.data);
    });
    return m;
  }, [issueQueries, issueIds]);

  return (
    <>
      <NowSection tasks={activeTasks} issueMap={issueMap} agent={agent} />
      <RecentWorkSection
        tasks={recentTasks}
        weekRuns={activityLoading ? null : weekRuns}
        hasMore={hasMoreRecent}
        loading={isLoadingRecent}
        fetchingMore={isFetchingNextPage}
        fetchMoreFailed={isFetchNextPageError}
        onShowMore={() => {
          if (recentDisplayLimit < recentTasksAll.length) {
            setRecentDisplayLimit((n) => n + RECENT_PAGE);
          } else if (hasNextPage && !isFetchingNextPage) {
            void fetchNextPage({ cancelRefetch: false }).then((result) => {
              if (!result.isFetchNextPageError) {
                setRecentDisplayLimit((n) => n + RECENT_PAGE);
              }
            });
          }
        }}
        onViewAll={onViewAll}
        issueMap={issueMap}
        agent={agent}
      />
    </>
  );
}

/** The 30-day performance numbers for the properties panel. Kept apart from
 * the work sections so metrics never outrank current tasks or failures. */
export function AgentPerformanceSummary({ agent }: { agent: Agent }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const { byAgent: activityMap } = useWorkspaceActivityMap(wsId);
  const activity = activityMap.get(agent.id);
  const summary = summarizeActivityWindow(activity, 30);
  const avgDurationMs = activity?.avgDurationMs ?? 0;

  if (summary.totalRuns === 0) {
    return (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.tab_body.activity.empty_30d)}
      </p>
    );
  }

  return (
    <div>
      <div className="grid grid-cols-2 gap-x-4 gap-y-3">
        <Metric
          value={String(summary.totalRuns)}
          label={t(($) => $.tab_body.activity.runs, {
            count: summary.totalRuns,
          })}
        />
        <Metric
          value={<SuccessRate rate={summary.successRate} />}
          label={t(($) => $.tab_body.activity.success_label)}
        />
        <Metric
          value={avgDurationMs > 0 ? formatDurationMs(avgDurationMs) : "—"}
          label={t(($) => $.tab_body.activity.avg_duration_label)}
        />
        <Metric
          value={String(summary.totalFailed)}
          label={t(($) => $.tab_body.activity.failed_label)}
          destructive={summary.totalFailed > 0}
        />
      </div>
      {summary.totalCancelled > 0 && (
        <p className="mt-2 text-caption text-muted-foreground">
          {t(($) => $.tab_body.activity.cancelled_count, {
            count: summary.totalCancelled,
          })}
        </p>
      )}
      <Sparkline
        buckets={summary.buckets}
        width={250}
        height={36}
        className="mt-4 h-9 w-full"
      />
    </div>
  );
}

function Metric({
  value,
  label,
  destructive = false,
}: {
  value: ReactNode;
  label: string;
  destructive?: boolean;
}) {
  return (
    <div className="min-w-0">
      <div
        className={`text-title font-semibold tabular-nums ${
          destructive ? "text-destructive" : "text-foreground"
        }`}
      >
        {value}
      </div>
      <div className="truncate text-micro text-muted-foreground">{label}</div>
    </div>
  );
}

/** A success rate with the tooltip that says what it counts. */
export function SuccessRate({
  rate,
  labelled = false,
}: {
  rate: number | null;
  labelled?: boolean;
}) {
  const { t } = useT("agents");
  return (
    <Tooltip>
      <TooltipTrigger render={<span tabIndex={0} />}>
        {rate === null
          ? "—"
          : labelled
            ? t(($) => $.tab_body.activity.success_pct, { percent: rate })
            : `${rate}%`}
      </TooltipTrigger>
      <TooltipContent>
        {rate === null
          ? t(($) => $.tab_body.activity.success_unavailable)
          : t(($) => $.tab_body.activity.success_hint)}
      </TooltipContent>
    </Tooltip>
  );
}

function NowSection({
  tasks,
  issueMap,
  agent,
}: {
  tasks: AgentTask[];
  issueMap: Map<string, Issue>;
  agent: Agent;
}) {
  const { t } = useT("agents");
  return (
    <ObjectSection
      title={t(($) => $.tab_body.activity.section_now)}
      count={
        tasks.length > 0
          ? t(($) => $.sections.runs_count, { count: tasks.length })
          : undefined
      }
    >
      {tasks.length === 0 ? (
        <p className="text-body text-muted-foreground">
          {t(($) => $.tab_body.activity.empty_now)}
        </p>
      ) : (
        <ObjectSectionCard>
          {tasks.map((task) => (
            <LiveTaskRow key={task.id} task={task} issueMap={issueMap} agent={agent} />
          ))}
        </ObjectSectionCard>
      )}
    </ObjectSection>
  );
}

function RecentWorkSection({
  tasks,
  weekRuns,
  hasMore,
  loading,
  onShowMore,
  fetchingMore,
  fetchMoreFailed,
  onViewAll,
  issueMap,
  agent,
}: {
  tasks: AgentTask[];
  /** Runs in the last 7 days; `null` until the activity window has loaded. */
  weekRuns: number | null;
  hasMore: boolean;
  loading: boolean;
  onShowMore: () => void;
  fetchingMore: boolean;
  fetchMoreFailed: boolean;
  onViewAll: () => void;
  issueMap: Map<string, Issue>;
  agent: Agent;
}) {
  const { t } = useT("agents");
  return (
    <ObjectSection
      title={t(($) => $.tab_body.activity.section_recent)}
      count={
        weekRuns === null
          ? undefined
          : t(($) => $.sections.recent_this_week, { count: weekRuns })
      }
      action={
        <ObjectSectionAction icon={ArrowUpRight} onClick={onViewAll}>
          {t(($) => $.sections.view_all)}
        </ObjectSectionAction>
      }
    >
      {loading ? (
        <RecentWorkSkeleton />
      ) : tasks.length === 0 ? (
        hasMore ? null : (
          <p className="text-body text-muted-foreground">
            {t(($) => $.tab_body.activity.empty_recent)}
          </p>
        )
      ) : (
        <ObjectSectionCard>
          {tasks.map((task) => (
            <RecentTaskRow key={task.id} task={task} issueMap={issueMap} agent={agent} />
          ))}
        </ObjectSectionCard>
      )}
      {!loading && hasMore && (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={onShowMore}
          disabled={fetchingMore}
          aria-busy={fetchingMore}
          className="text-muted-foreground"
        >
          {fetchMoreFailed ? t(($) => $.detail.try_again) : t(($) => $.tab_body.activity.show_more)}
        </Button>
      )}
    </ObjectSection>
  );
}

/**
 * Loading placeholder for the Recent work list. Mirrors the card and the
 * one-line row rhythm so the skeleton settles into the real rows without a
 * layout jump. Widths are staggered per row so it reads as content rather
 * than a solid block.
 */
function RecentWorkSkeleton() {
  const titleWidths = ["w-3/5", "w-4/5", "w-1/2", "w-2/3"];
  return (
    <ObjectSectionCard className="bg-transparent">
      <div aria-hidden="true" className="divide-y">
        {Array.from({ length: RECENT_SKELETON_ROWS }).map((_, i) => (
          <div key={i} className="flex h-10 items-center gap-3 px-3">
            <Skeleton className="size-4 shrink-0 rounded-full" />
            <Skeleton className={`h-3.5 ${titleWidths[i % titleWidths.length]}`} />
            <Skeleton className="ml-auto h-3 w-20" />
          </div>
        ))}
      </div>
    </ObjectSectionCard>
  );
}

type AgentsT = ReturnType<typeof useT<"agents">>["t"];
type TimeAgoFn = (dateStr: string) => string;

/** What every task row shows, whichever section it sits in. */
function useTaskRowModel(task: AgentTask, issueMap: Map<string, Issue>) {
  const { t } = useT("agents");
  const cfg = taskStatusConfig[task.status] ?? taskStatusConfig.queued!;
  const hasIssue = task.issue_id !== "";
  const issue = hasIssue ? issueMap.get(task.issue_id) : undefined;
  const isTerminalStatus =
    task.status === "completed" ||
    task.status === "failed" ||
    task.status === "cancelled";
  const sourceFallback = !hasIssue
    ? task.kind === "quick_create"
      ? isTerminalStatus
        ? t(($) => $.tab_body.activity.source_quick_create)
        : t(($) => $.tab_body.activity.source_creating_issue)
      : task.chat_session_id
        ? t(($) => $.tab_body.activity.source_chat_session)
        : task.autopilot_run_id
          ? t(($) => $.tab_body.activity.source_autopilot_run)
          : t(($) => $.tab_body.activity.source_untracked)
    : null;
  const SourceIcon = hasIssue
    ? Hash
    : task.chat_session_id
      ? MessageSquare
      : task.autopilot_run_id
        ? Workflow
        : CircleHelp;
  const sourceLabel = hasIssue
    ? t(($) => $.tab_body.activity.source_issue)
    : task.chat_session_id
      ? t(($) => $.tab_body.activity.source_chat)
      : task.autopilot_run_id
        ? t(($) => $.tab_body.activity.source_autopilot)
        : t(($) => $.tab_body.activity.source_untracked);
  const title =
    issue?.title ??
    (hasIssue
      ? t(($) => $.tab_body.activity.issue_short_fallback, { prefix: task.issue_id.slice(0, 8) })
      : (sourceFallback ?? t(($) => $.tab_body.activity.source_untracked)));
  // Failure reason. The back-end emits "" on non-failed tasks (omitempty
  // strips it on the wire) so the truthy guard is the right shape.
  // failureReasonLabel takes the raw open string because the taxonomy grows,
  // so there is no enum to cast to. Cancelled rows get a label only when the
  // SERVER cancelled them for a persisted reason (worktree claim gate,
  // preserved-work delivery); a user's own cancel stays a plain "Cancelled".
  const failureLabel =
    task.status === "failed"
      ? failureReasonLabel(task.failure_reason, t)
      : cancelReasonLabel(task, t);
  const statusLabel = cancellationActorLabel(task, t) ?? taskStatusLabel(task.status, t);
  return {
    cfg,
    hasIssue,
    issue,
    title,
    SourceIcon,
    sourceLabel,
    sourceFallback,
    failureLabel,
    statusLabel,
  };
}

/** The task title; hovering it says why the task ran. */
function TaskTitle({ task, title }: { task: AgentTask; title: string }) {
  const { t } = useT("agents");
  if (!task.trigger_summary) {
    return <span className="min-w-0 truncate text-body">{title}</span>;
  }
  // The snapshot lets the row stay anchored on issue.title while the user
  // can still dwell to see the trigger context — the same pattern as GitHub
  // Actions surfacing the commit message on hover.
  return (
    <Tooltip>
      <TooltipTrigger
        render={<span className="min-w-0 truncate text-body">{title}</span>}
      />
      <TooltipContent className="max-w-md">
        <div className="text-micro font-medium uppercase tracking-wider text-muted-foreground">
          {t(($) => $.tab_body.activity.triggered_by)}
        </div>
        <div className="mt-0.5 whitespace-pre-wrap text-caption">
          {task.trigger_summary}
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

function OpenIssueIconLink({ issueId }: { issueId: string }) {
  const { t } = useT("agents");
  const paths = useWorkspacePaths();
  return (
    <Tooltip>
      <TooltipTrigger
        render={<AppLink href={paths.issueDetail(issueId)} />}
        aria-label={t(($) => $.tab_body.activity.open_issue_aria)}
        className={`flex items-center justify-center rounded-xs p-1 text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground ${TOUCH_ACTION_CLASS}`}
      >
        <ArrowUpRight className="h-3.5 w-3.5" aria-hidden="true" />
      </TooltipTrigger>
      <TooltipContent>{t(($) => $.tab_body.activity.open_issue_tooltip)}</TooltipContent>
    </Tooltip>
  );
}

// The rows are intentionally non-clickable so neither destination is
// privileged — issue detail and transcript are equally valid follow-ups.
// focus-within keeps the action slot reachable for keyboard users; touch
// screens have no hover, so there the slot stays visible.
const HOVER_ACTIONS_CLASS =
  "flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity duration-100 group-hover:opacity-100 group-focus-within:opacity-100 [@media(hover:none)]:opacity-100";

/** A run in progress: what it is working on, for how long, and a way in. */
function LiveTaskRow({
  task,
  issueMap,
  agent,
}: {
  task: AgentTask;
  issueMap: Map<string, Issue>;
  agent: Agent;
}) {
  const { t } = useT("agents");
  const timeAgo = useTimeAgo();
  const paths = useWorkspacePaths();
  const [cancelling, setCancelling] = useState(false);
  const { cfg, hasIssue, issue, title, SourceIcon, sourceLabel, sourceFallback, statusLabel } =
    useTaskRowModel(task, issueMap);
  const Icon = cfg.icon;
  const isRunning = task.status === "running";
  // Queued tasks have no messages yet — hiding the transcript button avoids
  // a guaranteed "No execution data recorded." dialog open.
  const showTranscript = task.status !== "queued";
  const showCancel =
    task.status === "queued" ||
    task.status === "dispatched" ||
    task.status === "running";

  const handleCancel = async () => {
    if (cancelling) return;
    setCancelling(true);
    try {
      await api.cancelTaskById(task.id);
      // No manual invalidate needed — the task:cancelled WS event flows
      // through useRealtimeSync's `task:` prefix path which already
      // invalidates snapshot + per-agent + per-issue task lists.
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t(($) => $.tab_body.activity.cancel_failed_toast));
      setCancelling(false);
    }
  };

  return (
    <div className="group flex items-center gap-3 px-3 py-2.5">
      <span
        className={cn(
          "flex size-8 shrink-0 items-center justify-center rounded-md",
          isRunning ? "bg-brand/10" : "bg-muted",
        )}
      >
        <Icon
          className={cn(
            "size-4",
            cfg.color,
            isRunning && "animate-spin motion-reduce:animate-none",
          )}
          aria-hidden="true"
        />
      </span>
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground">
          <SourceIcon className="size-3 shrink-0 text-faint-foreground" aria-label={sourceLabel} />
          <span className={cn("shrink-0", issue && "font-mono")}>
            {issue?.identifier ?? sourceFallback ?? sourceLabel}
          </span>
          <span aria-hidden="true">·</span>
          <span className={cn("truncate", cfg.color)}>{statusLabel}</span>
        </div>
        <div className="flex min-w-0">
          <TaskTitle task={task} title={title} />
        </div>
      </div>
      <span className="hidden shrink-0 text-caption text-muted-foreground sm:inline">
        {activeTaskTimeText(task, t, timeAgo)}
      </span>
      <div className={HOVER_ACTIONS_CLASS}>
        {showTranscript && (
          <TranscriptButton
            task={task}
            agentName={agent.name}
            isLive={isRunning}
            title={t(($) => $.tab_body.activity.transcript_tooltip)}
            className={TOUCH_ACTION_CLASS}
          />
        )}
        {showCancel && (
          <Tooltip>
            <TooltipTrigger
              render={
                <button
                  type="button"
                  onClick={handleCancel}
                  disabled={cancelling}
                  aria-label={t(($) => $.tab_body.activity.cancel_task_aria)}
                />
              }
              className={`flex items-center justify-center rounded-xs p-1 text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive disabled:cursor-not-allowed disabled:opacity-50 ${TOUCH_ACTION_CLASS}`}
            >
              <X className="h-3.5 w-3.5" aria-hidden="true" />
            </TooltipTrigger>
            <TooltipContent>
              {cancelling ? t(($) => $.tab_body.activity.cancelling_tooltip) : t(($) => $.tab_body.activity.cancel_task_tooltip)}
            </TooltipContent>
          </Tooltip>
        )}
      </div>
      {hasIssue && (
        <Button
          variant="outline"
          size="sm"
          render={<AppLink href={paths.issueDetail(task.issue_id)} />}
          nativeButton={false}
        >
          {t(($) => $.sections.open)}
        </Button>
      )}
    </div>
  );
}

/** A finished run on one line: outcome, what it was, and when. */
function RecentTaskRow({
  task,
  issueMap,
  agent,
}: {
  task: AgentTask;
  issueMap: Map<string, Issue>;
  agent: Agent;
}) {
  const { t } = useT("agents");
  const timeAgo = useTimeAgo();
  const { cfg, hasIssue, issue, title, SourceIcon, sourceLabel, failureLabel, statusLabel } =
    useTaskRowModel(task, issueMap);
  const Icon = cfg.icon;

  let durationText: string | null = null;
  if (task.started_at && task.completed_at) {
    const dur =
      new Date(task.completed_at).getTime() -
      new Date(task.started_at).getTime();
    if (dur > 0) durationText = formatDurationMs(dur);
  }

  return (
    <div className="group flex min-h-10 items-center gap-3 px-3 py-2 transition-colors hover:bg-accent/40">
      <Icon className={cn("size-4 shrink-0", cfg.color)} aria-hidden="true" />
      {issue ? (
        <span className="shrink-0 font-mono text-caption text-muted-foreground">
          {issue.identifier}
        </span>
      ) : (
        <SourceIcon className="size-3 shrink-0 text-faint-foreground" aria-label={sourceLabel} />
      )}
      <div className="flex min-w-0 flex-1 items-center gap-2">
        <TaskTitle task={task} title={title} />
        {/* The localized reason is the whole user-facing explanation here;
            the raw `task.error` (untranslated, with absolute paths, #7411)
            lives in the transcript's Run details instead. */}
        {failureLabel && (
          <span className="hidden shrink-0 text-caption text-destructive sm:inline">
            {failureLabel}
          </span>
        )}
      </div>
      {/* Accountable member (MUL-4302 §9): whose behalf this run is on. */}
      {task.attribution?.initiator && (
        <AttributionBadge attribution={task.attribution} variant="avatar" />
      )}
      <span className="shrink-0 text-caption text-muted-foreground">
        <span className={cfg.color}>{statusLabel}</span>
        <Sep />
        {task.completed_at ? timeAgo(task.completed_at) : "—"}
        {durationText && (
          <span className="hidden sm:inline">
            <Sep />
            {durationText}
          </span>
        )}
      </span>
      <div className={HOVER_ACTIONS_CLASS}>
        {hasIssue && <OpenIssueIconLink issueId={task.issue_id} />}
        <TranscriptButton
          task={task}
          agentName={agent.name}
          isLive={false}
          title={t(($) => $.tab_body.activity.transcript_tooltip)}
          className={TOUCH_ACTION_CLASS}
        />
      </div>
    </div>
  );
}

function Sep() {
  // mx-1 puts visible whitespace around the dot; without it inline JSX
  // collapses neighbouring tokens into one word at a glance.
  return <span className="mx-1 text-faint-foreground">·</span>;
}

function taskStatusLabel(status: AgentTask["status"], t: AgentsT): string {
  switch (status) {
    case "queued":
    case "deferred":
      return t(($) => $.tab_body.activity.status.queued);
    case "dispatched":
      return t(($) => $.tab_body.activity.status.dispatched);
    case "waiting_local_directory":
      return t(($) => $.tab_body.activity.status.waiting_local_directory);
    case "running":
      return t(($) => $.tab_body.activity.status.running);
    case "completed":
      return t(($) => $.tab_body.activity.status.completed);
    case "failed":
      return t(($) => $.tab_body.activity.status.failed);
    case "cancelled":
      return t(($) => $.tab_body.activity.status.cancelled);
  }
}

function activeTaskTimeText(task: AgentTask, t: AgentsT, timeAgo: TimeAgoFn): string {
  if (task.status === "running" && task.started_at) {
    return t(($) => $.tab_body.activity.started_prefix, { when: timeAgo(task.started_at) });
  }
  if (task.status === "dispatched" && task.dispatched_at) {
    return t(($) => $.tab_body.activity.dispatched_prefix, { when: timeAgo(task.dispatched_at) });
  }
  return t(($) => $.tab_body.activity.queued_prefix, { when: timeAgo(task.created_at) });
}

/**
 * Compact human-readable duration ("12s", "2m 04s", "1h 30m"). Pads the
 * seconds inside the minute formatter so the column stays visually
 * aligned across rows.
 */
export function formatDurationMs(ms: number): string {
  if (ms <= 0) return "—";
  if (ms < 60_000) {
    return `${Math.max(1, Math.round(ms / 1000))}s`;
  }
  if (ms < 60 * 60_000) {
    const m = Math.floor(ms / 60_000);
    const s = Math.round((ms % 60_000) / 1000);
    return `${m}m ${String(s).padStart(2, "0")}s`;
  }
  const h = Math.floor(ms / (60 * 60_000));
  const m = Math.floor((ms % (60 * 60_000)) / 60_000);
  return `${h}h ${m}m`;
}
