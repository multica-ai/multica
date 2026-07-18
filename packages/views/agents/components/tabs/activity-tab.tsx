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
} from "lucide-react";
import { toast } from "sonner";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { NumberFlow } from "@multica/ui/components/ui/number-flow";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useQueries, useQuery } from "@tanstack/react-query";
import type {
  Agent,
  AgentTask,
  Issue,
  TaskFailureReason,
} from "@multica/core/types";
import {
  type AgentActivity,
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
import { taskStatusConfig } from "../../config";
import { failureReasonLabel } from "./task-failure";
import { Sparkline } from "../sparkline";
import { useT, useTimeAgo } from "../../../i18n";

const THIRTY_DAYS_MS = 30 * 24 * 60 * 60 * 1000;
// Recent work pagination: small initial cohort to keep the section
// scannable, then "Show more" reveals 20 at a time. Tasks are already
// fully cached client-side (one listAgentTasks for the whole agent), so
// "more" is a pure state flip — zero extra fetches.
const RECENT_INITIAL = 10;
const RECENT_PAGE = 20;
// Placeholder rows shown while the lazily-loaded per-agent task list is
// still in flight, so first paint of the tab is a skeleton rather than the
// "nothing finished yet" empty state (which reads as a wrong answer).
const RECENT_SKELETON_ROWS = 4;

interface ActivityTabProps {
  agent: Agent;
  showPerformance?: boolean;
}

/**
 * Right-pane Activity tab on the agent detail page. Three sections framed
 * around the user's three diagnostic questions, in scan order:
 *
 *   Now           — what's it doing right this second?
 *   Last 7 days   — how has it been doing in aggregate?
 *   Recent work   — what did it just finish?
 *
 * All three read from caches the rest of the page already fills (the
 * workspace task snapshot for "Now", per-agent task list for "Recent",
 * the workspace 7d activity buckets for the trend), so opening this tab
 * adds no extra fetches once the page is hydrated.
 */
export function ActivityTab({ agent, showPerformance = true }: ActivityTabProps) {
  const wsId = useWorkspaceId();

  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));
  // `isLoading` (pending + fetching, no cached data) is true only on the
  // very first fetch. Once the page has hydrated this cache elsewhere the
  // tab opens straight into data with no skeleton flash.
  const { data: agentTasks = [], isLoading: isLoadingRecent } = useQuery(
    agentTasksOptions(wsId, agent.id),
  );
  const { byAgent: activityMap } = useWorkspaceActivityMap(wsId);
  const activity = activityMap.get(agent.id);

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
  const hasMoreRecent = recentTasksAll.length > recentTasks.length;

  const avgDurationMs = useMemo(
    () => deriveAvgDurationLast30d(agentTasks, Date.now()),
    [agentTasks],
  );

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
    <div className="flex min-w-0 flex-col gap-6">
      <NowSection tasks={activeTasks} issueMap={issueMap} agent={agent} />
      {showPerformance && (
        <Last30dSection activity={activity} avgDurationMs={avgDurationMs} />
      )}
      <RecentWorkSection
        tasks={recentTasks}
        totalCount={recentTasksAll.length}
        hasMore={hasMoreRecent}
        loading={isLoadingRecent}
        onShowMore={() =>
          setRecentDisplayLimit((n) => n + RECENT_PAGE)
        }
        issueMap={issueMap}
        agent={agent}
      />
    </div>
  );
}

/** Compact performance context for the Overview sidebar. Kept separate from
 * the work list so metrics never outrank current tasks or failures. */
