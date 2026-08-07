"use client";

import { useMemo, useState } from "react";
import { BarChart3, RefreshCw } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import {
  CompactNumberFlow,
  CurrencyNumberFlow,
  NumberFlow,
} from "@multica/ui/components/ui/number-flow";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Agent } from "@multica/core/types";
import { agentListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import {
  dashboardKeys,
  dashboardUsageDailyOptions,
  dashboardUsageByAgentOptions,
  dashboardAgentRunTimeOptions,
  dashboardRunTimeDailyOptions,
  dashboardFailuresDailyOptions,
  dashboardFailuresByAgentOptions,
} from "@multica/core/dashboard";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";
import { useViewingTimezone } from "../../common/use-viewing-timezone";
import { PageHeader } from "../../layout/page-header";
import { KpiCard } from "../../runtimes/components/shared";
import { useNavigation } from "../../navigation";
import {
  addDaysIso,
  aggregateByWeek,
  formatTokens,
  todayIso,
} from "../../runtimes/utils";
import { useT } from "../../i18n";
import {
  aggregateAgentFailures,
  aggregateAgentTokens,
  aggregateDailyCost,
  aggregateDailyErrors,
  aggregateDailyTasks,
  aggregateDailyTime,
  aggregateDailyTokens,
  aggregateFailureClasses,
  aggregateFailureReasons,
  aggregateWeeklyErrors,
  aggregateWeeklyTasks,
  aggregateWeeklyTime,
  bucketUnknownAgentRows,
  anonymizeUnresolvedAgentRows,
  computeDailyTotals,
  computeFailureTotals,
  isSyntheticAgentRow,
  mergeAgentDashboardRows,
} from "../utils";
import {
  ALL_PROJECTS,
  DurationNumberFlow,
  dimsForDays,
  type TimeRange,
} from "./dashboard-shared";
import { ProjectFilter, TimeRangeFilter } from "./dashboard-filters";
import { UsageTrendCard } from "./usage-trend-card";
import { Leaderboard } from "./leaderboard";
import { ErrorsTab } from "./errors-tab";

// Stable references — `data ?? []` would create a new empty array on
// every render while the query is loading, which breaks useMemo's
// reference-equality dep check and trips the exhaustive-deps lint rule.
const EMPTY_DAILY: import("@multica/core/types").DashboardUsageDaily[] = [];
const EMPTY_BY_AGENT: import("@multica/core/types").DashboardUsageByAgent[] = [];
const EMPTY_RUNTIME: import("@multica/core/types").DashboardAgentRunTime[] = [];
const EMPTY_RUNTIME_DAILY: import("@multica/core/types").DashboardRunTimeDaily[] = [];
const EMPTY_FAILURE_DAILY: import("@multica/core/types").DashboardFailureDaily[] = [];
const EMPTY_FAILURE_BY_AGENT: import("@multica/core/types").DashboardFailureByAgent[] =
  [];
const EMPTY_AGENTS: Agent[] = [];

type DashboardTab = "usage" | "errors";
const TAB_QUERY_KEY = "tab";
const DEFAULT_TAB: DashboardTab = "usage";

/** Local time of the most recent successful fetch, in the viewer's timezone.
 *  Every number on this page is bucketed on that timezone, so the header says
 *  which one it is — the same figures under a different tz are a different
 *  answer, and nothing on the page used to admit that. */
function useDataFreshness(
  updatedAts: (number | undefined)[],
  viewTZ: string,
  locales: Intl.LocalesArgument,
): { tzLabel: string | null; updatedLabel: string | null } {
  return useMemo(() => {
    const stamps = updatedAts.filter(
      (n): n is number => typeof n === "number" && n > 0,
    );
    const latest = stamps.length > 0 ? Math.max(...stamps) : null;
    // A stored timezone is user input and reaches us unvalidated; Intl throws
    // on a string it does not recognise, and a header label is not worth
    // taking the page down for.
    try {
      const tzLabel =
        new Intl.DateTimeFormat(locales, {
          timeZone: viewTZ,
          timeZoneName: "shortOffset",
        })
          .formatToParts(new Date())
          .find((part) => part.type === "timeZoneName")?.value ?? null;
      const updatedLabel =
        latest === null
          ? null
          : new Intl.DateTimeFormat(locales, {
              timeZone: viewTZ,
              hour: "2-digit",
              minute: "2-digit",
            }).format(new Date(latest));
      return { tzLabel, updatedLabel };
    } catch {
      return { tzLabel: null, updatedLabel: null };
    }
    // `updatedAts` is a fresh array each render; spreading it into the dep list
    // keeps the memo keyed on the values rather than the identity.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...updatedAts, viewTZ, locales]);
}

/**
 * Workspace + project usage dashboard.
 *
 * Lives at `/{slug}/usage`. Two tabs, split by the question the reader arrived
 * with rather than by which rollup feeds them: Usage answers "what did this
 * cost", Errors answers "what broke". They used to share one scrolling page,
 * where the failure breakdown sat below a leaderboard that could itself run to
 * thirty rows, and the only way to chart failures was to hide spend.
 *
 * Scope is expressed by where a control lives: the toolbar under the header
 * carries the tabs and the two page-scoped filters (time range, project),
 * every card carries its own view switches. All six rollups are fetched for
 * both tabs — they are small, and prefetching is what makes switching tabs
 * instant — but the loading and empty states are per tab, so Usage does not
 * wait on the failure queries.
 *
 * Cost math runs client-side via the runtimes utils — keeps the dashboard
 * and the runtime page using one pricing table.
 */
export function DashboardPage() {
  const { t, i18n } = useT("usage");
  const wsId = useWorkspaceId();
  const viewTZ = useViewingTimezone();
  const navigation = useNavigation();
  const locales = i18n.resolvedLanguage ?? i18n.language;
  const [days, setDays] = useState<TimeRange>(30);
  const [projectValue, setProjectValue] = useState<string>(ALL_PROJECTS);

  // The tab lives in the URL because the Errors view is the half of this page
  // people paste at each other ("this agent failed 54 times, see for
  // yourself"); component state cannot be linked to. `replace`, not `push`, so
  // flipping tabs does not stack up history entries. An unknown ?tab= value
  // falls back to Usage rather than rendering nothing.
  const tabFromUrl = navigation.searchParams.get(TAB_QUERY_KEY);
  const tab: DashboardTab = tabFromUrl === "errors" ? "errors" : DEFAULT_TAB;
  const handleTabChange = (next: string) => {
    const params = new URLSearchParams(navigation.searchParams);
    if (next === DEFAULT_TAB) params.delete(TAB_QUERY_KEY);
    else params.set(TAB_QUERY_KEY, next);
    const query = params.toString();
    navigation.replace(query ? `${navigation.pathname}?${query}` : navigation.pathname);
  };

  // The user can save model prices from the runtimes page; re-render when
  // they do so the dashboard reflects the new rates.
  useCustomPricingStore((s) => s.pricings);

  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const agentsQuery = useQuery(agentListOptions(wsId));
  const agents = agentsQuery.data ?? EMPTY_AGENTS;

  // Validate the picked project against the current workspace's list. A
  // stale UUID — left over from a project that's been deleted, or from the
  // previous workspace after a switch — would silently filter every query to
  // empty rows while the header still reads "All projects". Derive the
  // effective filter so the API call matches the user-visible selection.
  const projectId = useMemo(() => {
    if (projectValue === ALL_PROJECTS) return null;
    return projects.some((p) => p.id === projectValue) ? projectValue : null;
  }, [projectValue, projects]);

  // The weekly charts paint `ceil(days / 7)` trailing calendar weeks anchored
  // at today-in-UTC. In the worst case (today = Sunday) the leftmost Monday
  // sits `weekCount * 7 - 1` days back, so a vanilla `days=30` request would
  // silently truncate the leftmost bucket. Over-fetch the per-date queries to
  // cover the full first week.
  //
  // Unconditionally, not only when a chart is weekly: the dimension is now a
  // card-level control, so the page cannot know which grain is on screen — and
  // fetching for the wider of the two means flipping a card between Daily and
  // Weekly never refetches. Daily aggregations trim back to exactly `days`
  // client-side via `dailyCutoffIso` below, so the extra rows change nothing
  // they show. The per-agent rollups stay at `days` so KPI/leaderboard labels
  // (e.g. "Tasks · 30D") keep their advertised window.
  const weekCount = Math.max(1, Math.ceil(days / 7));
  const chartFetchDays = weekCount * 7;

  const dailyQuery = useQuery(
    dashboardUsageDailyOptions(wsId, chartFetchDays, projectId, viewTZ),
  );
  // The three per-agent rollups carry no date, so `dailyCutoffIso` below
  // cannot trim them — their window is closed server-side at exactly `days`
  // calendar buckets (parseExactSinceParamInTZ). Anything derived from these
  // three is therefore already on the same span as the trimmed daily series;
  // do NOT put a per-agent rollup back on the N+1 cutoff, or the leaderboard
  // and the Run time / Tasks KPIs silently widen by one day while the chart
  // and the Cost / Tokens KPIs beside them do not (MUL-5551).
  const byAgentQuery = useQuery(
    dashboardUsageByAgentOptions(wsId, days, projectId, viewTZ),
  );
  const runTimeQuery = useQuery(
    dashboardAgentRunTimeOptions(wsId, days, projectId, viewTZ),
  );
  const runTimeDailyQuery = useQuery(
    dashboardRunTimeDailyOptions(wsId, chartFetchDays, projectId, viewTZ),
  );
  const failuresDailyQuery = useQuery(
    dashboardFailuresDailyOptions(wsId, chartFetchDays, projectId, viewTZ),
  );
  const failuresByAgentQuery = useQuery(
    dashboardFailuresByAgentOptions(wsId, days, projectId, viewTZ),
  );

  const dailyUsage = dailyQuery.data ?? EMPTY_DAILY;
  const byAgentUsage = byAgentQuery.data ?? EMPTY_BY_AGENT;
  const runTimeRows = runTimeQuery.data ?? EMPTY_RUNTIME;
  const runTimeDailyRows = runTimeDailyQuery.data ?? EMPTY_RUNTIME_DAILY;
  const failureDailyRows = failuresDailyQuery.data ?? EMPTY_FAILURE_DAILY;
  const failureByAgentRows = failuresByAgentQuery.data ?? EMPTY_FAILURE_BY_AGENT;

  const queryClient = useQueryClient();
  // "Refreshing" covers any of the six rollups being in flight, whichever
  // trigger started it (button, interval, mount) — the header spinner and the
  // timestamp describe the same set of queries.
  const isRefreshing =
    dailyQuery.isFetching ||
    byAgentQuery.isFetching ||
    runTimeQuery.isFetching ||
    runTimeDailyQuery.isFetching ||
    failuresDailyQuery.isFetching ||
    failuresByAgentQuery.isFetching;
  const handleRefresh = () => {
    void queryClient.invalidateQueries({ queryKey: dashboardKeys.all(wsId) });
  };

  const { tzLabel, updatedLabel } = useDataFreshness(
    [
      dailyQuery.dataUpdatedAt,
      byAgentQuery.dataUpdatedAt,
      runTimeQuery.dataUpdatedAt,
      runTimeDailyQuery.dataUpdatedAt,
      failuresDailyQuery.dataUpdatedAt,
      failuresByAgentQuery.dataUpdatedAt,
    ],
    viewTZ,
    locales,
  );

  // Daily-aggregation surfaces re-scope to the user-selected `days` even
  // though the per-date queries over-fetch for the weekly charts. The cutoff is
  // anchored on the viewer's timezone — the same axis the backend slices
  // `bucket_hour` on — so it lands on the same calendar boundary. Applied in
  // both dims so 1d strictly means "today" even at the midnight edge where a
  // wall-clock cutoff would otherwise include yesterday.
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
  const failureDailyInWindow = useMemo(
    () => failureDailyRows.filter((r) => r.date >= dailyCutoffIso),
    [failureDailyRows, dailyCutoffIso],
  );

  // Loading and empty are per tab: the Usage tab has no reason to wait on the
  // two failure rollups, and a workspace with spend but no failures is not an
  // empty dashboard.
  const usageLoading =
    dailyQuery.isLoading ||
    byAgentQuery.isLoading ||
    runTimeQuery.isLoading ||
    runTimeDailyQuery.isLoading;
  const errorsLoading =
    failuresDailyQuery.isLoading || failuresByAgentQuery.isLoading;

  const usageHasNoData =
    !usageLoading &&
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
  const dailyErrors = useMemo(
    () => aggregateDailyErrors(failureDailyInWindow),
    [failureDailyInWindow],
  );

  // Failure summaries.
  //
  // Totals / classes / reasons are derived from the DATE-BUCKETED rollup after
  // the same `dailyCutoffIso` trim the charts use, not from the per-agent one.
  // `parseSinceParamInTZ` deliberately returns N+1 calendar days of headroom
  // (see sinceFromDays in server/internal/handler/runtime.go), and only a
  // series carrying a date can trim that back client-side. Reading these off
  // the per-agent rollup put the summary one calendar day wider than the chart
  // beside it — at 1D the chart could show no failures while the tile counted
  // yesterday's.
  const failureTotals = useMemo(
    () => computeFailureTotals(failureDailyInWindow),
    [failureDailyInWindow],
  );
  const failureClassRows = useMemo(
    () => aggregateFailureClasses(failureDailyInWindow),
    [failureDailyInWindow],
  );
  const failureReasonRows = useMemo(
    () => aggregateFailureReasons(failureDailyInWindow),
    [failureDailyInWindow],
  );
  // Which agent ids this viewer can actually resolve to a name. Declared here
  // rather than next to the leaderboard because the Errors aggregation below
  // needs it too — see anonymizeUnresolvedAgentRows.
  const knownAgentIds = useMemo(
    () => (agentsQuery.isSuccess ? new Set(agents.map((a) => a.id)) : null),
    [agentsQuery.isSuccess, agents],
  );

  // The per-agent split has no date to trim on, so its window is closed
  // server-side instead — GetDashboardFailuresByAgent uses the exact N-day
  // cutoff rather than the N+1 one.
  //
  // Anonymize BEFORE aggregating: the sentinel then behaves like any other
  // agent id, so the bucket's failure classes are summed from real
  // per-(agent, reason) rows instead of being reconstructed from rows that
  // have already collapsed to a single dominant class.
  const agentFailureRows = useMemo(
    () =>
      aggregateAgentFailures(
        anonymizeUnresolvedAgentRows(failureByAgentRows, knownAgentIds),
      ),
    [failureByAgentRows, knownAgentIds],
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
  const weeklyErrors = useMemo(
    () => aggregateWeeklyErrors(failureDailyRows, viewTZ, weekCount),
    [failureDailyRows, viewTZ, weekCount],
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

  // Fold rollup rows for hard-deleted agents into one aggregated "Deleted
  // agents" row instead of showing them as a bare UUID (MUL-3771) or dropping
  // them outright — dropping made the per-agent breakdown stop reconciling
  // with the top-line Cost/Tokens KPIs, which still count that spend (MUL-3776,
  // #4640). Archived agents stay as themselves (the agent list is fetched with
  // archived included); only truly-removed agents collapse into the bucket.
  // Skip bucketing until the agent list has loaded so a slow agents fetch
  // doesn't transiently merge every row.
  const visibleAgentRows = useMemo(
    () => bucketUnknownAgentRows(agentRows, knownAgentIds),
    [agentRows, knownAgentIds],
  );
  // Distinct hard-deleted agents folded into the bucket — drives the caption's
  // "· N deleted" suffix (the bucket itself is a single row). The server's
  // restricted bucket is not in `knownAgentIds` either but is not a deletion,
  // so it must not inflate this count — that mislabelling is exactly the bug
  // MUL-5409 came with.
  const deletedAgentCount = useMemo(
    () =>
      knownAgentIds
        ? agentRows.filter(
            (r) => !knownAgentIds.has(r.agentId) && !isSyntheticAgentRow(r.agentId),
          ).length
        : 0,
    [agentRows, knownAgentIds],
  );

  const allowedDims = dimsForDays(days);
  const lessThanMinuteLabel = t(($) => $.duration.less_than_minute);

  return (
    <Tabs
      value={tab}
      onValueChange={handleTabChange}
      className="flex h-full min-h-0 flex-col gap-0"
    >
      <PageHeader className="justify-between gap-2 px-5">
        <div className="flex min-w-0 items-center gap-2">
          <BarChart3 className="h-4 w-4 shrink-0 text-muted-foreground" />
          <h1 className="text-body font-medium">{t(($) => $.title)}</h1>
        </div>
        {/* Data freshness cluster: the timestamp and the action that advances
            it stay together. Refresh re-pulls the same scope, so it lives here
            with the page metadata rather than among the scope controls. */}
        <div className="flex shrink-0 items-center gap-1">
          {tzLabel ? (
            <span className="hidden text-caption text-muted-foreground lg:inline">
              {updatedLabel
                ? t(($) => $.header.timezone_and_updated, {
                    tz: tzLabel,
                    time: updatedLabel,
                  })
                : tzLabel}
            </span>
          ) : null}
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t(($) => $.header.refresh)}
            onClick={handleRefresh}
            disabled={isRefreshing}
          >
            <RefreshCw className={isRefreshing ? "animate-spin" : undefined} />
          </Button>
        </div>
      </PageHeader>

      {/* View toolbar, same grammar as the issues surface header: view
          switching on the left, page-scoped filters on the right. Both tabs
          share the range and project filter, which is why the filters live
          here and not inside a tab. */}
      <div className="h-12 shrink-0 overflow-x-auto border-b px-5 [-webkit-overflow-scrolling:touch]">
        <div className="flex h-full w-max min-w-full items-center justify-between gap-2">
          <TabsList variant="line" className="gap-0 p-0 group-data-horizontal/tabs:h-full">
            <TabsTrigger
              value="usage"
              className="h-full rounded-none px-2.5 text-label group-data-horizontal/tabs:after:bottom-0"
            >
              {t(($) => $.tab_usage)}
            </TabsTrigger>
            <TabsTrigger
              value="errors"
              className="h-full rounded-none px-2.5 text-label group-data-horizontal/tabs:after:bottom-0"
            >
              {t(($) => $.errors.title)}
            </TabsTrigger>
          </TabsList>
          <div className="flex shrink-0 items-center gap-2">
            <TimeRangeFilter days={days} onChange={setDays} />
            <ProjectFilter
              projects={projects}
              projectValue={projectValue}
              onProjectChange={setProjectValue}
            />
          </div>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-6xl p-6">
          <TabsContent value="usage">
            {usageLoading ? (
              <DashboardSkeleton />
            ) : usageHasNoData ? (
              <DashboardEmpty />
            ) : (
              <>
                {/* KPI row — same 3-divide-x card grid the runtime usage
                    section uses, expanded to four tiles. */}
                <div className="grid grid-cols-1 divide-y rounded-lg border bg-card sm:grid-cols-2 sm:divide-x sm:divide-y-0 lg:grid-cols-4">
                  <KpiCard
                    label={t(($) => $.kpi.cost_label, { days })}
                    value={<CurrencyNumberFlow value={totals.cost} locales={locales} />}
                  />
                  <KpiCard
                    label={t(($) => $.kpi.tokens_label, { days })}
                    value={
                      <CompactNumberFlow
                        value={
                          totals.input +
                          totals.output +
                          totals.cacheRead +
                          totals.cacheWrite
                        }
                        locales={locales}
                      />
                    }
                    hint={t(($) => $.kpi.tokens_hint, {
                      input: formatTokens(totals.input),
                      output: formatTokens(totals.output),
                    })}
                  />
                  <KpiCard
                    label={t(($) => $.kpi.run_time_label, { days })}
                    value={
                      <DurationNumberFlow
                        seconds={runTimeTotals.totalSeconds}
                        lessThanMinuteLabel={lessThanMinuteLabel}
                        locales={locales}
                      />
                    }
                    hint={t(($) => $.kpi.run_time_hint, {
                      tasks: runTimeTotals.taskCount,
                    })}
                  />
                  <KpiCard
                    label={t(($) => $.kpi.tasks_label, { days })}
                    value={
                      <NumberFlow
                        value={runTimeTotals.taskCount}
                        locales={locales}
                        format={{ maximumFractionDigits: 0 }}
                        aria-label={String(runTimeTotals.taskCount)}
                      />
                    }
                    // Deliberately sourced from `runTimeTotals`, not the
                    // failure rollup: the tile's own value counts started tasks
                    // only, so quoting the failure rollup's larger failure count
                    // here would put two different denominators in one tile. The
                    // Errors tab states its rate with the denominator spelled
                    // out instead.
                    hint={t(($) => $.kpi.tasks_hint, {
                      failed: runTimeTotals.failedCount,
                    })}
                  />
                </div>

                <UsageTrendCard
                  allowedDims={allowedDims}
                  dailyCost={dailyCost}
                  dailyTokens={dailyTokens}
                  dailyTime={dailyTime}
                  dailyTasks={dailyTasks}
                  weeklyCost={weeklyCost}
                  weeklyTokens={weeklyTokens}
                  weeklyTime={weeklyTime}
                  weeklyTasks={weeklyTasks}
                  lessThanMinuteLabel={lessThanMinuteLabel}
                />

                <Leaderboard
                  rows={visibleAgentRows}
                  agents={agents}
                  deletedAgentCount={deletedAgentCount}
                  lessThanMinuteLabel={lessThanMinuteLabel}
                />
              </>
            )}
          </TabsContent>

          <TabsContent value="errors">
            {errorsLoading ? (
              <DashboardSkeleton />
            ) : (
              <ErrorsTab
                days={days}
                allowedDims={allowedDims}
                totals={failureTotals}
                classRows={failureClassRows}
                reasonRows={failureReasonRows}
                agentRows={agentFailureRows}
                dailyErrors={dailyErrors}
                weeklyErrors={weeklyErrors}
                agents={agents}
                locales={locales}
              />
            )}
          </TabsContent>
        </div>
      </div>
    </Tabs>
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
      <BarChart3 className="h-6 w-6 text-faint-foreground" />
      <p className="mt-3 text-body font-medium">{t(($) => $.empty.title)}</p>
      <p className="mt-1 max-w-md text-caption text-muted-foreground">
        {t(($) => $.empty.body)}
      </p>
    </div>
  );
}
