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
import { timeAgo } from "@multica/core/utils";
import { AppLink } from "../../../navigation";
import { TranscriptButton } from "../../../common/task-transcript";
import { taskStatusConfig } from "../../config";
import { failureReasonLabel } from "./task-failure";
import { Sparkline } from "../sparkline";

const THIRTY_DAYS_MS = 30 * 24 * 60 * 60 * 1000;
// Recent work pagination: small initial cohort to keep the section
// scannable, then "Show more" reveals 20 at a time. Tasks are already
// fully cached client-side (one listAgentTasks for the whole agent), so
// "more" is a pure state flip — zero extra fetches.
const RECENT_INITIAL = 5;
const RECENT_PAGE = 20;

interface ActivityTabProps {
  agent: Agent;
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
export function ActivityTab({ agent }: ActivityTabProps) {
  const wsId = useWorkspaceId();

  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));
  const { data: agentTasks = [] } = useQuery(agentTasksOptions(wsId, agent.id));
  const { byAgent: activityMap } = useWorkspaceActivityMap(wsId);
  const activity = activityMap.get(agent.id);

  const [recentDisplayLimit, setRecentDisplayLimit] = useState(RECENT_INITIAL);

  // Chat tasks are intentionally hidden across every Agent-scoped surface
  // (list / detail / activity). They have their own UI in the chat
  // experience; mixing them in here muddies "what is this agent doing
  // for the team" with "what is this agent doing in private chat".
  const isWorkflowTask = (t: AgentTask) => !t.chat_session_id;

  const activeTasks = useMemo(() => {
    return snapshot.filter(
      (t) =>
        t.agent_id === agent.id &&
        isWorkflowTask(t) &&
        (t.status === "running" ||
          t.status === "queued" ||
          t.status === "dispatched"),
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
    <div className="flex flex-col gap-4 p-6">
      <NowSection tasks={activeTasks} issueMap={issueMap} agent={agent} />
      <Last30dSection activity={activity} avgDurationMs={avgDurationMs} />
      <RecentWorkSection
        tasks={recentTasks}
        totalCount={recentTasksAll.length}
        hasMore={hasMoreRecent}
        onShowMore={() =>
          setRecentDisplayLimit((n) => n + RECENT_PAGE)
        }
        issueMap={issueMap}
        agent={agent}
      />
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
  return (
    <Section
      title="Now"
      subtitle={
        tasks.length === 0
          ? "No active work"
          : `${tasks.length} active task${tasks.length === 1 ? "" : "s"}`
      }
    >
      {tasks.length === 0 ? (
        <EmptyText>This agent isn&apos;t running anything right now.</EmptyText>
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
  const summary = summarizeActivityWindow(activity, 30);
  const { totalRuns, totalFailed } = summary;
  const successPct =
    totalRuns > 0
      ? Math.round(((totalRuns - totalFailed) / totalRuns) * 100)
      : 100;

  return (
    <Section title="Last 30 days" subtitle="Performance">
      {totalRuns === 0 ? (
        <EmptyText>No completions in the last 30 days.</EmptyText>
      ) : (
        // Layout: number is the hero, sparkline is a garnish on the
        // right. Reversed from "chart hero + tiny number" because at
        // detail-page sample sizes (often <30 over the window) a wide
        // sparkline reads as mostly-empty space, which looks broken;
        // the headline number stays legible at any sample size and
        // anchors the section. Stripe / Vercel / Shopify metric cards
        // all follow this pattern for the same reason.
        <div className="flex items-end justify-between gap-5">
          <div className="flex min-w-0 flex-col gap-1">
            <div className="flex items-baseline gap-1.5">
              <span className="text-3xl font-bold leading-none tabular-nums">
                {totalRuns}
              </span>
              <span className="text-sm text-muted-foreground">
                run{totalRuns === 1 ? "" : "s"}
              </span>
            </div>
            <div className="text-xs text-muted-foreground">
              {successPct}% success
              {avgDurationMs > 0 && (
                <>
                  <Sep />
                  <span>avg {formatDurationMs(avgDurationMs)}</span>
                </>
              )}
              {totalFailed > 0 && (
                <>
                  <Sep />
                  <span className="text-destructive">
                    {totalFailed} failed
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
  onShowMore,
  issueMap,
  agent,
}: {
  tasks: AgentTask[];
  totalCount: number;
  hasMore: boolean;
  onShowMore: () => void;
  issueMap: Map<string, Issue>;
  agent: Agent;
}) {
  // Subtitle phrasing: "5 of 47" once we know the total is bigger than
  // what we're rendering, otherwise "5 latest". Total comes from
  // recentTasksAll (already filtered for chat / terminals) so it
  // accurately reflects what would appear if the user kept clicking
  // "Show more" — not the raw on-the-wire row count.
  const subtitle =
    tasks.length === 0
      ? "Nothing finished yet"
      : totalCount > tasks.length
        ? `${tasks.length} of ${totalCount}`
        : `${tasks.length} latest`;
  return (
    <Section title="Recent work" subtitle={subtitle}>
      {tasks.length === 0 ? (
        <EmptyText>This agent hasn&apos;t completed anything yet.</EmptyText>
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
              Show more →
            </button>
          )}
        </>
      )}
    </Section>
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
    <div className="space-y-1.5">
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
      toast.error(e instanceof Error ? e.message : "Failed to cancel task");
      setCancelling(false);
    }
  };

  // Terminal states never use active wording. The server links the new
  // issue back to a quick-create task on completion (so most successful
  // rows transition to kind=direct on next refetch), but rows whose link
  // write failed — or whose agent never created the issue at all — would
  // otherwise sit on "Creating issue" forever.
  const isTerminalStatus =
    task.status === "completed" ||
    task.status === "failed" ||
    task.status === "cancelled";
  const sourceFallback = !hasIssue
    ? task.kind === "quick_create"
      ? isTerminalStatus
        ? "Quick create"
        : "Creating issue"
      : task.chat_session_id
        ? "Chat session"
        : task.autopilot_run_id
          ? "Autopilot run"
          : "Untracked"
    : null;

  // Origin marker — issue / chat / autopilot / untracked. The issue
  // identifier alone is technically enough for the issue case, but
  // pairing it with a Hash icon keeps the four sources visually aligned
  // so the row reads at a glance instead of requiring a parse.
  const SourceIcon = hasIssue
    ? Hash
    : task.chat_session_id
      ? MessageSquare
      : task.autopilot_run_id
        ? Workflow
        : CircleHelp;
  const sourceLabel = hasIssue
    ? "Issue"
    : task.chat_session_id
      ? "Chat"
      : task.autopilot_run_id
        ? "Autopilot"
        : "Untracked";

  const timeText =
    timeMode === "active"
      ? activeTaskTimeText(task)
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

  const rowClass = `group flex items-center gap-3 rounded-md border px-3 py-2.5 ${
    isRunning ? "border-brand/40 bg-brand/5" : ""
  }`;

  return (
    <div className={rowClass}>
      <Icon
        className={`h-4 w-4 shrink-0 ${cfg.color} ${
          isRunning ? "animate-spin" : ""
        }`}
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
                        ? `Issue ${task.issue_id.slice(0, 8)}…`
                        : (sourceFallback ?? "Untracked"))}
                  </span>
                }
              />
              <TooltipContent className="max-w-md">
                <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground/80">
                  Triggered by
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
                  ? `Issue ${task.issue_id.slice(0, 8)}…`
                  : (sourceFallback ?? "Untracked"))}
            </span>
          )}
        </div>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-xs text-muted-foreground">
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
              aria-label="Open issue"
              className="flex items-center justify-center rounded p-1 text-muted-foreground hover:text-foreground hover:bg-accent/50 transition-colors"
            >
              <ArrowUpRight className="h-3.5 w-3.5" />
            </TooltipTrigger>
            <TooltipContent>Open issue</TooltipContent>
          </Tooltip>
        )}
        {showTranscript && (
          <TranscriptButton
            task={task}
            agentName={agent.name}
            isLive={isRunning}
            title="View transcript"
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
                  aria-label="Cancel task"
                />
              }
              className="flex items-center justify-center rounded p-1 text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive disabled:cursor-not-allowed disabled:opacity-50"
            >
              <X className="h-3.5 w-3.5" />
            </TooltipTrigger>
            <TooltipContent>
              {cancelling ? "Cancelling…" : "Cancel task"}
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
    <section className="flex flex-col gap-3 rounded-lg border bg-background p-5">
      <div className="flex items-baseline gap-2">
        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
          {title}
        </h3>
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

function activeTaskTimeText(task: AgentTask): string {
  if (task.status === "running" && task.started_at) {
    return `Started ${timeAgo(task.started_at)}`;
  }
  if (task.status === "dispatched" && task.dispatched_at) {
    return `Dispatched ${timeAgo(task.dispatched_at)}`;
  }
  return `Queued ${timeAgo(task.created_at)}`;
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
