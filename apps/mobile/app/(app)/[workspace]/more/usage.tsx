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
import SegmentedControl from "@react-native-segmented-control/segmented-control";
import type {
  DashboardAgentRunTime,
  DashboardRunTimeDaily,
  DashboardUsageByAgent,
  DashboardUsageDaily,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
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
import { addDaysIso, computeDailyTotals, formatDuration, todayIso } from "@/lib/usage-display";
import { fmtMoney, formatTokens } from "@/lib/usage-pricing";

type Dim = "daily" | "weekly";

// Stable references — `data ?? []` would create a new empty array on every
// render while a query is loading, which breaks useMemo's reference-equality
// dep check and trips the exhaustive-deps lint rule. Mirrors
// packages/views/dashboard/components/dashboard-page.tsx's EMPTY_* constants.
const EMPTY_DAILY: DashboardUsageDaily[] = [];
const EMPTY_BY_AGENT: DashboardUsageByAgent[] = [];
const EMPTY_RUNTIME: DashboardAgentRunTime[] = [];
const EMPTY_RUNTIME_DAILY: DashboardRunTimeDaily[] = [];

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
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

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
        </ScrollView>
      )}
    </SafeAreaView>
  );
}