export function AgentPerformanceSummary({ agent }: { agent: Agent }) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const { data: agentTasks = [] } = useQuery(
    agentTasksOptions(wsId, agent.id),
  );
  const { byAgent: activityMap } = useWorkspaceActivityMap(wsId);
  const activity = activityMap.get(agent.id);
  const summary = summarizeActivityWindow(activity, 30);
  const avgDurationMs = useMemo(
    () => deriveAvgDurationLast30d(agentTasks, Date.now()),
    [agentTasks],
  );
  const successPct =
    summary.totalRuns > 0
      ? Math.round(
          ((summary.totalRuns - summary.totalFailed) / summary.totalRuns) *
            100,
        )
      : 100;

  return (
    <section className="mt-5 border-t pt-5">
      <h2 className="text-sm font-medium">
        {t(($) => $.tab_body.activity.section_last_30d)}
      </h2>
      {summary.totalRuns === 0 ? (
        <p className="mt-3 text-xs text-muted-foreground">
          {t(($) => $.tab_body.activity.empty_30d)}
        </p>
      ) : (
        <>
          <div className="mt-4 grid grid-cols-2 gap-x-4 gap-y-3">
            <Metric
              value={String(summary.totalRuns)}
              label={t(($) => $.tab_body.activity.runs, {
                count: summary.totalRuns,
              })}
            />
            <Metric
              value={`${successPct}%`}
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
          <Sparkline
            buckets={summary.buckets}
            width={250}
            height={36}
            className="mt-4 h-9 w-full"
          />
        </>
      )}
    </section>
  );
}

function Metric({
  value,
  label,
  destructive = false,
}: {
  value: string;
  label: string;
  destructive?: boolean;
}) {
  return (
    <div className="min-w-0">
      <div
        className={`text-lg font-semibold tabular-nums ${
          destructive ? "text-destructive" : "text-foreground"
        }`}
      >
        {value}
      </div>
      <div className="truncate text-[11px] text-muted-foreground">{label}</div>
    </div>
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
    <Section
      title={t(($) => $.tab_body.activity.section_now)}
      subtitle={
        tasks.length === 0
          ? t(($) => $.tab_body.activity.subtitle_no_active)
          : t(($) => $.tab_body.activity.subtitle_active, { count: tasks.length })
      }
    >
      {tasks.length === 0 ? (
        <EmptyText>{t(($) => $.tab_body.activity.empty_now)}</EmptyText>
      ) : (
        <TaskList
          tasks={tasks}
          issueMap={issueMap}
          timeMode="active"
          agent={agent}
        />
      )}
    </Section>
  );
}

function Last30dSection({
  activity,
  avgDurationMs,
}: {
  activity: AgentActivity | undefined;
  avgDurationMs: number;
}) {
  const { t, i18n } = useT("agents");
  const summary = summarizeActivityWindow(activity, 30);
  const { totalRuns, totalFailed } = summary;
  const locales = i18n.resolvedLanguage ?? i18n.language;
  const successPct =
    totalRuns > 0
      ? Math.round(((totalRuns - totalFailed) / totalRuns) * 100)
      : 100;

  return (
    <Section title={t(($) => $.tab_body.activity.section_last_30d)} subtitle={t(($) => $.tab_body.activity.subtitle_performance)}>
      {totalRuns === 0 ? (
        <EmptyText>{t(($) => $.tab_body.activity.empty_30d)}</EmptyText>
      ) : (
        <div className="flex items-end justify-between gap-5">
          <div className="flex min-w-0 flex-col gap-1">
            <div className="flex items-baseline gap-1.5">
              <NumberFlow
                value={totalRuns}
                locales={locales}
                format={{ maximumFractionDigits: 0 }}
                aria-label={String(totalRuns)}
                className="text-3xl font-bold leading-none"
              />
              <span className="text-sm text-muted-foreground">
                {t(($) => $.tab_body.activity.runs, { count: totalRuns })}
              </span>
            </div>
            <div className="text-xs text-muted-foreground">
              {t(($) => $.tab_body.activity.success_pct, { percent: successPct })}
              {avgDurationMs > 0 && (
                <>
                  <Sep />
                  <span>{t(($) => $.tab_body.activity.avg_duration, { value: formatDurationMs(avgDurationMs) })}</span>
                </>
              )}
              {totalFailed > 0 && (
                <>
                  <Sep />
                  <span className="text-destructive">
                    {t(($) => $.tab_body.activity.failed_count, { count: totalFailed })}
                  </span>
                </>
              )}
            </div>
          </div>
          {/* Garnish, not hero — small enough that a sparse 30-day series
              doesn't read as visually broken. Bottom-aligned with the
              number so the dense end of the bars sits on the same
              baseline as the digits. */}
          <Sparkline
            buckets={summary.buckets}
            width={120}
            height={32}
            className="shrink-0"
          />
        </div>
      )}
    </Section>
  );
}

