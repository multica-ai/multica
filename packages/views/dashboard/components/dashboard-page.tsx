"use client";

import { useMemo, useState } from "react";
import { BarChart3, FolderKanban } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
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
  dashboardUsageDailyOptions,
  dashboardUsageByAgentOptions,
  dashboardAgentRunTimeOptions,
  dashboardRunTimeDailyOptions,
} from "@multica/core/dashboard";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";
import { PageHeader } from "../../layout/page-header";
import { KpiCard } from "../../runtimes/components/shared";
import {
  DailyCostChart,
  DailyTokensChart,
  DailyTimeChart,
  DailyTasksChart,
} from "../../runtimes/components/charts";
import { ProjectIcon } from "../../projects/components/project-icon";
import { ActorAvatar } from "../../common/actor-avatar";
import {
  TimezoneSelect,
  browserTimezone,
} from "../../common/timezone-select";
import { formatTokens } from "../../runtimes/utils";
import { useT } from "../../i18n";
import {
  aggregateAgentTokens,
  aggregateDailyCost,
  aggregateDailyTasks,
  aggregateDailyTime,
  aggregateDailyTokens,
  computeDailyTotals,
  formatDuration,
  mergeAgentDashboardRows,
  type AgentDashboardRow,
} from "../utils";

// One-place source of truth for the period selector. Matches the runtime
// detail page so users see the same three options across the dashboards.
const TIME_RANGES = [
  { label: "7d", days: 7 },
  { label: "30d", days: 30 },
  { label: "90d", days: 90 },
] as const;
type TimeRange = (typeof TIME_RANGES)[number]["days"];

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
  const { t: tRuntimes } = useT("runtimes");
  const wsId = useWorkspaceId();
  const [days, setDays] = useState<TimeRange>(30);
  const [projectValue, setProjectValue] = useState<string>(ALL_PROJECTS);
  // Default to the browser's resolved zone so day-boundary buckets match the
  // user's local clock on first render. Pure client-state — the rollup queries
  // are zone-agnostic today; this is the UI affordance the user can pin.
  const [timezone, setTimezone] = useState<string>(() => browserTimezone());

  // The user can save model prices from the runtimes page; re-render when
  // they do so the dashboard reflects the new rates.
  useCustomPricingStore((s) => s.pricings);

  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

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

  const dailyQuery = useQuery(dashboardUsageDailyOptions(wsId, days, projectId));
  const byAgentQuery = useQuery(dashboardUsageByAgentOptions(wsId, days, projectId));
  const runTimeQuery = useQuery(dashboardAgentRunTimeOptions(wsId, days, projectId));
  const runTimeDailyQuery = useQuery(
    dashboardRunTimeDailyOptions(wsId, days, projectId),
  );

  const dailyUsage = dailyQuery.data ?? EMPTY_DAILY;
  const byAgentUsage = byAgentQuery.data ?? EMPTY_BY_AGENT;
  const runTimeRows = runTimeQuery.data ?? EMPTY_RUNTIME;
  const runTimeDailyRows = runTimeDailyQuery.data ?? EMPTY_RUNTIME_DAILY;

  const isLoading =
    dailyQuery.isLoading ||
    byAgentQuery.isLoading ||
    runTimeQuery.isLoading ||
    runTimeDailyQuery.isLoading;

  // Four independent rollups, but the empty-state is one decision — only
  // show "no data yet" when ALL came back empty so a project with tokens
  // but no runs (or vice-versa) doesn't look broken.
  const hasNoData =
    !isLoading &&
    dailyUsage.length === 0 &&
    byAgentUsage.length === 0 &&
    runTimeRows.length === 0 &&
    runTimeDailyRows.length === 0;

  // Cost / token math — re-derived when usage, days, or pricings change.
  const totals = useMemo(() => computeDailyTotals(dailyUsage), [dailyUsage]);
  const dailyCost = useMemo(() => aggregateDailyCost(dailyUsage), [dailyUsage]);
  const dailyTokens = useMemo(() => aggregateDailyTokens(dailyUsage), [dailyUsage]);
  const dailyTime = useMemo(
    () => aggregateDailyTime(runTimeDailyRows),
    [runTimeDailyRows],
  );
  const dailyTasks = useMemo(
    () => aggregateDailyTasks(runTimeDailyRows),
    [runTimeDailyRows],
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

  return (
    <div className="flex h-full flex-col">
      {/* h-auto + min-h-12 + flex-wrap: the toolbar (project filter, range
          switch, timezone select) overflows the single h-12 row on narrow
          and medium widths once the timezone picker is added — letting the
          right cluster wrap underneath keeps every control reachable
          without an off-screen bleed. Wider viewports still render the
          original single row. */}
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
            value={days}
            onChange={setDays}
            options={TIME_RANGES.map((r) => ({ label: r.label, value: r.days }))}
          />
          <TimezoneSelect
            value={timezone}
            onValueChange={setTimezone}
            browserSuffix={tRuntimes(($) => $.detail.timezone_browser_suffix)}
            triggerClassName="rounded-md font-mono text-xs"
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

              {/* Daily trend chart — toggle picks Tokens / Cost / Time /
                  Tasks. All four share the same x-axis (date) so the user
                  can mentally overlay them by switching the toggle. */}
              <DailyTrendBlock
                dailyCost={dailyCost}
                dailyTokens={dailyTokens}
                dailyTime={dailyTime}
                dailyTasks={dailyTasks}
                lessThanMinuteLabel={t(($) => $.duration.less_than_minute)}
              />

              {/* Per-agent leaderboard — user picks the ranking metric;
                  the progress bar and column emphasis follow the metric. */}
              <Leaderboard
                rows={agentRows}
                agents={agents}
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

function DailyTrendBlock({
  dailyCost,
  dailyTokens,
  dailyTime,
  dailyTasks,
  lessThanMinuteLabel,
}: {
  dailyCost: ReturnType<typeof aggregateDailyCost>;
  dailyTokens: ReturnType<typeof aggregateDailyTokens>;
  dailyTime: ReturnType<typeof aggregateDailyTime>;
  dailyTasks: ReturnType<typeof aggregateDailyTasks>;
  lessThanMinuteLabel: string;
}) {
  const { t } = useT("usage");
  const [metric, setMetric] = useState<DailyMetric>("tokens");

  // Empty-state is per-metric so each toggle option independently decides
  // whether it has data — e.g. tokens recorded but no terminal runs yet
  // should show Tokens normally while Time / Tasks fall through to empty.
  const totalCost = dailyCost.reduce((sum, d) => sum + d.total, 0);
  const totalTokens = dailyTokens.reduce(
    (sum, d) => sum + d.input + d.output + d.cacheRead + d.cacheWrite,
    0,
  );
  const totalSeconds = dailyTime.reduce((sum, d) => sum + d.totalSeconds, 0);
  const totalTasks = dailyTasks.reduce(
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
    metric === "cost"
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
    return [...rows].sort((a, b) => metric(b) - metric(a));
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
