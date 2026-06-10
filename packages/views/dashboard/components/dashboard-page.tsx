"use client";

import { useMemo, useState, useEffect } from "react";
import { useNavigation } from "../../navigation";
import { BarChart3, ChevronDown, FolderKanban } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import {
  dashboardUsageDailyOptions,
  dashboardUsageByAgentOptions,
  dashboardLocalUsageDailyOptions,
  dashboardLocalUsageByRunnerOptions,
  dashboardLocalRunTimeByRunnerOptions,
  dashboardLocalRunTimeDailyOptions,
  dashboardAgentRunTimeOptions,
  dashboardRunTimeDailyOptions,
} from "@multica/core/dashboard";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";
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
import {
  addDaysIso,
  aggregateByWeek,
  formatTokens,
  todayIso,
} from "../../runtimes/utils";
import { useT } from "../../i18n";
import {
  aggregateAgentTokens,
  aggregateLocalRunnerTokens,
  aggregateDailyCost,
  aggregateDailyTasks,
  aggregateDailyTime,
  aggregateDailyTokens,
  aggregateWeeklyTasks,
  aggregateWeeklyTime,
  computeDailyTotals,
  formatDuration,
  mergeDailyRunTimeRows,
  mergeAgentDashboardRows,
  aggregateByMember,
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
  daily: 7,
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
const EMPTY_LOCAL_BY_RUNNER: import("@multica/core/types").DashboardLocalUsageByRunner[] = [];
const EMPTY_LOCAL_RUNTIME_BY_RUNNER: import("@multica/core/types").DashboardLocalRunTimeByRunner[] = [];
const EMPTY_RUNTIME: import("@multica/core/types").DashboardAgentRunTime[] = [];
const EMPTY_RUNTIME_DAILY: import("@multica/core/types").DashboardRunTimeDaily[] = [];

function fmtMoney(n: number): string {
  if (n >= 100) return `$${n.toFixed(0)}`;
  return `$${n.toFixed(2)}`;
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
    <div className="inline-flex max-w-full flex-wrap items-center gap-0.5 rounded-md bg-muted p-0.5">
      {options.map((o) => (
        <button
          key={String(o.value)}
          type="button"
          onClick={() => onChange(o.value)}
          className={`rounded-sm px-2.5 py-1 text-xs font-medium whitespace-nowrap transition-colors ${
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
  const { searchParams, replace, pathname } = useNavigation();
  const [dim, setDimState] = useState<Dim>("daily");
  const [days, setDaysState] = useState<TimeRange>(7);
  const [projectValue, setProjectValue] = useState<string>(ALL_PROJECTS);

  // Sync state from URL
  useEffect(() => {
    const urlRange = searchParams.get("range");
    let nextDays: TimeRange = 7;
    if (urlRange) {
      const match = urlRange.match(/^(\d+)d$/);
      if (match) {
        const d = parseInt(match[1]!, 10);
        const allowed = TIME_RANGES.map((r) => r.days);
        if (allowed.includes(d as TimeRange)) {
          nextDays = d as TimeRange;
        }
      }
    }

    const urlGran = searchParams.get("granularity");
    let nextDim: Dim = "daily";
    if (urlGran === "day") {
      nextDim = "daily";
    } else if (urlGran === "week") {
      nextDim = "weekly";
    }

    if (nextDays !== days) {
      setDaysState(nextDays);
    }
    if (nextDim !== dim) {
      setDimState(nextDim);
    }
  }, [searchParams, days, dim]);

  // Fill in default URL parameters if missing
  useEffect(() => {
    const params = new URLSearchParams(searchParams);
    let changed = false;
    if (!params.has("range")) {
      params.set("range", "7d");
      changed = true;
    }
    if (!params.has("granularity")) {
      params.set("granularity", "day");
      changed = true;
    }
    if (changed) {
      replace(`${pathname}?${params.toString()}`);
    }
  }, [searchParams, pathname, replace]);

  const allowedRanges = rangesForDim(dim);
  const handleDimChange = (next: Dim) => {
    const params = new URLSearchParams(searchParams);
    params.set("granularity", next === "daily" ? "day" : "week");
    const stillAllowed = (rangesForDim(next) as readonly { days: number }[]).some(
      (r) => r.days === days,
    );
    if (!stillAllowed) {
      params.set("range", `${DEFAULT_DAYS_BY_DIM[next]}d`);
    }
    replace(`${pathname}?${params.toString()}`);
  };

  const handleDaysChange = (nextDays: TimeRange) => {
    const params = new URLSearchParams(searchParams);
    params.set("range", `${nextDays}d`);
    replace(`${pathname}?${params.toString()}`);
  };

  // The user can save model prices from the runtimes page; re-render when
  // they do so the dashboard reflects the new rates.
  useCustomPricingStore((s) => s.pricings);

  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));

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
  const localDailyQuery = useQuery(
    dashboardLocalUsageDailyOptions(wsId, chartFetchDays, projectId),
  );
  const localByRunnerQuery = useQuery(
    dashboardLocalUsageByRunnerOptions(wsId, days, projectId),
  );
  const localRunTimeByRunnerQuery = useQuery(
    dashboardLocalRunTimeByRunnerOptions(wsId, days, projectId),
  );
  const localRunTimeDailyQuery = useQuery(
    dashboardLocalRunTimeDailyOptions(wsId, chartFetchDays, projectId),
  );
  const runTimeDailyQuery = useQuery(
    dashboardRunTimeDailyOptions(wsId, chartFetchDays, projectId, viewTZ),
  );

  const dailyUsage = dailyQuery.data ?? EMPTY_DAILY;
  const byAgentUsage = byAgentQuery.data ?? EMPTY_BY_AGENT;
  const localDailyUsage = localDailyQuery.data ?? EMPTY_DAILY;
  const localByRunnerUsage = localByRunnerQuery.data ?? EMPTY_LOCAL_BY_RUNNER;
  const localRunTimeByRunnerRows =
    localRunTimeByRunnerQuery.data ?? EMPTY_LOCAL_RUNTIME_BY_RUNNER;
  const localRunTimeDailyRows =
    localRunTimeDailyQuery.data ?? EMPTY_RUNTIME_DAILY;
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
  const isLoading =
    dailyQuery.isLoading ||
    byAgentQuery.isLoading ||
    localDailyQuery.isLoading ||
    localByRunnerQuery.isLoading ||
    localRunTimeByRunnerQuery.isLoading ||
    localRunTimeDailyQuery.isLoading ||
    runTimeQuery.isLoading ||
    runTimeDailyQuery.isLoading;

  // Four independent rollups, but the empty-state is one decision — only
  // show "no data yet" when ALL came back empty so a project with tokens
  // but no runs (or vice-versa) doesn't look broken.
  const hasNoData =
    !isLoading &&
    dailyUsage.length === 0 &&
    byAgentUsage.length === 0 &&
    localDailyUsage.length === 0 &&
    localByRunnerUsage.length === 0 &&
    localRunTimeByRunnerRows.length === 0 &&
    localRunTimeDailyRows.length === 0 &&
    runTimeRows.length === 0 &&
    runTimeDailyRows.length === 0;

  // Cost / token math — re-derived when usage, days, or pricings change.
  const combinedDailyUsage = useMemo(
    () => [...dailyUsage, ...localDailyUsage],
    [dailyUsage, localDailyUsage],
  );
  const dailyUsageInWindow = useMemo(
    () => combinedDailyUsage.filter((u) => u.date >= dailyCutoffIso),
    [combinedDailyUsage, dailyCutoffIso],
  );
  const combinedRunTimeDailyRows = useMemo(
    () => mergeDailyRunTimeRows(runTimeDailyRows, localRunTimeDailyRows),
    [runTimeDailyRows, localRunTimeDailyRows],
  );
  const runTimeDailyInWindow = useMemo(
    () => combinedRunTimeDailyRows.filter((r) => r.date >= dailyCutoffIso),
    [combinedRunTimeDailyRows, dailyCutoffIso],
  );
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
    () => aggregateByWeek(combinedDailyUsage, viewTZ, weekCount),
    [combinedDailyUsage, viewTZ, weekCount],
  );
  const weeklyCost = weekly.weeklyCostStack;
  const weeklyTokens = weekly.weeklyTokens;
  const weeklyTime = useMemo(
    () => aggregateWeeklyTime(combinedRunTimeDailyRows, viewTZ, weekCount),
    [combinedRunTimeDailyRows, viewTZ, weekCount],
  );
  const weeklyTasks = useMemo(
    () => aggregateWeeklyTasks(combinedRunTimeDailyRows, viewTZ, weekCount),
    [combinedRunTimeDailyRows, viewTZ, weekCount],
  );
  const agentTokenRows = useMemo(
    () => aggregateAgentTokens(byAgentUsage),
    [byAgentUsage],
  );
  const localTokenRows = useMemo(
    () => aggregateLocalRunnerTokens(localByRunnerUsage),
    [localByRunnerUsage],
  );

  // Run-time totals — taskCount + failedCount summed for the KPI row.
  const runTimeTotals = useMemo(() => {
    let totalSeconds = 0;
    let taskCount = 0;
    let failedCount = 0;
    for (const r of [...runTimeRows, ...localRunTimeByRunnerRows]) {
      totalSeconds += r.total_seconds;
      taskCount += r.task_count;
      failedCount += r.failed_count;
    }
    return { totalSeconds, taskCount, failedCount };
  }, [runTimeRows, localRunTimeByRunnerRows]);

  const agentRows = useMemo(
    () =>
      mergeAgentDashboardRows(
        agentTokenRows,
        runTimeRows,
        localTokenRows,
        localRunTimeByRunnerRows,
        agents,
      ),
    [agentTokenRows, runTimeRows, localTokenRows, localRunTimeByRunnerRows, agents],
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
            onChange={handleDaysChange}
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
              {/* KPI row — same 3-divide-x card grid the runtime usage
                  section uses, expanded to four tiles. */}
              <div className="grid grid-cols-1 divide-y rounded-lg border bg-card sm:grid-cols-2 sm:divide-x sm:divide-y-0 lg:grid-cols-4">
                <KpiCard
                  label={t(($) => $.kpi.cost_label, { days })}
                  value={fmtMoney(totals.cost)}
                />
                <KpiCard
                  label={t(($) => $.kpi.tokens_label, { days })}
                  value={formatTokens(
                    totals.input + totals.output + totals.cacheRead + totals.cacheWrite,
                  )}
                  hint={t(($) => $.kpi.tokens_hint, {
                    input: formatTokens(totals.input),
                    output: formatTokens(totals.output),
                  })}
                />
                <KpiCard
                  label={t(($) => $.kpi.run_time_label, { days })}
                  value={formatDuration(
                    runTimeTotals.totalSeconds,
                    t(($) => $.duration.less_than_minute),
                  )}
                  hint={t(($) => $.kpi.run_time_hint, {
                    tasks: runTimeTotals.taskCount,
                  })}
                />
                <KpiCard
                  label={t(($) => $.kpi.tasks_label, { days })}
                  value={String(runTimeTotals.taskCount)}
                  hint={t(($) => $.kpi.tasks_hint, {
                    failed: runTimeTotals.failedCount,
                  })}
                  accent={runTimeTotals.failedCount > 0 ? "default" : "default"}
                />
              </div>

              {/* Trend chart — toggle picks Tokens / Cost / Time / Tasks
                  and the parent's dim selector decides whether the bars are
                  per-day or per-calendar-week. All four metrics share the
                  same x-axis so the user can mentally overlay them by
                  flipping the toggle. */}
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

              {/* Per-agent leaderboard — user picks the ranking metric;
                  the progress bar and column emphasis follow the metric. */}
              <Leaderboard
                rows={agentRows}
                agents={agents}
                members={members}
                lessThanMinuteLabel={t(($) => $.duration.less_than_minute)}
              />
            </>
          )}
        </div>
      </div>
    </div>
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
type LeaderboardSort = "processedTokens" | "nonCachedTokens" | "cachedTokens" | "cost" | "time" | "tasks";
type LeaderboardTab = "agent" | "member";
type LeaderboardMetricRow = {
  tokens: number;
  nonCachedTokens: number;
  cachedTokens: number;
  cost: number;
  seconds: number;
  taskCount: number;
};
type TokenBreakdownRow = Pick<
  AgentDashboardRow,
  "inputTokens" | "outputTokens" | "cacheReadTokens" | "cacheWriteTokens"
>;

const SORT_METRIC: Record<LeaderboardSort, (r: LeaderboardMetricRow) => number> = {
  processedTokens: (r) => r.tokens,
  nonCachedTokens: (r) => r.nonCachedTokens,
  cachedTokens: (r) => r.cachedTokens,
  cost: (r) => r.cost,
  time: (r) => r.seconds,
  tasks: (r) => r.taskCount,
};

function tokenValueForSort(row: LeaderboardMetricRow, sortBy: LeaderboardSort): number {
  switch (sortBy) {
    case "nonCachedTokens":
      return row.nonCachedTokens;
    case "cachedTokens":
      return row.cachedTokens;
    default:
      return row.tokens;
  }
}

function TokenBreakdown({
  row,
  value,
}: {
  row: TokenBreakdownRow;
  value: number;
}) {
  const { t } = useT("usage");
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span className="inline-block cursor-default underline decoration-dotted underline-offset-2">
            {formatTokens(value)}
          </span>
        }
      />
      <TooltipContent side="top" className="w-44 text-xs">
        <div className="space-y-1">
          <div className="flex justify-between gap-3">
            <span>{t(($) => $.leaderboard.breakdown_input)}</span>
            <span className="tabular-nums">{formatTokens(row.inputTokens)}</span>
          </div>
          <div className="flex justify-between gap-3">
            <span>{t(($) => $.leaderboard.breakdown_output)}</span>
            <span className="tabular-nums">{formatTokens(row.outputTokens)}</span>
          </div>
          <div className="flex justify-between gap-3">
            <span>{t(($) => $.leaderboard.breakdown_cache_read)}</span>
            <span className="tabular-nums">{formatTokens(row.cacheReadTokens)}</span>
          </div>
          <div className="flex justify-between gap-3">
            <span>{t(($) => $.leaderboard.breakdown_cache_write)}</span>
            <span className="tabular-nums">{formatTokens(row.cacheWriteTokens)}</span>
          </div>
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

// Sentinel matching utils.ts SYSTEM_OWNER — agents without an ownerId are
// grouped under this key.
const SYSTEM_OWNER = "__system__";

function Leaderboard({
  rows,
  agents,
  members,
  lessThanMinuteLabel,
}: {
  rows: AgentDashboardRow[];
  agents: { id: string; name: string }[];
  members: { user_id: string; name: string }[];
  lessThanMinuteLabel: string;
}) {
  const { t } = useT("usage");
  const [tab, setTab] = useState<LeaderboardTab>("agent");
  const [sortBy, setSortBy] = useState<LeaderboardSort>("processedTokens");
  const [expandedMember, setExpandedMember] = useState<string | null>(null);

  const memberRows = useMemo(() => aggregateByMember(rows), [rows]);

  // Resolve member display name from user_id, falling back to "System" for
  // unowned agents.
  const memberName = useMemo(() => {
    const map = new Map(members.map((m) => [m.user_id, m.name] as const));
    return (ownerId: string) =>
      ownerId === SYSTEM_OWNER
        ? t(($) => $.leaderboard.system_group)
        : map.get(ownerId) ?? ownerId;
  }, [members, t]);

  const sortOptions = useMemo(
    () => [
      { value: "processedTokens" as const, label: t(($) => $.leaderboard.header_processed_tokens) },
      { value: "nonCachedTokens" as const, label: t(($) => $.leaderboard.header_non_cached_tokens) },
      { value: "cachedTokens" as const, label: t(($) => $.leaderboard.header_cached_tokens) },
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

  const sortedMemberRows = useMemo(() => {
    const metric = SORT_METRIC[sortBy];
    return memberRows.toSorted((a, b) => metric(b) - metric(a));
  }, [memberRows, sortBy]);

  const maxValue = useMemo(() => {
    const metric = SORT_METRIC[sortBy];
    const source = tab === "agent" ? sortedRows : sortedMemberRows;
    return source.reduce((m, r) => Math.max(m, metric(r)), 0);
  }, [sortedRows, sortedMemberRows, sortBy, tab]);

  // Active column gets foreground text; others stay muted. Helps the user
  // see "this is what the bar is measuring" at a glance.
  const colClass = (key: LeaderboardSort) =>
    `text-right ${sortBy === key ? "text-foreground" : "text-muted-foreground"}`;

  const tokenHeader =
    sortBy === "nonCachedTokens"
      ? t(($) => $.leaderboard.header_non_cached_tokens)
      : sortBy === "cachedTokens"
        ? t(($) => $.leaderboard.header_cached_tokens)
        : t(($) => $.leaderboard.header_processed_tokens);
  const tokenColumnActive =
    sortBy === "processedTokens" || sortBy === "nonCachedTokens" || sortBy === "cachedTokens";

  const caption =
    tab === "agent"
      ? t(($) => $.leaderboard.caption, { count: rows.length })
      : t(($) => $.leaderboard.caption_member, { count: memberRows.length });

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 pt-4 pb-3">
        <div className="flex items-center gap-3">
          <h4 className="text-sm font-semibold">{t(($) => $.leaderboard.title)}</h4>
          <Segmented
            value={tab}
            onChange={setTab}
            options={[
              { label: t(($) => $.leaderboard.tab_agent), value: "agent" as const },
              { label: t(($) => $.leaderboard.tab_member), value: "member" as const },
            ]}
          />
        </div>
        <div className="flex items-center gap-3">
          <Segmented value={sortBy} onChange={setSortBy} options={sortOptions} />
          <span className="text-xs text-muted-foreground">{caption}</span>
        </div>
      </div>
      {tab === "agent" ? (
        sortedRows.length === 0 ? (
          <p className="px-4 py-8 text-center text-xs text-muted-foreground">
            {t(($) => $.leaderboard.no_data)}
          </p>
        ) : (
          <>
            <div className="grid grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_6rem_5rem_5rem_4rem] items-center gap-3 border-b px-4 py-2 text-xs font-medium text-muted-foreground">
              <span>{t(($) => $.leaderboard.header_agent)}</span>
              <span />
              <span className={tokenColumnActive ? "text-right text-foreground" : "text-right text-muted-foreground"}>{tokenHeader}</span>
              <span className={colClass("cost")}>{t(($) => $.leaderboard.header_cost)}</span>
              <span className={colClass("time")}>{t(($) => $.leaderboard.header_time)}</span>
              <span className={colClass("tasks")}>{t(($) => $.leaderboard.header_tasks)}</span>
            </div>
            <div className="divide-y">
              {sortedRows.map((row) => {
                const agent = agents.find((a) => a.id === row.agentId);
                const value = SORT_METRIC[sortBy](row);
                const tokenValue = tokenValueForSort(row, sortBy);
                const pct = maxValue > 0 ? (value / maxValue) * 100 : 0;
                const displayName = row.displayName ?? agent?.name ?? row.agentId;
                return (
                  <div
                    key={row.agentId}
                    className="grid grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_6rem_5rem_5rem_4rem] items-center gap-3 px-4 py-2"
                  >
                    <div className="flex min-w-0 items-center gap-2">
                      <ActorAvatar
                        actorType={row.source === "local" ? "member" : "agent"}
                        actorId={row.source === "local" ? (row.ownerId ?? row.agentId) : row.agentId}
                        size={22}
                        enableHoverCard={row.source === "agent"}
                      />
                      <span className="cursor-pointer truncate text-sm font-medium">
                        {displayName}
                      </span>
                    </div>
                    <div className="relative h-2 overflow-hidden rounded-full bg-muted">
                      <div
                        className="h-full rounded-full bg-chart-1 transition-[width] duration-300 ease-out"
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                    <div
                      className={`text-right text-xs tabular-nums ${tokenColumnActive ? "font-medium text-foreground" : "text-muted-foreground"}`}
                    >
                      <TokenBreakdown row={row} value={tokenValue} />
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
        )
      ) : sortedMemberRows.length === 0 ? (
        <p className="px-4 py-8 text-center text-xs text-muted-foreground">
          {t(($) => $.leaderboard.no_data)}
        </p>
      ) : (
        <>
          <div className="grid grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_6rem_5rem_5rem_4rem] items-center gap-3 border-b px-4 py-2 text-xs font-medium text-muted-foreground">
            <span>{t(($) => $.leaderboard.header_member)}</span>
            <span />
            <span className={tokenColumnActive ? "text-right text-foreground" : "text-right text-muted-foreground"}>{tokenHeader}</span>
            <span className={colClass("cost")}>{t(($) => $.leaderboard.header_cost)}</span>
            <span className={colClass("time")}>{t(($) => $.leaderboard.header_time)}</span>
            <span className={colClass("tasks")}>{t(($) => $.leaderboard.header_tasks)}</span>
          </div>
          <div className="divide-y">
            {sortedMemberRows.map((mRow) => {
              const value = SORT_METRIC[sortBy](mRow);
              const tokenValue = tokenValueForSort(mRow, sortBy);
              const pct = maxValue > 0 ? (value / maxValue) * 100 : 0;
              const isExpanded = expandedMember === mRow.ownerId;
              const actorType = mRow.ownerId === SYSTEM_OWNER ? "system" : "member";
              return (
                <div key={mRow.ownerId}>
                  <div
                    role="button"
                    tabIndex={0}
                    onClick={() => setExpandedMember(isExpanded ? null : mRow.ownerId)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        setExpandedMember(isExpanded ? null : mRow.ownerId);
                      }
                    }}
                    className="grid grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_6rem_5rem_5rem_4rem] items-center gap-3 px-4 py-2 cursor-pointer hover:bg-muted/40 transition-colors"
                  >
                    <div className="flex min-w-0 items-center gap-2">
                      <ActorAvatar
                        actorType={actorType}
                        actorId={mRow.ownerId === SYSTEM_OWNER ? "" : mRow.ownerId}
                        size={22}
                      />
                      <span className="truncate text-sm font-medium">
                        {memberName(mRow.ownerId)}
                      </span>
                      <ChevronDown
                        className={`h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform duration-200 ${isExpanded ? "rotate-180" : ""}`}
                      />
                    </div>
                    <div className="relative h-2 overflow-hidden rounded-full bg-muted">
                      <div
                        className="h-full rounded-full bg-chart-1 transition-[width] duration-300 ease-out"
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                    <div
                      className={`text-right text-xs tabular-nums ${tokenColumnActive ? "font-medium text-foreground" : "text-muted-foreground"}`}
                    >
                      <TokenBreakdown row={mRow} value={tokenValue} />
                    </div>
                    <div
                      className={`text-right tabular-nums ${sortBy === "cost" ? "text-sm font-medium" : "text-xs text-muted-foreground"}`}
                    >
                      ${mRow.cost.toFixed(2)}
                    </div>
                    <div
                      className={`text-right text-xs tabular-nums ${sortBy === "time" ? "font-medium text-foreground" : "text-muted-foreground"}`}
                    >
                      {formatDuration(mRow.seconds, lessThanMinuteLabel)}
                    </div>
                    <div
                      className={`text-right text-xs tabular-nums ${sortBy === "tasks" ? "font-medium text-foreground" : "text-muted-foreground"}`}
                    >
                      {mRow.taskCount}
                    </div>
                  </div>
                  {/* Collapsible agent detail panel */}
                  <div
                    className={`grid transition-[grid-template-rows] duration-200 ease-out ${
                      isExpanded ? "grid-rows-[1fr]" : "grid-rows-[0fr]"
                    }`}
                  >
                    <div className="overflow-hidden">
                      <div className="border-t bg-muted/20 px-4 py-2 space-y-1">
                        {mRow.agents.toSorted(
                          (a, b) => SORT_METRIC[sortBy](b) - SORT_METRIC[sortBy](a),
                        ).map((row) => {
                          const agent = agents.find((a) => a.id === row.agentId);
                          const displayName = row.displayName ?? agent?.name ?? row.agentId;
                          const agentValue = SORT_METRIC[sortBy](row);
                          const memberMaxValue = mRow.agents.reduce(
                            (max, r) => Math.max(max, SORT_METRIC[sortBy](r)),
                            0,
                          );
                          const agentPct = memberMaxValue > 0 ? (agentValue / memberMaxValue) * 100 : 0;
                          return (
                            <div
                              key={row.agentId}
                              className="grid grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_6rem_5rem_5rem_4rem] items-center gap-3 py-1.5"
                            >
                              <div className="flex min-w-0 items-center gap-2 pl-2">
                                <ActorAvatar
                                  actorType={row.source === "local" ? "member" : "agent"}
                                  actorId={row.source === "local" ? (row.ownerId ?? row.agentId) : row.agentId}
                                  size={18}
                                  enableHoverCard={row.source === "agent"}
                                />
                                <span className="truncate text-xs text-muted-foreground">
                                  {displayName}
                                </span>
                              </div>
                              <div className="relative h-1.5 overflow-hidden rounded-full bg-muted">
                                <div
                                  className="h-full rounded-full bg-chart-2 transition-[width] duration-300 ease-out"
                                  style={{ width: `${agentPct}%` }}
                                />
                              </div>
                              <div className="text-right text-xs tabular-nums text-muted-foreground">
                                <TokenBreakdown row={row} value={tokenValueForSort(row, sortBy)} />
                              </div>
                              <div className="text-right text-xs tabular-nums text-muted-foreground">
                                ${row.cost.toFixed(2)}
                              </div>
                              <div className="text-right text-xs tabular-nums text-muted-foreground">
                                {formatDuration(row.seconds, lessThanMinuteLabel)}
                              </div>
                              <div className="text-right text-xs tabular-nums text-muted-foreground">
                                {row.taskCount}
                              </div>
                            </div>
                          );
                        })}
                      </div>
                    </div>
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