function RecentWorkSection({
  tasks,
  totalCount,
  hasMore,
  loading,
  onShowMore,
  issueMap,
  agent,
}: {
  tasks: AgentTask[];
  totalCount: number;
  hasMore: boolean;
  loading: boolean;
  onShowMore: () => void;
  issueMap: Map<string, Issue>;
  agent: Agent;
}) {
  const { t } = useT("agents");
  // While the first fetch is in flight we have no counts to summarise, so
  // the subtitle stays blank rather than claiming "nothing finished yet".
  const subtitle = loading
    ? ""
    : tasks.length === 0
      ? t(($) => $.tab_body.activity.subtitle_no_recent)
      : totalCount > tasks.length
        ? t(($) => $.tab_body.activity.subtitle_recent_progress, { shown: tasks.length, total: totalCount })
        : t(($) => $.tab_body.activity.subtitle_recent_latest, { count: tasks.length });
  return (
    <Section title={t(($) => $.tab_body.activity.section_recent)} subtitle={subtitle}>
      {loading ? (
        <RecentWorkSkeleton />
      ) : tasks.length === 0 ? (
        <EmptyText>{t(($) => $.tab_body.activity.empty_recent)}</EmptyText>
      ) : (
        <>
          <TaskList
            tasks={tasks}
            issueMap={issueMap}
            timeMode="completed"
            agent={agent}
          />
          {hasMore && (
            <button
              type="button"
              onClick={onShowMore}
              className="mt-2 self-start rounded text-xs text-muted-foreground transition-colors hover:text-foreground"
            >
              {t(($) => $.tab_body.activity.show_more)}
            </button>
          )}
        </>
      )}
    </Section>
  );
}

/**
 * Loading placeholder for the Recent work list. Mirrors the completed-mode
 * TaskList shell (bordered, divided rows) and the two-line TaskRow rhythm —
 * a status glyph plus title and meta line — so the skeleton settles into the
 * real rows without a layout jump. Widths are staggered per row so it reads
 * as content rather than a solid block.
 */
function RecentWorkSkeleton() {
  const titleWidths = ["w-3/5", "w-4/5", "w-1/2", "w-2/3"];
  const metaWidths = ["w-2/5", "w-1/3", "w-2/5", "w-1/4"];
  return (
    <div
      className="overflow-hidden rounded-lg border divide-y"
      aria-hidden="true"
    >
      {Array.from({ length: RECENT_SKELETON_ROWS }).map((_, i) => (
        <div key={i} className="flex items-center gap-3 px-3 py-3">
          <Skeleton className="h-4 w-4 shrink-0 rounded-full" />
          <div className="min-w-0 flex-1 space-y-2">
            <Skeleton className={`h-3.5 ${titleWidths[i % titleWidths.length]}`} />
            <Skeleton className={`h-3 ${metaWidths[i % metaWidths.length]}`} />
          </div>
        </div>
      ))}
    </div>
  );
}

function TaskList({
  tasks,
  issueMap,
  timeMode,
  agent,
}: {
  tasks: AgentTask[];
  issueMap: Map<string, Issue>;
  timeMode: "active" | "completed";
  agent: Agent;
}) {
  return (
    <div
      className={
        timeMode === "completed"
          ? "overflow-hidden rounded-lg border divide-y"
          : "space-y-2"
      }
    >
      {tasks.map((task) => (
        <TaskRow
          key={task.id}
          task={task}
          issueMap={issueMap}
          timeMode={timeMode}
          agent={agent}
        />
      ))}
    </div>
  );
}

