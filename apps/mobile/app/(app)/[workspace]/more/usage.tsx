/**
 * Workspace Usage page. Mirrors packages/views/dashboard/components/
 * dashboard-page.tsx's DashboardPage (the "Usage" nav page — not the
 * per-runtime UsageSection, which is out of scope). See
 * docs/superpowers/specs/2026-07-13-mobile-usage-browse-design.md.
 */
import { useMemo, useState } from "react";
import { ActivityIndicator, ScrollView, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { useActionSheet } from "@expo/react-native-action-sheet";
import { Ionicons } from "@expo/vector-icons";
import SegmentedControl from "@react-native-segmented-control/segmented-control";
import { BarChart } from "react-native-gifted-charts";
import type {
  Agent,
  DashboardAgentRunTime,
  DashboardRunTimeDaily,
  DashboardUsageByAgent,
  DashboardUsageDaily,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { UsageStatCard } from "@/components/usage/usage-stat-card";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useViewingTimezone } from "@/lib/use-viewing-timezone";
import { projectListOptions } from "@/data/queries/projects";
import { agentListOptions } from "@/data/queries/agents";
import {
  dashboardAgentRunTimeOptions,
  dashboardRunTimeDailyOptions,
  dashboardUsageByAgentOptions,
  dashboardUsageDailyOptions,
} from "@/data/queries/usage";
import {
  addDaysIso,
  type AgentDashboardRow,
  aggregateAgentTokens,
  aggregateByWeek,
  aggregateDailyCost,
  aggregateDailyTasks,
  aggregateDailyTime,
  aggregateDailyTokens,
  aggregateWeeklyTasks,
  aggregateWeeklyTime,
  bucketUnknownAgentRows,
  computeDailyTotals,
  DELETED_AGENTS_ROW_ID,
  formatDuration,
  mergeAgentDashboardRows,
  todayIso,
} from "@/lib/usage-display";
import { fmtMoney, formatTokens } from "@/lib/usage-pricing";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

type Dim = "daily" | "weekly";

type LeaderboardSort = "tokens" | "cost" | "time" | "tasks";

// Which metric ranks the leaderboard — drives row order and progress-bar
// width. Hoisted to module scope (not re-created every render) since each
// accessor only reads its own row argument, mirroring
// packages/views/dashboard/components/dashboard-page.tsx's SORT_METRIC.
const SORT_METRIC: Record<LeaderboardSort, (r: AgentDashboardRow) => number> = {
  tokens: (r) => r.tokens,
  cost: (r) => r.cost,
  time: (r) => r.seconds,
  tasks: (r) => r.taskCount,
};

// Stable references — `data ?? []` would create a new empty array on every
// render while a query is loading, which breaks useMemo's reference-equality
// dep check and trips the exhaustive-deps lint rule. Mirrors
// packages/views/dashboard/components/dashboard-page.tsx's EMPTY_* constants.
const EMPTY_DAILY: DashboardUsageDaily[] = [];
const EMPTY_BY_AGENT: DashboardUsageByAgent[] = [];
const EMPTY_RUNTIME: DashboardAgentRunTime[] = [];
const EMPTY_RUNTIME_DAILY: DashboardRunTimeDaily[] = [];
const EMPTY_AGENTS: Agent[] = [];

// Legal periods per dimension + default-on-switch, confirmed against
// packages/views/dashboard/components/dashboard-page.tsx's
// TIME_RANGES / DEFAULT_DAYS_BY_DIM.
const TIME_RANGES = [
  { label: "1d", days: 1, dims: ["daily"] as const },
  { label: "7d", days: 7, dims: ["daily"] as const },
  { label: "30d", days: 30, dims: ["daily", "weekly"] as const },
  { label: "90d", days: 90, dims: ["daily", "weekly"] as const },
  { label: "180d", days: 180, dims: ["weekly"] as const },
] as const;
const DEFAULT_DAYS_BY_DIM: Record<Dim, number> = { daily: 30, weekly: 90 };
const ALL_PROJECTS = "__all__";

function rangesForDim(dim: Dim) {
  return TIME_RANGES.filter((r) => (r.dims as readonly Dim[]).includes(dim));
}

export default function UsagePage() {
  const { t } = useTranslation("usage");
  const { t: tCommon } = useTranslation("common");
  const { showActionSheetWithOptions } = useActionSheet();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const viewTZ = useViewingTimezone();

  const [dim, setDim] = useState<Dim>("daily");
  const [days, setDays] = useState<number>(30);
  const [projectValue, setProjectValue] = useState<string>(ALL_PROJECTS);

  const allowedRanges = rangesForDim(dim);
  const handleDimChange = (next: Dim) => {
    setDim(next);
    const stillAllowed = rangesForDim(next).some((r) => r.days === days);
    if (!stillAllowed) setDays(DEFAULT_DAYS_BY_DIM[next]);
  };

  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const agentsQuery = useQuery(agentListOptions(wsId));
  const agents = agentsQuery.data ?? EMPTY_AGENTS;

  const projectId = useMemo(() => {
    if (projectValue === ALL_PROJECTS) return null;
    return projects.some((p) => p.id === projectValue) ? projectValue : null;
  }, [projectValue, projects]);

  const selectedProjectTitle =
    projectValue === ALL_PROJECTS
      ? t("filter.all_projects")
      : (projects.find((p) => p.id === projectValue)?.title ?? t("filter.all_projects"));

  const openProjectPicker = () => {
    const options = [t("filter.all_projects"), ...projects.map((p) => p.title), tCommon("cancel")];
    showActionSheetWithOptions(
      { options, cancelButtonIndex: options.length - 1 },
      (index) => {
        if (index == null || index === options.length - 1) return;
        setProjectValue(index === 0 ? ALL_PROJECTS : projects[index - 1].id);
      },
    );
  };

  // The weekly chart paints ceil(days / 7) trailing calendar weeks. In the
  // worst case (today = Sunday) the leftmost Monday sits weekCount*7-1
  // days back, so over-fetch the per-date queries to cover the full first
  // week — mirrors DashboardPage's chartFetchDays exactly.
  const weekCount = Math.max(1, Math.ceil(days / 7));
  const chartFetchDays = dim === "weekly" ? weekCount * 7 : days;

  const dailyQuery = useQuery(dashboardUsageDailyOptions(wsId, chartFetchDays, projectId, viewTZ));
  const byAgentQuery = useQuery(dashboardUsageByAgentOptions(wsId, days, projectId, viewTZ));
  const runTimeQuery = useQuery(dashboardAgentRunTimeOptions(wsId, days, projectId, viewTZ));
  const runTimeDailyQuery = useQuery(dashboardRunTimeDailyOptions(wsId, chartFetchDays, projectId, viewTZ));

  const isLoading =
    dailyQuery.isLoading || byAgentQuery.isLoading || runTimeQuery.isLoading || runTimeDailyQuery.isLoading;
  const error = dailyQuery.error ?? byAgentQuery.error ?? runTimeQuery.error ?? runTimeDailyQuery.error;
  const refetchAll = () => {
    dailyQuery.refetch();
    byAgentQuery.refetch();
    runTimeQuery.refetch();
    runTimeDailyQuery.refetch();
  };

  const dailyUsage = dailyQuery.data ?? EMPTY_DAILY;
  const byAgentUsage = byAgentQuery.data ?? EMPTY_BY_AGENT;
  const runTimeRows = runTimeQuery.data ?? EMPTY_RUNTIME;
  const runTimeDailyRows = runTimeDailyQuery.data ?? EMPTY_RUNTIME_DAILY;

  // Client-side day-window re-slice, mirroring DashboardPage's
  // dailyCutoffIso — dailyQuery/runTimeDailyQuery are over-fetched to
  // chartFetchDays for the weekly chart, so the KPI totals must re-slice
  // back down to the advertised `days` window.
  const dailyCutoffIso = useMemo(
    () => addDaysIso(todayIso(viewTZ), -(days - 1)),
    [days, viewTZ],
  );
  const dailyUsageInWindow = useMemo(
    () => dailyUsage.filter((u) => u.date >= dailyCutoffIso),
    [dailyUsage, dailyCutoffIso],
  );

  const totals = useMemo(() => computeDailyTotals(dailyUsageInWindow), [dailyUsageInWindow]);
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

  type Metric = "cost" | "tokens" | "time" | "tasks";
  const [metric, setMetric] = useState<Metric>("tokens");

  const dailyCost = useMemo(() => aggregateDailyCost(dailyUsageInWindow), [dailyUsageInWindow]);
  const dailyTokens = useMemo(() => aggregateDailyTokens(dailyUsageInWindow), [dailyUsageInWindow]);
  const runTimeDailyInWindow = useMemo(
    () => runTimeDailyRows.filter((r) => r.date >= dailyCutoffIso),
    [runTimeDailyRows, dailyCutoffIso],
  );
  const dailyTime = useMemo(() => aggregateDailyTime(runTimeDailyInWindow), [runTimeDailyInWindow]);
  const dailyTasks = useMemo(() => aggregateDailyTasks(runTimeDailyInWindow), [runTimeDailyInWindow]);

  // Weekly aggregates use the raw over-fetched series (chartFetchDays),
  // NOT the windowed one — mirrors DashboardPage exactly, so the leftmost
  // week isn't truncated.
  const weekly = useMemo(() => aggregateByWeek(dailyUsage, viewTZ, weekCount), [dailyUsage, viewTZ, weekCount]);
  const weeklyTime = useMemo(
    () => aggregateWeeklyTime(runTimeDailyRows, viewTZ, weekCount),
    [runTimeDailyRows, viewTZ, weekCount],
  );
  const weeklyTasks = useMemo(
    () => aggregateWeeklyTasks(runTimeDailyRows, viewTZ, weekCount),
    [runTimeDailyRows, viewTZ, weekCount],
  );

  const { colorScheme } = useColorScheme();
  const theme = THEME[colorScheme];
  const lessThanMinuteLabel = t("duration.less_than_minute");

  // Per-metric chart data. Cost/Tokens are stacked series (input/output/
  // cache-write for Cost — cache-read excluded; input/output/cache-read/
  // cache-write for Tokens); Time is a single unstacked series; Tasks is
  // a stacked 2-series (completed/failed) — mirrors DashboardPage's
  // DailyCostChart/DailyTokensChart/DailyTimeChart/DailyTasksChart (and
  // their Weekly siblings) exactly, per
  // packages/views/runtimes/components/charts/*.
  //
  // Explicit ChartStackRow/ChartBarRow types (rather than the two `as`
  // casts inferring loose inline shapes) so this stays checked against
  // react-native-gifted-charts' actual stackDataItem/barDataItem prop
  // types instead of `any`.
  type ChartStackRow = { label: string; stacks: { value: number; color: string }[] };
  type ChartBarRow = { label: string; value: number; frontColor: string };
  const chartBarData = useMemo<(ChartStackRow | ChartBarRow)[]>(() => {
    const stackColors = {
      input: theme.chart1,
      output: theme.chart2,
      cacheRead: theme.chart4,
      cacheWrite: theme.chart3,
      completed: theme.chart1,
      failed: theme.chart5,
      single: theme.chart1,
    };
    const stacked = (
      rows: { label: string }[],
      segments: { key: string; color: string }[],
      getValue: (row: any, key: string) => number,
    ): ChartStackRow[] =>
      rows.map((row) => ({
        label: row.label,
        stacks: segments.map((s) => ({ value: getValue(row, s.key), color: s.color })),
      }));

    if (metric === "cost") {
      const rows = dim === "weekly" ? weekly.weeklyCostStack : dailyCost;
      return stacked(
        rows,
        [
          { key: "input", color: stackColors.input },
          { key: "output", color: stackColors.output },
          { key: "cacheWrite", color: stackColors.cacheWrite },
        ],
        (r, k) => r[k],
      );
    }
    if (metric === "tokens") {
      const rows = dim === "weekly" ? weekly.weeklyTokens : dailyTokens;
      return stacked(
        rows,
        [
          { key: "input", color: stackColors.input },
          { key: "output", color: stackColors.output },
          { key: "cacheRead", color: stackColors.cacheRead },
          { key: "cacheWrite", color: stackColors.cacheWrite },
        ],
        (r, k) => r[k],
      );
    }
    if (metric === "time") {
      const rows = dim === "weekly" ? weeklyTime : dailyTime;
      return rows.map((row) => ({ label: row.label, value: row.totalSeconds, frontColor: stackColors.single }));
    }
    const rows = dim === "weekly" ? weeklyTasks : dailyTasks;
    return stacked(
      rows,
      [
        { key: "completed", color: stackColors.completed },
        { key: "failed", color: stackColors.failed },
      ],
      (r, k) => r[k],
    );
  }, [metric, dim, dailyCost, dailyTokens, dailyTime, dailyTasks, weekly, weeklyTime, weeklyTasks, theme]);

  // Empty-state mirrors DashboardPage's per-metric `isEmpty` (sum/every of
  // the metric's own values), NOT chartBarData.length — aggregateByWeek /
  // aggregateWeeklyTime / aggregateWeeklyTasks always emit `weekCount` week
  // shells regardless of underlying data (see buildWeekShells), so a length
  // check can never be 0 in the weekly dimension and would silently break
  // the empty-state on a brand-new workspace viewed in Weekly mode.
  const chartIsEmpty = useMemo(
    () => chartBarData.every((row) => ("stacks" in row ? row.stacks.every((s) => s.value === 0) : row.value === 0)),
    [chartBarData],
  );

  const [sortBy, setSortBy] = useState<LeaderboardSort>("tokens");

  const agentTokenRows = useMemo(() => aggregateAgentTokens(byAgentUsage), [byAgentUsage]);
  const agentRows = useMemo(
    () => mergeAgentDashboardRows(agentTokenRows, runTimeRows),
    [agentTokenRows, runTimeRows],
  );
  // Skip bucketing until the agent list has loaded so a slow agents fetch
  // doesn't transiently merge every row into "Deleted agents" — mirrors
  // packages/views/dashboard/components/dashboard-page.tsx's knownAgentIds.
  const knownAgentIds = useMemo(
    () => (agentsQuery.isSuccess ? new Set(agents.map((a) => a.id)) : null),
    [agentsQuery.isSuccess, agents],
  );
  const visibleAgentRows = useMemo(
    () => bucketUnknownAgentRows(agentRows, knownAgentIds),
    [agentRows, knownAgentIds],
  );
  const deletedAgentCount = useMemo(
    () => (knownAgentIds ? agentRows.filter((r) => !knownAgentIds.has(r.agentId)).length : 0),
    [agentRows, knownAgentIds],
  );

  const sortedRows = useMemo(() => {
    const metricFn = SORT_METRIC[sortBy];
    return [...visibleAgentRows].sort((a, b) => metricFn(b) - metricFn(a));
  }, [visibleAgentRows, sortBy]);
  const maxValue = useMemo(() => {
    const metricFn = SORT_METRIC[sortBy];
    return sortedRows.reduce((m, r) => Math.max(m, metricFn(r)), 0);
  }, [sortedRows, sortBy]);

  return (
    <SafeAreaView className="flex-1 bg-background" edges={[]}>
      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : error ? (
        <View className="px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            {t("error.load_prefix")} {error instanceof Error ? error.message : t("error.unknown")}
          </Text>
          <Button variant="outline" onPress={refetchAll}>
            <Text>{t("error.retry")}</Text>
          </Button>
        </View>
      ) : (
        <ScrollView contentContainerClassName="pb-6">
          <View className="flex-row flex-wrap items-center gap-2 px-4 pt-4">
            <Button variant="outline" size="sm" onPress={openProjectPicker}>
              <Text numberOfLines={1}>{selectedProjectTitle}</Text>
            </Button>
            <SegmentedControl
              values={[t("dim.daily"), t("dim.weekly")]}
              selectedIndex={dim === "daily" ? 0 : 1}
              onChange={(e) =>
                handleDimChange(e.nativeEvent.selectedSegmentIndex === 0 ? "daily" : "weekly")
              }
              style={{ width: 140 }}
            />
            <SegmentedControl
              values={allowedRanges.map((r) => r.label)}
              selectedIndex={Math.max(0, allowedRanges.findIndex((r) => r.days === days))}
              onChange={(e) => setDays(allowedRanges[e.nativeEvent.selectedSegmentIndex].days)}
              style={{ width: allowedRanges.length * 46 }}
            />
          </View>

          <View className="flex-row flex-wrap px-2 pt-2">
            <UsageStatCard label={t("kpi.cost_label", { days })} value={fmtMoney(totals.cost)} />
            <UsageStatCard
              label={t("kpi.tokens_label", { days })}
              value={formatTokens(totals.input + totals.output + totals.cacheRead + totals.cacheWrite)}
              hint={t("kpi.tokens_hint", { input: formatTokens(totals.input), output: formatTokens(totals.output) })}
            />
            <UsageStatCard
              label={t("kpi.run_time_label", { days })}
              value={formatDuration(runTimeTotals.totalSeconds, t("duration.less_than_minute"))}
              hint={t("kpi.run_time_hint", { tasks: runTimeTotals.taskCount })}
            />
            <UsageStatCard
              label={t("kpi.tasks_label", { days })}
              value={String(runTimeTotals.taskCount)}
              hint={t("kpi.tasks_hint", { failed: runTimeTotals.failedCount })}
            />
          </View>

          <View className="px-4 pt-4">
            <SegmentedControl
              values={[t("metric.tokens"), t("metric.cost"), t("metric.time"), t("metric.tasks")]}
              selectedIndex={["tokens", "cost", "time", "tasks"].indexOf(metric)}
              onChange={(e) =>
                setMetric((["tokens", "cost", "time", "tasks"] as const)[e.nativeEvent.selectedSegmentIndex])
              }
            />
            <View className="pt-3">
              {chartIsEmpty ? (
                <Text className="text-sm text-muted-foreground text-center py-8">{t("empty.title")}</Text>
              ) : metric === "time" ? (
                <BarChart
                  data={chartBarData as ChartBarRow[]}
                  height={180}
                  barWidth={dim === "weekly" ? 24 : 12}
                  spacing={dim === "weekly" ? 16 : 6}
                  noOfSections={4}
                  yAxisTextStyle={{ color: theme.mutedForeground }}
                  xAxisLabelTextStyle={{ color: theme.mutedForeground, fontSize: 10 }}
                  formatYLabel={(v: string) => formatDuration(Number(v), lessThanMinuteLabel)}
                />
              ) : (
                <BarChart
                  stackData={chartBarData as ChartStackRow[]}
                  height={180}
                  barWidth={dim === "weekly" ? 24 : 12}
                  spacing={dim === "weekly" ? 16 : 6}
                  noOfSections={4}
                  yAxisTextStyle={{ color: theme.mutedForeground }}
                  xAxisLabelTextStyle={{ color: theme.mutedForeground, fontSize: 10 }}
                  formatYLabel={(v: string) =>
                    metric === "cost" ? fmtMoney(Number(v)) : metric === "tokens" ? formatTokens(Number(v)) : v
                  }
                />
              )}
            </View>
          </View>

          <View className="mt-4 border-t border-border">
            <View className="flex-row items-center justify-between px-4 pt-4 pb-2">
              <Text className="text-sm font-semibold text-foreground">{t("leaderboard.title")}</Text>
              <Text className="text-xs text-muted-foreground">
                {deletedAgentCount > 0
                  ? t("leaderboard.caption_with_deleted", {
                      count: visibleAgentRows.length - 1,
                      deleted: deletedAgentCount,
                    })
                  : t("leaderboard.caption", { count: visibleAgentRows.length })}
              </Text>
            </View>
            <SegmentedControl
              values={[t("leaderboard.sort_tokens"), t("leaderboard.sort_cost"), t("leaderboard.sort_time"), t("leaderboard.sort_tasks")]}
              selectedIndex={(["tokens", "cost", "time", "tasks"] as const).indexOf(sortBy)}
              onChange={(e) =>
                setSortBy((["tokens", "cost", "time", "tasks"] as const)[e.nativeEvent.selectedSegmentIndex])
              }
              style={{ marginHorizontal: 16, marginBottom: 8 }}
            />
            {sortedRows.length === 0 ? (
              <Text className="text-sm text-muted-foreground text-center py-6">{t("empty.title")}</Text>
            ) : (
              sortedRows.map((row) => {
                const isDeletedBucket = row.agentId === DELETED_AGENTS_ROW_ID;
                const value = SORT_METRIC[sortBy](row);
                const pct = maxValue > 0 ? (value / maxValue) * 100 : 0;
                return (
                  <View key={row.agentId} className="flex-row items-center gap-3 px-4 py-2.5">
                    {isDeletedBucket ? (
                      <View className="h-6 w-6 items-center justify-center rounded-full bg-muted">
                        <Ionicons name="trash-outline" size={14} color={theme.mutedForeground} />
                      </View>
                    ) : (
                      <ActorAvatar type="agent" id={row.agentId} size={24} />
                    )}
                    <View className="flex-1 gap-1">
                      <Text
                        numberOfLines={1}
                        className={isDeletedBucket ? "text-sm italic text-muted-foreground" : "text-sm font-medium text-foreground"}
                      >
                        {isDeletedBucket ? t("leaderboard.deleted_agents") : (agents.find((a) => a.id === row.agentId)?.name ?? row.agentId)}
                      </Text>
                      <View className="h-1.5 rounded-full bg-muted overflow-hidden">
                        <View style={{ width: `${pct}%` }} className="h-full rounded-full bg-primary" />
                      </View>
                    </View>
                    <Text className="text-xs tabular-nums text-muted-foreground w-16 text-right">
                      {formatTokens(row.tokens)}
                    </Text>
                    <Text className="text-xs tabular-nums text-muted-foreground w-14 text-right">
                      {fmtMoney(row.cost)}
                    </Text>
                  </View>
                );
              })
            )}
          </View>
        </ScrollView>
      )}
    </SafeAreaView>
  );
}
