"use client";

import { useMemo, useState } from "react";
import {
  BarChart3,
  Bot,
  CheckCircle2,
  Clock3,
  FolderKanban,
  Loader2,
  RadioTower,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import {
  summarizeActivityWindow,
  useWorkspaceActivityMap,
  useWorkspacePresenceMap,
  type AgentActivity,
  type AgentAvailability,
  type AgentPresenceDetail,
  type Workload,
} from "@multica/core/agents";
import {
  dashboardUsageDailyOptions,
  dashboardUsageByAgentOptions,
  dashboardAgentRunTimeOptions,
  dashboardRunTimeDailyOptions,
} from "@multica/core/dashboard";
import { runtimeListOptions } from "@multica/core/runtimes";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";
import type { Agent, AgentRuntime, Project } from "@multica/core/types";
import { useViewingTimezone } from "../../common/use-viewing-timezone";
import { PageHeader } from "../../layout/page-header";
import { KpiCard } from "../../runtimes/components/shared";
import {
  DailyCostChart,
  DailyTokensChart,
  DailyTimeChart,
  DailyTasksChart,
  WeeklyCostChart,
  WeeklyTokensChart,
  WeeklyTimeChart,
  WeeklyTasksChart,
} from "../../runtimes/components/charts";
import { ProjectIcon } from "../../projects/components/project-icon";
import { ActorAvatar } from "../../common/actor-avatar";
import { Sparkline } from "../../agents/components/sparkline";
import {
  addDaysIso,
  aggregateByWeek,
  formatTokens,
  todayIso,
} from "../../runtimes/utils";
import { useT } from "../../i18n";
import {
  aggregateAgentTokens,
  aggregateDailyCost,
  aggregateDailyTasks,
  aggregateDailyTime,
  aggregateDailyTokens,
  aggregateWeeklyTasks,
  aggregateWeeklyTime,
  computeDailyTotals,
  formatDuration,
  mergeAgentDashboardRows,
  type AgentDashboardRow,
} from "../utils";

// Period selector — mirrors the runtime detail page so users see the same
// option set across both dashboards. `dims` declares which dimensions each
// range is allowed in: 1d / 7d at the weekly grain collapse to a single bar,
// 180d at the daily grain is 180 unreadable bars, so each end of the range
// belongs to a single dimension. Switching dimensions resets `days` if the
// current value isn't in the new dimension's allowed set (see
// `handleDimChange` below).
//
// 1d semantic: "today" (the natural calendar day from 00:00 in the viewer's
// timezone), not "the last 24 hours". The client-side `dailyCutoffIso` filter
// below enforces this even at the midnight edge.
const TIME_RANGES = [
  { label: "1d", days: 1, dims: ["daily"] as const },
  { label: "7d", days: 7, dims: ["daily"] as const },
  { label: "30d", days: 30, dims: ["daily", "weekly"] as const },
  { label: "90d", days: 90, dims: ["daily", "weekly"] as const },
  { label: "180d", days: 180, dims: ["weekly"] as const },
] as const;
type TimeRange = (typeof TIME_RANGES)[number]["days"];
type Dim = "daily" | "weekly";

const DEFAULT_DAYS_BY_DIM: Record<Dim, TimeRange> = {
  daily: 30,
  weekly: 90,
};

function rangesForDim(dim: Dim) {
  return TIME_RANGES.filter((r) => (r.dims as readonly string[]).includes(dim));
}

// Sentinel for "no project filter" — kept distinct from the empty string
// so it survives a refactor that ever lets a project be slug-keyed.
const ALL_PROJECTS = "__all__";

// Stable references — `data ?? []` would create a new empty array on
// every render while the query is loading, which breaks useMemo's
// reference-equality dep check and trips the exhaustive-deps lint rule.
const EMPTY_DAILY: import("@multica/core/types").DashboardUsageDaily[] = [];
const EMPTY_BY_AGENT: import("@multica/core/types").DashboardUsageByAgent[] = [];
const EMPTY_RUNTIME: import("@multica/core/types").DashboardAgentRunTime[] = [];
const EMPTY_RUNTIME_DAILY: import("@multica/core/types").DashboardRunTimeDaily[] = [];
const EMPTY_AGENTS: Agent[] = [];
const EMPTY_RUNTIMES: AgentRuntime[] = [];
const EMPTY_PROJECTS: Project[] = [];

const AVAILABILITY_DOT_CLASS: Record<AgentAvailability | "loading", string> = {
  online: "bg-success",
  unstable: "bg-warning",
  offline: "bg-muted-foreground/40",
  archived: "bg-muted-foreground/40",
  loading: "bg-muted-foreground/30",
};

const AVAILABILITY_TEXT_CLASS: Record<AgentAvailability | "loading", string> = {
  online: "text-success",
  unstable: "text-warning",
  offline: "text-muted-foreground",
  archived: "text-muted-foreground",
  loading: "text-muted-foreground",
};

const WORKLOAD_TEXT_CLASS: Record<Workload, string> = {
  working: "text-brand",
  queued: "text-warning",
  idle: "text-muted-foreground",
};

interface WorkforceRow {
  agent: Agent;
  runtime: AgentRuntime | null;
  presence: AgentPresenceDetail | null;
  activity: AgentActivity | undefined;
  activityWindow: ReturnType<typeof summarizeActivityWindow>;
  metrics: AgentDashboardRow | null;
  failedCount: number;
}

interface WorkforceSummary {
  total: number;
  archived: number;
  online: number;
  unstable: number;
  offline: number;
  working: number;
  queued: number;
  idle: number;
  runningTasks: number;
  queuedTasks: number;
  capacity: number;
  activeInWindow: number;
}

function fmtMoney(n: number): string {
  if (n >= 100) return `$${n.toFixed(0)}`;
  return `$${n.toFixed(2)}`;
}

function formatPercent(n: number): string {
  return `${Math.round(n)}%`;
}

function activityDaysAgo(activity: AgentActivity | undefined): number | null {
  if (!activity) return null;
  for (let i = activity.buckets.length - 1; i >= 0; i--) {
    const bucket = activity.buckets[i];
    if (bucket && bucket.total > 0) return activity.buckets.length - 1 - i;
  }
  return null;
}

function rowSortScore(row: WorkforceRow): number {
  const presence = row.presence;
  if (!presence) return 50;
  if (presence.workload === "working") return 0;
  if (presence.workload === "queued") return 10;
  if (presence.availability === "online") return 20;
  if (presence.availability === "unstable") return 30;
  return 40;
}

function buildWorkforceSummary(
  rows: WorkforceRow[],
  archived: number,
): WorkforceSummary {
  const summary: WorkforceSummary = {
    total: rows.length,
    archived,
    online: 0,
    unstable: 0,
    offline: 0,
    working: 0,
    queued: 0,
    idle: 0,
    runningTasks: 0,
    queuedTasks: 0,
    capacity: 0,
    activeInWindow: 0,
  };

  for (const row of rows) {
    const p = row.presence;
    if (p) {
      if (p.availability === "online") summary.online += 1;
      else if (p.availability === "unstable") summary.unstable += 1;
      else summary.offline += 1;

      summary[p.workload] += 1;
      summary.runningTasks += p.runningCount;
      summary.queuedTasks += p.queuedCount;
      summary.capacity += p.capacity;
    }
    if (row.activityWindow.totalRuns > 0) summary.activeInWindow += 1;
  }

  return summary;
}

// Local segmented control — same visual language the runtime usage section
// uses for its period / tab toggles. shadcn's Tabs is wired for full tab
// pages with ARIA semantics the compact toolbar pill doesn't need.
function Segmented<T extends string | number>({
  value,
  onChange,
  options,
}: {
  value: T;
  onChange: (v: T) => void;
  options: readonly { label: string; value: T }[];
}) {
  return (
    <div className="inline-flex items-center gap-0.5 rounded-md bg-muted p-0.5">
      {options.map((o) => (
        <button
          key={String(o.value)}
          type="button"
          onClick={() => onChange(o.value)}
          className={`rounded-sm px-2.5 py-1 text-xs font-medium transition-colors ${
            o.value === value
              ? "bg-background text-foreground shadow-sm"
              : "text-muted-foreground hover:text-foreground"
          }`}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

/**
 * Workspace + project token / run-time dashboard.
 *
 * Lives at `/{slug}/dashboard`. Three independent rollups (daily cost,
 * per-agent tokens, per-agent run-time) feed four KPI tiles, a daily cost
 * chart, and a combined "by agent" list. A project dropdown narrows every
 * query to one project; the period selector applies to all three.
 *
 * Cost math runs client-side via the runtimes utils — keeps the dashboard
 * and the runtime page using one pricing table.
 */
export function DashboardPage() {
  const { t } = useT("usage");
  const wsId = useWorkspaceId();
  const viewTZ = useViewingTimezone();
  const [dim, setDim] = useState<Dim>("daily");
  const [days, setDays] = useState<TimeRange>(30);
  const [projectValue, setProjectValue] = useState<string>(ALL_PROJECTS);

  const allowedRanges = rangesForDim(dim);
  const handleDimChange = (next: Dim) => {
    setDim(next);
    const stillAllowed = (rangesForDim(next) as readonly { days: number }[]).some(
      (r) => r.days === days,
    );
    if (!stillAllowed) setDays(DEFAULT_DAYS_BY_DIM[next]);
  };

  // The user can save model prices from the runtimes page; re-render when
  // they do so the dashboard reflects the new rates.
  useCustomPricingStore((s) => s.pricings);

  const projectsQuery = useQuery(projectListOptions(wsId));
  const agentsQuery = useQuery(agentListOptions(wsId));
  const runtimesQuery = useQuery(runtimeListOptions(wsId));
  const { byAgent: presenceMap, loading: presenceLoading } =
    useWorkspacePresenceMap(wsId);
  const { byAgent: activityMap, loading: activityLoading } =
    useWorkspaceActivityMap(wsId);
  const projects = projectsQuery.data ?? EMPTY_PROJECTS;
  const agents = agentsQuery.data ?? EMPTY_AGENTS;
  const runtimes = runtimesQuery.data ?? EMPTY_RUNTIMES;
  const activeAgents = useMemo(
    () => agents.filter((a) => !a.archived_at),
    [agents],
  );
  const archivedCount = agents.length - activeAgents.length;

  // Validate the picked project against the current workspace's list. A
  // stale UUID — left over from a project that's been deleted, or from the
  // previous workspace after a switch — would silently filter all three
  // queries to empty rows while the dropdown still reads "All projects".
  // Derive the effective filter so the API call matches the user-visible
  // selection.
  const projectId = useMemo(() => {
    if (projectValue === ALL_PROJECTS) return null;
    return projects.some((p) => p.id === projectValue) ? projectValue : null;
  }, [projectValue, projects]);

  // The weekly chart paints `ceil(days / 7)` trailing calendar weeks anchored
  // at today-in-UTC. In the worst case (today = Sunday) the leftmost Monday
  // sits `weekCount * 7 - 1` days back, so a vanilla `days=30` request would
  // silently truncate the leftmost bucket. Over-fetch the per-date queries
  // to cover the full first week; the per-agent rollups stay at `days` so
  // KPI/leaderboard labels (e.g. "Tasks · 30D") keep their advertised window.
  const weekCount = Math.max(1, Math.ceil(days / 7));
  const chartFetchDays = dim === "weekly" ? weekCount * 7 : days;

  const dailyQuery = useQuery(
    dashboardUsageDailyOptions(wsId, chartFetchDays, projectId, viewTZ),
  );
  const byAgentQuery = useQuery(
    dashboardUsageByAgentOptions(wsId, days, projectId, viewTZ),
  );
  const runTimeQuery = useQuery(
    dashboardAgentRunTimeOptions(wsId, days, projectId, viewTZ),
  );
  const runTimeDailyQuery = useQuery(
    dashboardRunTimeDailyOptions(wsId, chartFetchDays, projectId, viewTZ),
  );

  const dailyUsage = dailyQuery.data ?? EMPTY_DAILY;
  const byAgentUsage = byAgentQuery.data ?? EMPTY_BY_AGENT;
  const runTimeRows = runTimeQuery.data ?? EMPTY_RUNTIME;
  const runTimeDailyRows = runTimeDailyQuery.data ?? EMPTY_RUNTIME_DAILY;

  // Daily-aggregation surfaces (cost/tokens/time/tasks KPIs and the Daily
  // trend chart) re-scope to the user-selected `days` even when we
  // over-fetched for the weekly chart. The cutoff is anchored on the viewer's
  // timezone — the same axis the backend slices `bucket_hour` on — so it
  // lands on the same calendar boundary. Applied in both dims so 1d strictly
  // means "today" even at the midnight edge where a wall-clock cutoff would
  // otherwise include yesterday.
  const dailyCutoffIso = useMemo(
    () => addDaysIso(todayIso(viewTZ), -(days - 1)),
    [days, viewTZ],
  );
  const dailyUsageInWindow = useMemo(
    () => dailyUsage.filter((u) => u.date >= dailyCutoffIso),
    [dailyUsage, dailyCutoffIso],
  );
  const runTimeDailyInWindow = useMemo(
    () => runTimeDailyRows.filter((r) => r.date >= dailyCutoffIso),
    [runTimeDailyRows, dailyCutoffIso],
  );

  const isLoading =
    agentsQuery.isLoading ||
    runtimesQuery.isLoading ||
    presenceLoading ||
    activityLoading ||
    dailyQuery.isLoading ||
    byAgentQuery.isLoading ||
    runTimeQuery.isLoading ||
    runTimeDailyQuery.isLoading;

  // Empty only when there are no active agents AND all rollups are empty.
  // A workspace with configured digital employees but no recent tasks still
  // deserves an operations board; it is an idle fleet, not a blank dashboard.
  const hasNoData =
    !isLoading &&
    activeAgents.length === 0 &&
    dailyUsage.length === 0 &&
    byAgentUsage.length === 0 &&
    runTimeRows.length === 0 &&
    runTimeDailyRows.length === 0;

  // Cost / token math — re-derived when usage, days, or pricings change.
  const totals = useMemo(
    () => computeDailyTotals(dailyUsageInWindow),
    [dailyUsageInWindow],
  );
  const dailyCost = useMemo(
    () => aggregateDailyCost(dailyUsageInWindow),
    [dailyUsageInWindow],
  );
  const dailyTokens = useMemo(
    () => aggregateDailyTokens(dailyUsageInWindow),
    [dailyUsageInWindow],
  );
  const dailyTime = useMemo(
    () => aggregateDailyTime(runTimeDailyInWindow),
    [runTimeDailyInWindow],
  );
  const dailyTasks = useMemo(
    () => aggregateDailyTasks(runTimeDailyInWindow),
    [runTimeDailyInWindow],
  );

  // Weekly aggregates — built from the over-fetched per-date queries so the
  // leftmost trailing week always has data even when the user-selected `days`
  // (e.g. 30D) is shorter than the chart's `weekCount * 7` span. Buckets are
  // pre-zeroed inside the helpers, so sparse weeks render as empty bars
  // instead of being dropped (MUL-2382 weekly window scoping). Week
  // boundaries follow the viewer's timezone.
  const weekly = useMemo(
    () => aggregateByWeek(dailyUsage, viewTZ, weekCount),
    [dailyUsage, viewTZ, weekCount],
  );
  const weeklyCost = weekly.weeklyCostStack;
  const weeklyTokens = weekly.weeklyTokens;
  const weeklyTime = useMemo(
    () => aggregateWeeklyTime(runTimeDailyRows, viewTZ, weekCount),
    [runTimeDailyRows, viewTZ, weekCount],
  );
  const weeklyTasks = useMemo(
    () => aggregateWeeklyTasks(runTimeDailyRows, viewTZ, weekCount),
    [runTimeDailyRows, viewTZ, weekCount],
  );
  const agentTokenRows = useMemo(
    () => aggregateAgentTokens(byAgentUsage),
    [byAgentUsage],
  );

  // Run-time totals — taskCount + failedCount summed for the KPI row.
  const runTimeTotals = useMemo(() => {
    let totalSeconds = 0;
    let taskCount = 0;
    let failedCount = 0;
    for (const r of runTimeRows) {
      totalSeconds += r.total_seconds;
      taskCount += r.task_count;
      failedCount += r.failed_count;
    }
    return { totalSeconds, taskCount, failedCount };
  }, [runTimeRows]);

  const agentRows = useMemo(
    () => mergeAgentDashboardRows(agentTokenRows, runTimeRows),
    [agentTokenRows, runTimeRows],
  );
  const runtimesById = useMemo(
    () => new Map(runtimes.map((r) => [r.id, r] as const)),
    [runtimes],
  );
  const metricsByAgent = useMemo(
    () => new Map(agentRows.map((r) => [r.agentId, r] as const)),
    [agentRows],
  );
  const failuresByAgent = useMemo(
    () => new Map(runTimeRows.map((r) => [r.agent_id, r.failed_count] as const)),
    [runTimeRows],
  );
  const activityWindowDays = Math.min(days, 30);
  const workforceRows = useMemo(() => {
    return activeAgents
      .map<WorkforceRow>((agent) => {
        const activity = activityMap.get(agent.id);
        return {
          agent,
          runtime: runtimesById.get(agent.runtime_id) ?? null,
          presence: presenceMap.get(agent.id) ?? null,
          activity,
          activityWindow: summarizeActivityWindow(activity, activityWindowDays),
          metrics: metricsByAgent.get(agent.id) ?? null,
          failedCount: failuresByAgent.get(agent.id) ?? 0,
        };
      })
      .toSorted((a, b) => {
        const score = rowSortScore(a) - rowSortScore(b);
        if (score !== 0) return score;
        const taskDelta =
          (b.metrics?.taskCount ?? 0) - (a.metrics?.taskCount ?? 0);
        if (taskDelta !== 0) return taskDelta;
        return b.activityWindow.totalRuns - a.activityWindow.totalRuns;
      });
  }, [
    activeAgents,
    activityMap,
    activityWindowDays,
    failuresByAgent,
    metricsByAgent,
    presenceMap,
    runtimesById,
  ]);
  const workforceSummary = useMemo(
    () => buildWorkforceSummary(workforceRows, archivedCount),
    [workforceRows, archivedCount],
  );

  return (
    <div className="flex h-full flex-col">
      {/* h-auto + min-h-12 + flex-wrap: the toolbar (project filter,
          dimension switch, range switch) wraps on narrow viewports so every
          control stays reachable. Wider viewports still render the original
          single row. */}
      <PageHeader className="h-auto min-h-12 flex-wrap justify-between gap-y-1.5 px-5 py-1.5 sm:py-0">
        <div className="flex min-w-0 items-center gap-2">
          <BarChart3 className="h-4 w-4 shrink-0 text-muted-foreground" />
          <h1 className="truncate text-sm font-medium">{t(($) => $.title)}</h1>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <ProjectFilter
            projects={projects}
            value={projectValue}
            onChange={setProjectValue}
          />
          <Segmented
            value={dim}
            onChange={handleDimChange}
            options={[
              { label: t(($) => $.dim.daily), value: "daily" as const },
              { label: t(($) => $.dim.weekly), value: "weekly" as const },
            ]}
          />
          <Segmented
            value={days}
            onChange={setDays}
            options={allowedRanges.map((r) => ({ label: r.label, value: r.days }))}
          />
        </div>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-6xl space-y-5 p-6">
          <p className="text-xs text-muted-foreground">{t(($) => $.subtitle)}</p>

          {isLoading ? (
            <DashboardSkeleton />
          ) : hasNoData ? (
            <DashboardEmpty />
          ) : (
            <>
              <WorkforceKpis
                summary={workforceSummary}
                totals={totals}
                runTimeTotals={runTimeTotals}
                days={days}
                lessThanMinuteLabel={t(($) => $.duration.less_than_minute)}
              />

              <WorkforceBoard
                rows={workforceRows}
                summary={workforceSummary}
                days={days}
                activityWindowDays={activityWindowDays}
                lessThanMinuteLabel={t(($) => $.duration.less_than_minute)}
              />

              <section className="space-y-3">
                <div className="flex items-center gap-2">
                  <BarChart3 className="h-4 w-4 text-muted-foreground" />
                  <h2 className="text-sm font-semibold">
                    {t(($) => $.workforce.usage_detail_title)}
                  </h2>
                </div>
                <TrendBlock
                  dim={dim}
                  dailyCost={dailyCost}
                  dailyTokens={dailyTokens}
                  dailyTime={dailyTime}
                  dailyTasks={dailyTasks}
                  weeklyCost={weeklyCost}
                  weeklyTokens={weeklyTokens}
                  weeklyTime={weeklyTime}
                  weeklyTasks={weeklyTasks}
                  lessThanMinuteLabel={t(($) => $.duration.less_than_minute)}
                />
                <Leaderboard
                  rows={agentRows}
                  agents={agents}
                  lessThanMinuteLabel={t(($) => $.duration.less_than_minute)}
                />
              </section>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function WorkforceKpis({
  summary,
  totals,
  runTimeTotals,
  days,
  lessThanMinuteLabel,
}: {
  summary: WorkforceSummary;
  totals: ReturnType<typeof computeDailyTotals>;
  runTimeTotals: { totalSeconds: number; taskCount: number; failedCount: number };
  days: TimeRange;
  lessThanMinuteLabel: string;
}) {
  const { t } = useT("usage");
  const totalTokens =
    totals.input + totals.output + totals.cacheRead + totals.cacheWrite;
  return (
    <div className="grid grid-cols-1 divide-y rounded-lg border bg-card sm:grid-cols-2 sm:divide-x sm:divide-y-0 lg:grid-cols-4">
      <KpiCard
        label={t(($) => $.workforce.kpi.employees)}
        value={String(summary.total)}
        hint={t(($) => $.workforce.kpi.employees_hint, {
          online: summary.online,
          archived: summary.archived,
        })}
      />
      <KpiCard
        label={t(($) => $.workforce.kpi.on_duty)}
        value={String(summary.working + summary.queued)}
        hint={t(($) => $.workforce.kpi.on_duty_hint, {
          running: summary.runningTasks,
          queued: summary.queuedTasks,
        })}
        accent={summary.working > 0 ? "brand" : "default"}
      />
      <KpiCard
        label={t(($) => $.workforce.kpi.tasks, { days })}
        value={String(runTimeTotals.taskCount)}
        hint={t(($) => $.workforce.kpi.tasks_hint, {
          failed: runTimeTotals.failedCount,
          time: formatDuration(runTimeTotals.totalSeconds, lessThanMinuteLabel),
        })}
        accent={runTimeTotals.failedCount === 0 ? "success" : "default"}
      />
      <KpiCard
        label={t(($) => $.workforce.kpi.tokens, { days })}
        value={formatTokens(totalTokens)}
        hint={t(($) => $.workforce.kpi.tokens_hint, {
          cost: fmtMoney(totals.cost),
        })}
      />
    </div>
  );
}

function WorkforceBoard({
  rows,
  summary,
  days,
  activityWindowDays,
  lessThanMinuteLabel,
}: {
  rows: WorkforceRow[];
  summary: WorkforceSummary;
  days: TimeRange;
  activityWindowDays: number;
  lessThanMinuteLabel: string;
}) {
  const { t } = useT("usage");
  const topRows = useMemo(
    () =>
      rows
        .toSorted((a, b) => {
          const taskDelta =
            (b.metrics?.taskCount ?? 0) - (a.metrics?.taskCount ?? 0);
          if (taskDelta !== 0) return taskDelta;
          return b.activityWindow.totalRuns - a.activityWindow.totalRuns;
        })
        .slice(0, 5),
    [rows],
  );

  return (
    <div className="grid gap-5 xl:grid-cols-[minmax(0,1.45fr)_minmax(320px,0.8fr)]">
      <section className="rounded-lg border bg-card">
        <div className="flex flex-wrap items-start justify-between gap-3 border-b px-4 py-3">
          <div>
            <div className="flex items-center gap-2">
              <Bot className="h-4 w-4 text-muted-foreground" />
              <h2 className="text-sm font-semibold">
                {t(($) => $.workforce.board.title)}
              </h2>
            </div>
            <p className="mt-1 text-xs text-muted-foreground">
              {t(($) => $.workforce.board.subtitle, {
                count: rows.length,
                days,
              })}
            </p>
          </div>
          <div className="rounded-md border bg-background px-2.5 py-1.5 text-xs text-muted-foreground">
            {t(($) => $.workforce.board.active_window, {
              count: summary.activeInWindow,
              days: activityWindowDays,
            })}
          </div>
        </div>
        <WorkforceRowsTable
          rows={rows}
          days={days}
          activityWindowDays={activityWindowDays}
          lessThanMinuteLabel={lessThanMinuteLabel}
        />
      </section>

      <div className="space-y-5">
        <StatusDistributionPanel summary={summary} />
        <TopAgentsPanel
          rows={topRows}
          days={days}
          lessThanMinuteLabel={lessThanMinuteLabel}
        />
      </div>
    </div>
  );
}

function WorkforceRowsTable({
  rows,
  days,
  activityWindowDays,
  lessThanMinuteLabel,
}: {
  rows: WorkforceRow[];
  days: TimeRange;
  activityWindowDays: number;
  lessThanMinuteLabel: string;
}) {
  const { t } = useT("usage");
  if (rows.length === 0) {
    return (
      <div className="px-4 py-10 text-center text-xs text-muted-foreground">
        {t(($) => $.workforce.board.no_agents)}
      </div>
    );
  }

  return (
    <div className="overflow-x-auto">
      <div className="min-w-[760px]">
        <div className="grid grid-cols-[minmax(0,1.55fr)_9.5rem_7rem_6rem_6.5rem] items-center gap-3 border-b px-4 py-2 text-xs font-medium text-muted-foreground">
          <span>{t(($) => $.workforce.board.header_agent)}</span>
          <span>{t(($) => $.workforce.board.header_status)}</span>
          <span>{t(($) => $.workforce.board.header_activity)}</span>
          <span className="text-right">
            {t(($) => $.workforce.board.header_tasks, { days })}
          </span>
          <span className="text-right">
            {t(($) => $.workforce.board.header_runtime)}
          </span>
        </div>
        <div className="divide-y">
          {rows.map((row) => {
            const metrics = row.metrics;
            const daysAgo = activityDaysAgo(row.activity);
            const recentLabel =
              daysAgo == null
                ? t(($) => $.workforce.last_active.none)
                : daysAgo === 0
                  ? t(($) => $.workforce.last_active.today)
                  : daysAgo === 1
                    ? t(($) => $.workforce.last_active.yesterday)
                    : t(($) => $.workforce.last_active.days_ago, {
                        days: daysAgo,
                      });
            const taskCount = metrics?.taskCount ?? 0;
            const failedCount = row.failedCount;
            return (
              <div
                key={row.agent.id}
                className="grid grid-cols-[minmax(0,1.55fr)_9.5rem_7rem_6rem_6.5rem] items-center gap-3 px-4 py-3"
              >
                <div className="flex min-w-0 items-center gap-2">
                  <ActorAvatar
                    actorType="agent"
                    actorId={row.agent.id}
                    size={26}
                    enableHoverCard
                  />
                  <div className="min-w-0">
                    <div className="truncate text-sm font-medium">
                      {row.agent.name}
                    </div>
                    <div className="truncate text-xs text-muted-foreground">
                      {row.runtime
                        ? `${row.runtime.provider} · ${row.runtime.name}`
                        : t(($) => $.workforce.board.no_runtime)}
                    </div>
                  </div>
                </div>
                <div className="space-y-1">
                  <AvailabilityBadge availability={row.presence?.availability} />
                  <WorkloadBadge presence={row.presence} />
                </div>
                <div className="space-y-1">
                  <Sparkline
                    buckets={row.activityWindow.buckets}
                    width={84}
                    height={20}
                    className="block"
                  />
                  <div className="text-[11px] text-muted-foreground">
                    {t(($) => $.workforce.board.runs_in_window, {
                      count: row.activityWindow.totalRuns,
                      days: activityWindowDays,
                    })}
                  </div>
                </div>
                <div className="text-right">
                  <div className="text-sm font-medium tabular-nums">
                    {taskCount}
                  </div>
                  <div
                    className={cn(
                      "text-[11px] tabular-nums",
                      failedCount > 0
                        ? "text-destructive"
                        : "text-muted-foreground",
                    )}
                  >
                    {failedCount > 0
                      ? t(($) => $.workforce.board.failed_count, {
                          count: failedCount,
                        })
                      : recentLabel}
                  </div>
                </div>
                <div className="text-right">
                  <div className="text-xs tabular-nums">
                    {formatDuration(metrics?.seconds ?? 0, lessThanMinuteLabel)}
                  </div>
                  <div className="text-[11px] tabular-nums text-muted-foreground">
                    {fmtMoney(metrics?.cost ?? 0)}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

function StatusDistributionPanel({ summary }: { summary: WorkforceSummary }) {
  const { t } = useT("usage");
  const utilization =
    summary.capacity > 0 ? (summary.runningTasks / summary.capacity) * 100 : 0;
  return (
    <section className="rounded-lg border bg-card p-4">
      <div className="mb-4 flex items-center gap-2">
        <RadioTower className="h-4 w-4 text-muted-foreground" />
        <h3 className="text-sm font-semibold">
          {t(($) => $.workforce.status.title)}
        </h3>
      </div>
      <div className="space-y-4">
        <div className="space-y-2">
          <DistributionBar
            label={t(($) => $.workforce.availability.online)}
            value={summary.online}
            total={summary.total}
            fillClassName="bg-success"
          />
          <DistributionBar
            label={t(($) => $.workforce.availability.unstable)}
            value={summary.unstable}
            total={summary.total}
            fillClassName="bg-warning"
          />
          <DistributionBar
            label={t(($) => $.workforce.availability.offline)}
            value={summary.offline}
            total={summary.total}
            fillClassName="bg-muted-foreground/40"
          />
        </div>
        <div className="space-y-2 border-t pt-4">
          <DistributionBar
            label={t(($) => $.workforce.workload.working)}
            value={summary.working}
            total={summary.total}
            fillClassName="bg-brand"
          />
          <DistributionBar
            label={t(($) => $.workforce.workload.queued)}
            value={summary.queued}
            total={summary.total}
            fillClassName="bg-warning"
          />
          <DistributionBar
            label={t(($) => $.workforce.workload.idle)}
            value={summary.idle}
            total={summary.total}
            fillClassName="bg-muted-foreground/40"
          />
        </div>
        <div className="border-t pt-4">
          <div className="mb-2 flex items-center justify-between text-xs">
            <span className="text-muted-foreground">
              {t(($) => $.workforce.status.capacity)}
            </span>
            <span className="font-medium tabular-nums">
              {summary.runningTasks}/{summary.capacity}
            </span>
          </div>
          <div className="h-2 overflow-hidden rounded-full bg-muted">
            <div
              className="h-full rounded-full bg-brand transition-[width] duration-300"
              style={{ width: `${Math.min(100, utilization)}%` }}
            />
          </div>
          <div className="mt-1 text-[11px] text-muted-foreground">
            {t(($) => $.workforce.status.capacity_hint, {
              percent: formatPercent(utilization),
            })}
          </div>
        </div>
      </div>
    </section>
  );
}

function TopAgentsPanel({
  rows,
  days,
  lessThanMinuteLabel,
}: {
  rows: WorkforceRow[];
  days: TimeRange;
  lessThanMinuteLabel: string;
}) {
  const { t } = useT("usage");
  return (
    <section className="rounded-lg border bg-card p-4">
      <div className="mb-3 flex items-center gap-2">
        <CheckCircle2 className="h-4 w-4 text-muted-foreground" />
        <h3 className="text-sm font-semibold">
          {t(($) => $.workforce.top.title, { days })}
        </h3>
      </div>
      {rows.length === 0 ? (
        <div className="py-8 text-center text-xs text-muted-foreground">
          {t(($) => $.workforce.top.no_data)}
        </div>
      ) : (
        <div className="space-y-3">
          {rows.map((row, index) => {
            const tasks = row.metrics?.taskCount ?? 0;
            return (
              <div
                key={row.agent.id}
                className="grid grid-cols-[1.25rem_minmax(0,1fr)_auto] items-center gap-2"
              >
                <div className="font-mono text-xs text-muted-foreground">
                  {index + 1}
                </div>
                <div className="min-w-0">
                  <div className="truncate text-sm font-medium">
                    {row.agent.name}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {formatDuration(row.metrics?.seconds ?? 0, lessThanMinuteLabel)}
                  </div>
                </div>
                <div className="text-right">
                  <div className="text-sm font-medium tabular-nums">{tasks}</div>
                  <div className="text-[11px] text-muted-foreground">
                    {fmtMoney(row.metrics?.cost ?? 0)}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}

function DistributionBar({
  label,
  value,
  total,
  fillClassName,
}: {
  label: string;
  value: number;
  total: number;
  fillClassName: string;
}) {
  const pct = total > 0 ? (value / total) * 100 : 0;
  return (
    <div>
      <div className="mb-1 flex items-center justify-between text-xs">
        <span className="text-muted-foreground">{label}</span>
        <span className="font-medium tabular-nums">{value}</span>
      </div>
      <div className="h-2 overflow-hidden rounded-full bg-muted">
        <div
          className={cn(
            "h-full rounded-full transition-[width] duration-300",
            fillClassName,
          )}
          style={{ width: `${pct}%` }}
        />
      </div>
    </div>
  );
}

function AvailabilityBadge({
  availability,
}: {
  availability: AgentAvailability | undefined;
}) {
  const { t } = useT("usage");
  const key = availability ?? "loading";
  const label =
    key === "online"
      ? t(($) => $.workforce.availability.online)
      : key === "unstable"
        ? t(($) => $.workforce.availability.unstable)
        : key === "archived"
          ? t(($) => $.workforce.availability.archived)
          : key === "offline"
            ? t(($) => $.workforce.availability.offline)
            : t(($) => $.workforce.availability.loading);
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs",
        AVAILABILITY_TEXT_CLASS[key],
      )}
    >
      <span
        className={cn("h-1.5 w-1.5 rounded-full", AVAILABILITY_DOT_CLASS[key])}
      />
      {label}
    </span>
  );
}

function WorkloadBadge({
  presence,
}: {
  presence: AgentPresenceDetail | null;
}) {
  const { t } = useT("usage");
  const workload = presence?.workload ?? "idle";
  const Icon =
    workload === "working" ? Loader2 : workload === "queued" ? Clock3 : CheckCircle2;
  const label =
    workload === "working"
      ? t(($) => $.workforce.workload.working)
      : workload === "queued"
        ? t(($) => $.workforce.workload.queued)
        : t(($) => $.workforce.workload.idle);
  const detail =
    workload === "working"
      ? `${presence?.runningCount ?? 0}/${presence?.capacity ?? 0}`
      : workload === "queued"
        ? String(presence?.queuedCount ?? 0)
        : "";
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 text-xs",
        WORKLOAD_TEXT_CLASS[workload],
      )}
    >
      <Icon
        className={cn("h-3 w-3", workload === "working" && "animate-spin")}
      />
      <span>{label}</span>
      {detail && <span className="tabular-nums text-muted-foreground">{detail}</span>}
    </span>
  );
}

function ProjectFilter({
  projects,
  value,
  onChange,
}: {
  projects: { id: string; title: string; icon: string | null }[];
  value: string;
  onChange: (v: string) => void;
}) {
  const { t } = useT("usage");
  const allLabel = t(($) => $.filter.all_projects);
  const selected = projects.find((p) => p.id === value);
  const selectedTitle =
    value === ALL_PROJECTS ? allLabel : selected?.title ?? allLabel;

  return (
    <Select
      value={value}
      onValueChange={(v) => onChange(v ?? ALL_PROJECTS)}
    >
      <SelectTrigger size="sm" className="min-w-[180px]">
        <SelectValue>
          {() => (
            <>
              {selected ? (
                <ProjectIcon project={selected} size="sm" />
              ) : (
                <FolderKanban className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              )}
              <span className="truncate">{selectedTitle}</span>
            </>
          )}
        </SelectValue>
      </SelectTrigger>
      {/* alignItemWithTrigger=false: the default aligns the *selected* item
          to the trigger, which pushes "All projects" above the trigger and
          clips it off-screen when the usage header sits at the top of the
          viewport. Anchor the dropdown to the bottom of the trigger so
          every entry stays reachable.
          max-h-72: cap the dropdown so a long project list scrolls instead
          of stretching to the bottom of the window. */}
      <SelectContent align="start" alignItemWithTrigger={false} className="max-h-72">
        <SelectItem value={ALL_PROJECTS}>
          <FolderKanban className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">{allLabel}</span>
        </SelectItem>
        {projects.map((p) => (
          <SelectItem key={p.id} value={p.id}>
            <ProjectIcon project={p} size="sm" />
            <span className="truncate">{p.title}</span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

type DailyMetric = "tokens" | "cost" | "time" | "tasks";

function TrendBlock({
  dim,
  dailyCost,
  dailyTokens,
  dailyTime,
  dailyTasks,
  weeklyCost,
  weeklyTokens,
  weeklyTime,
  weeklyTasks,
  lessThanMinuteLabel,
}: {
  dim: Dim;
  dailyCost: ReturnType<typeof aggregateDailyCost>;
  dailyTokens: ReturnType<typeof aggregateDailyTokens>;
  dailyTime: ReturnType<typeof aggregateDailyTime>;
  dailyTasks: ReturnType<typeof aggregateDailyTasks>;
  weeklyCost: ReturnType<typeof aggregateByWeek>["weeklyCostStack"];
  weeklyTokens: ReturnType<typeof aggregateByWeek>["weeklyTokens"];
  weeklyTime: ReturnType<typeof aggregateWeeklyTime>;
  weeklyTasks: ReturnType<typeof aggregateWeeklyTasks>;
  lessThanMinuteLabel: string;
}) {
  const { t } = useT("usage");
  const [metric, setMetric] = useState<DailyMetric>("tokens");

  // Empty-state is per-metric so each toggle option independently decides
  // whether it has data — e.g. tokens recorded but no terminal runs yet
  // should show Tokens normally while Time / Tasks fall through to empty.
  const costData = dim === "weekly" ? weeklyCost : dailyCost;
  const tokensData = dim === "weekly" ? weeklyTokens : dailyTokens;
  const timeData = dim === "weekly" ? weeklyTime : dailyTime;
  const tasksData = dim === "weekly" ? weeklyTasks : dailyTasks;

  const totalCost = costData.reduce((sum, d) => sum + d.total, 0);
  const totalTokens = tokensData.reduce(
    (sum, d) => sum + d.input + d.output + d.cacheRead + d.cacheWrite,
    0,
  );
  const totalSeconds = timeData.reduce((sum, d) => sum + d.totalSeconds, 0);
  const totalTasks = tasksData.reduce(
    (sum, d) => sum + d.completed + d.failed,
    0,
  );
  const isEmpty =
    metric === "cost"
      ? totalCost === 0
      : metric === "tokens"
        ? totalTokens === 0
        : metric === "time"
          ? totalSeconds === 0
          : totalTasks === 0;

  const title =
    dim === "weekly"
      ? metric === "cost"
        ? t(($) => $.weekly.title_cost)
        : metric === "tokens"
          ? t(($) => $.weekly.title_tokens)
          : metric === "time"
            ? t(($) => $.weekly.title_time)
            : t(($) => $.weekly.title_tasks)
      : metric === "cost"
        ? t(($) => $.daily.title_cost)
        : metric === "tokens"
          ? t(($) => $.daily.title_tokens)
          : metric === "time"
            ? t(($) => $.daily.title_time)
            : t(($) => $.daily.title_tasks);

  return (
    <div className="rounded-lg border bg-card p-4">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
        <h4 className="text-sm font-semibold">{title}</h4>
        <Segmented
          value={metric}
          onChange={setMetric}
          options={[
            { label: t(($) => $.daily.metric_tokens), value: "tokens" as const },
            { label: t(($) => $.daily.metric_cost), value: "cost" as const },
            { label: t(($) => $.daily.metric_time), value: "time" as const },
            { label: t(($) => $.daily.metric_tasks), value: "tasks" as const },
          ]}
        />
      </div>
      <div className="min-h-[240px]">
        {isEmpty ? (
          <div className="flex aspect-[3/1] flex-col items-center justify-center gap-2 rounded-md border border-dashed bg-muted/20 p-6 text-center">
            <BarChart3 className="h-5 w-5 text-muted-foreground/50" />
            <p className="text-xs text-muted-foreground">
              {t(($) => $.daily.no_data)}
            </p>
          </div>
        ) : dim === "weekly" ? (
          metric === "cost" ? (
            <WeeklyCostChart data={weeklyCost} />
          ) : metric === "tokens" ? (
            <WeeklyTokensChart data={weeklyTokens} />
          ) : metric === "time" ? (
            <WeeklyTimeChart
              data={weeklyTime}
              formatY={(s) => formatDuration(s, lessThanMinuteLabel)}
              formatTooltip={(s) => formatDuration(s, lessThanMinuteLabel)}
            />
          ) : (
            <WeeklyTasksChart data={weeklyTasks} />
          )
        ) : metric === "cost" ? (
          <DailyCostChart data={dailyCost} />
        ) : metric === "tokens" ? (
          <DailyTokensChart data={dailyTokens} />
        ) : metric === "time" ? (
          <DailyTimeChart
            data={dailyTime}
            formatY={(s) => formatDuration(s, lessThanMinuteLabel)}
            formatTooltip={(s) => formatDuration(s, lessThanMinuteLabel)}
          />
        ) : (
          <DailyTasksChart data={dailyTasks} />
        )}
      </div>
    </div>
  );
}

// Which metric ranks the leaderboard. Drives row order, progress bar
// width, and which column header is emphasised — keeping the three in
// lockstep so the user always sees what the ranking actually measures.
type LeaderboardSort = "tokens" | "cost" | "time" | "tasks";

const SORT_METRIC: Record<LeaderboardSort, (r: AgentDashboardRow) => number> = {
  tokens: (r) => r.tokens,
  cost: (r) => r.cost,
  time: (r) => r.seconds,
  tasks: (r) => r.taskCount,
};

function Leaderboard({
  rows,
  agents,
  lessThanMinuteLabel,
}: {
  rows: AgentDashboardRow[];
  agents: { id: string; name: string }[];
  lessThanMinuteLabel: string;
}) {
  const { t } = useT("usage");
  const [sortBy, setSortBy] = useState<LeaderboardSort>("tokens");

  const sortOptions = useMemo(
    () => [
      { value: "tokens" as const, label: t(($) => $.leaderboard.header_tokens) },
      { value: "cost" as const, label: t(($) => $.leaderboard.header_cost) },
      { value: "time" as const, label: t(($) => $.leaderboard.header_time) },
      { value: "tasks" as const, label: t(($) => $.leaderboard.header_tasks) },
    ],
    [t],
  );

  // Re-rank when the metric changes; keep the merged input untouched so
  // upstream `mergeAgentDashboardRows`'s tiebreaker (run time desc) still
  // applies inside an equal-bucket.
  const sortedRows = useMemo(() => {
    const metric = SORT_METRIC[sortBy];
    return rows.toSorted((a, b) => metric(b) - metric(a));
  }, [rows, sortBy]);

  const maxValue = useMemo(() => {
    const metric = SORT_METRIC[sortBy];
    return sortedRows.reduce((m, r) => Math.max(m, metric(r)), 0);
  }, [sortedRows, sortBy]);

  // Active column gets foreground text; others stay muted. Helps the user
  // see "this is what the bar is measuring" at a glance.
  const colClass = (key: LeaderboardSort) =>
    `text-right ${sortBy === key ? "text-foreground" : "text-muted-foreground"}`;

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 pt-4 pb-3">
        <h4 className="text-sm font-semibold">{t(($) => $.leaderboard.title)}</h4>
        <div className="flex items-center gap-3">
          <Segmented value={sortBy} onChange={setSortBy} options={sortOptions} />
          <span className="text-xs text-muted-foreground">
            {t(($) => $.leaderboard.caption, { count: rows.length })}
          </span>
        </div>
      </div>
      {sortedRows.length === 0 ? (
        <p className="px-4 py-8 text-center text-xs text-muted-foreground">
          {t(($) => $.leaderboard.no_data)}
        </p>
      ) : (
        <>
          <div className="grid grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_5rem_5rem_5rem_4rem] items-center gap-3 border-b px-4 py-2 text-xs font-medium text-muted-foreground">
            <span>{t(($) => $.leaderboard.header_agent)}</span>
            <span />
            <span className={colClass("tokens")}>{t(($) => $.leaderboard.header_tokens)}</span>
            <span className={colClass("cost")}>{t(($) => $.leaderboard.header_cost)}</span>
            <span className={colClass("time")}>{t(($) => $.leaderboard.header_time)}</span>
            <span className={colClass("tasks")}>{t(($) => $.leaderboard.header_tasks)}</span>
          </div>
          <div className="divide-y">
            {sortedRows.map((row) => {
              const agent = agents.find((a) => a.id === row.agentId);
              const value = SORT_METRIC[sortBy](row);
              const pct = maxValue > 0 ? (value / maxValue) * 100 : 0;
              return (
                <div
                  key={row.agentId}
                  className="grid grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_5rem_5rem_5rem_4rem] items-center gap-3 px-4 py-2"
                >
                  <div className="flex min-w-0 items-center gap-2">
                    <ActorAvatar
                      actorType="agent"
                      actorId={row.agentId}
                      size={22}
                      enableHoverCard
                    />
                    <span className="cursor-pointer truncate text-sm font-medium">
                      {agent?.name ?? row.agentId}
                    </span>
                  </div>
                  <div className="relative h-2 overflow-hidden rounded-full bg-muted">
                    <div
                      className="h-full rounded-full bg-chart-1 transition-[width] duration-300 ease-out"
                      style={{ width: `${pct}%` }}
                    />
                  </div>
                  <div
                    className={`text-right text-xs tabular-nums ${sortBy === "tokens" ? "font-medium text-foreground" : "text-muted-foreground"}`}
                  >
                    {formatTokens(row.tokens)}
                  </div>
                  <div
                    className={`text-right tabular-nums ${sortBy === "cost" ? "text-sm font-medium" : "text-xs text-muted-foreground"}`}
                  >
                    ${row.cost.toFixed(2)}
                  </div>
                  <div
                    className={`text-right text-xs tabular-nums ${sortBy === "time" ? "font-medium text-foreground" : "text-muted-foreground"}`}
                  >
                    {formatDuration(row.seconds, lessThanMinuteLabel)}
                  </div>
                  <div
                    className={`text-right text-xs tabular-nums ${sortBy === "tasks" ? "font-medium text-foreground" : "text-muted-foreground"}`}
                  >
                    {row.taskCount}
                  </div>
                </div>
              );
            })}
          </div>
        </>
      )}
    </div>
  );
}

function DashboardSkeleton() {
  return (
    <div className="space-y-5">
      <Skeleton className="h-28 rounded-lg" />
      <Skeleton className="h-56 rounded-lg" />
      <Skeleton className="h-48 rounded-lg" />
    </div>
  );
}

function DashboardEmpty() {
  const { t } = useT("usage");
  return (
    <div className="flex flex-col items-center rounded-lg border border-dashed py-12 text-center">
      <BarChart3 className="h-6 w-6 text-muted-foreground/40" />
      <p className="mt-3 text-sm font-medium">{t(($) => $.empty.title)}</p>
      <p className="mt-1 max-w-md text-xs text-muted-foreground">
        {t(($) => $.empty.body)}
      </p>
    </div>
  );
}