function TaskRow({
  task,
  issueMap,
  timeMode,
  agent,
}: {
  task: AgentTask;
  issueMap: Map<string, Issue>;
  timeMode: "active" | "completed";
  agent: Agent;
}) {
  const { t } = useT("agents");
  const timeAgo = useTimeAgo();
  const paths = useWorkspacePaths();
  const [cancelling, setCancelling] = useState(false);
  const cfg = taskStatusConfig[task.status] ?? taskStatusConfig.queued!;
  const Icon = cfg.icon;
  const hasIssue = task.issue_id !== "";
  const issue = hasIssue ? issueMap.get(task.issue_id) : undefined;
  const isRunning = task.status === "running";
  // Queued tasks have no messages yet — hiding the transcript button avoids
  // a guaranteed "No execution data recorded." dialog open.
  const showTranscript = task.status !== "queued";
  // Cancel only makes sense for the three active states. Terminal rows
  // (completed / failed / cancelled) hide the button entirely.
  const showCancel =
    timeMode === "active" &&
    (task.status === "queued" ||
      task.status === "dispatched" ||
      task.status === "running");

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

  const timeText =
    timeMode === "active"
      ? activeTaskTimeText(task, t, timeAgo)
      : task.completed_at
        ? timeAgo(task.completed_at)
        : "—";

  // Failure reason. The back-end emits "" on non-failed tasks (omitempty
  // strips it on the wire) so the truthy guard is the right shape; the
  // cast is safe because the back-end only emits one of the enum values.
  const failureLabel =
    task.status === "failed" && task.failure_reason
      ? failureReasonLabel[task.failure_reason as TaskFailureReason]
      : null;

  // Only show duration for terminal rows. An active row's duration is
  // inferred from the timeText already ("Started 2m ago") and adding a
  // second time bubble next to it just clutters the line.
  let durationText: string | null = null;
  if (timeMode === "completed" && task.started_at && task.completed_at) {
    const dur =
      new Date(task.completed_at).getTime() -
      new Date(task.started_at).getTime();
    if (dur > 0) durationText = formatDurationMs(dur);
  }

  const rowClass =
    timeMode === "completed"
      ? "group flex items-center gap-3 px-3 py-3 transition-colors hover:bg-muted/30"
      : `group flex items-center gap-3 rounded-md border px-3 py-3 ${
          isRunning ? "border-brand/40 bg-brand/5" : ""
        }`;

  return (
    <div className={rowClass}>
      <Icon
        className={`h-4 w-4 shrink-0 ${cfg.color} ${
          isRunning ? "animate-spin motion-reduce:animate-none" : ""
        }`}
        aria-hidden="true"
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <SourceIcon
            className="h-3 w-3 shrink-0 text-muted-foreground/70"
            aria-label={sourceLabel}
          />
          {issue && (
            <span className="shrink-0 font-mono text-xs text-muted-foreground">
              {issue.identifier}
            </span>
          )}
          {task.trigger_summary ? (
            // Hover surfaces "why this task ran" — the snapshot lets the
            // agent-side row stay anchored on issue.title (the
            // identification axis here) while still letting the user
            // dwell to see the trigger context. Same pattern as
            // GitHub Actions surfacing the commit message on hover.
            <Tooltip>
              <TooltipTrigger
                render={
                  <span className="truncate text-sm">
                    {issue?.title ??
                      (hasIssue
                        ? t(($) => $.tab_body.activity.issue_short_fallback, { prefix: task.issue_id.slice(0, 8) })
                        : (sourceFallback ?? t(($) => $.tab_body.activity.source_untracked)))}
                  </span>
                }
              />
              <TooltipContent className="max-w-md">
                <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground/80">
                  {t(($) => $.tab_body.activity.triggered_by)}
                </div>
                <div className="mt-0.5 whitespace-pre-wrap text-xs">
                  {task.trigger_summary}
                </div>
              </TooltipContent>
            </Tooltip>
          ) : (
            <span className="truncate text-sm">
              {issue?.title ??
                (hasIssue
                  ? t(($) => $.tab_body.activity.issue_short_fallback, { prefix: task.issue_id.slice(0, 8) })
                  : (sourceFallback ?? t(($) => $.tab_body.activity.source_untracked)))}
            </span>
          )}
        </div>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-muted-foreground">
          <span className={cfg.color}>
            {taskStatusLabel(task.status, t)}
          </span>
          <Sep />
          <span>{timeText}</span>
          {durationText && (
            <>
              <Sep />
              <span>{durationText}</span>
            </>
          )}
          {failureLabel && (
            <>
              <Sep />
              <span className="text-destructive">{failureLabel}</span>
            </>
          )}
          {/* Accountable member (MUL-4302 §9): whose behalf this run is on.
              A leading separator keeps the avatar on the same middot rhythm as
              the rest of the meta line instead of glued to the duration. The
              guard mirrors the badge's own render condition (avatar-only needs
              an initiator) so no dangling separator is left for an
              unattributed run. */}
          {task.attribution?.initiator && (
            <>
              <Sep />
              <AttributionBadge
                attribution={task.attribution}
                variant="avatar"
              />
            </>
          )}
        </div>
      </div>

      {/* Hover-only actions. The row is intentionally non-clickable so
          neither destination is privileged — issue detail and transcript
          are equally valid follow-ups. focus-within keeps the slot
          reachable for keyboard users. */}
      <div className="ml-2 flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity duration-100 group-hover:opacity-100 group-focus-within:opacity-100">
        {hasIssue && (
          <Tooltip>
            <TooltipTrigger
              render={<AppLink href={paths.issueDetail(task.issue_id)} />}
              aria-label={t(($) => $.tab_body.activity.open_issue_aria)}
              className="flex items-center justify-center rounded p-1 text-muted-foreground hover:text-foreground hover:bg-accent/50 transition-colors"
            >
              <ArrowUpRight className="h-3.5 w-3.5" aria-hidden="true" />
            </TooltipTrigger>
            <TooltipContent>{t(($) => $.tab_body.activity.open_issue_tooltip)}</TooltipContent>
          </Tooltip>
        )}
        {showTranscript && (
          <TranscriptButton
            task={task}
            agentName={agent.name}
            isLive={isRunning}
            title={t(($) => $.tab_body.activity.transcript_tooltip)}
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
              className="flex items-center justify-center rounded p-1 text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive disabled:cursor-not-allowed disabled:opacity-50"
            >
              <X className="h-3.5 w-3.5" aria-hidden="true" />
            </TooltipTrigger>
            <TooltipContent>
              {cancelling ? t(($) => $.tab_body.activity.cancelling_tooltip) : t(($) => $.tab_body.activity.cancel_task_tooltip)}
            </TooltipContent>
          </Tooltip>
        )}
      </div>
    </div>
  );
}

function Section({
  title,
  subtitle,
  children,
}: {
  title: string;
  subtitle: string;
  children: ReactNode;
}) {
  return (
    <section className="flex flex-col gap-3 border-b pb-6 last:border-b-0 last:pb-0">
      <div className="flex items-baseline gap-2">
        <h2 className="text-sm font-semibold text-foreground">
          {title}
        </h2>
        <span className="text-[11px] text-muted-foreground/70">{subtitle}</span>
      </div>
      {children}
    </section>
  );
}

function EmptyText({ children }: { children: ReactNode }) {
  return <p className="text-xs italic text-muted-foreground/60">{children}</p>;
}

function Sep() {
  // mx-1 puts visible whitespace around the dot; without it inline JSX
  // collapses neighbouring tokens to "100% success·avg 30s" which reads
  // as "successdotavg" at a glance.
  return <span className="mx-1 text-muted-foreground/40">·</span>;
}

type AgentsT = ReturnType<typeof useT<"agents">>["t"];
type TimeAgoFn = (dateStr: string) => string;

function taskStatusLabel(status: AgentTask["status"], t: AgentsT): string {
  switch (status) {
    case "queued":
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
 * Average wall-clock duration of completed/failed tasks whose completion
 * lands in the last 30 days. Pure function so callers can pass a
 * deterministic `now` in tests.
 */
export function deriveAvgDurationLast30d(
  tasks: readonly AgentTask[],
  now: number,
): number {
  let sum = 0;
  let count = 0;
  for (const t of tasks) {
    if (!t.completed_at || !t.started_at) continue;
    const completedAt = new Date(t.completed_at).getTime();
    if (Number.isNaN(completedAt)) continue;
    if (now - completedAt > THIRTY_DAYS_MS) continue;
    const startedAt = new Date(t.started_at).getTime();
    const dur = completedAt - startedAt;
    if (Number.isFinite(dur) && dur > 0) {
      sum += dur;
      count += 1;
    }
  }
  return count > 0 ? Math.round(sum / count) : 0;
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
