import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const dashboardKeys = {
  all: (wsId: string) => ["dashboard", wsId] as const,
  daily: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
  ) => [...dashboardKeys.all(wsId), "daily", days, projectId, tz] as const,
  byAgent: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
  ) => [...dashboardKeys.all(wsId), "by-agent", days, projectId, tz] as const,
  agentRuntime: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
  ) => [...dashboardKeys.all(wsId), "agent-runtime", days, projectId, tz] as const,
  runTimeDaily: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
  ) => [...dashboardKeys.all(wsId), "runtime-daily", days, projectId, tz] as const,
  failuresDaily: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
  ) => [...dashboardKeys.all(wsId), "failures-daily", days, projectId, tz] as const,
  failuresByAgent: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
  ) =>
    [...dashboardKeys.all(wsId), "failures-by-agent", days, projectId, tz] as const,
};

// The server materializes these rollups on a 5-minute cadence, so a mounted
// dashboard re-polls on that same cadence — polling faster would only re-read
// an unchanged rollup. The short staleTime keeps re-entering the page honest:
// anything older than a minute refetches on mount instead of waiting out the
// interval. Neither fires for unmounted queries or backgrounded windows.
const STALE_TIME = 60 * 1000;
const REFETCH_INTERVAL = 5 * 60 * 1000;

// Range changes should keep the previous result mounted so KPI cards and
// charts transition in place instead of falling back to a full-page skeleton.
// Scope changes are deliberately excluded: carrying data across workspaces,
// projects, report kinds, or timezones would briefly display the wrong data.
function isSameDashboardScope(
  previousKey: readonly unknown[] | undefined,
  nextKey: readonly unknown[],
): boolean {
  if (!previousKey || previousKey.length !== nextKey.length) return false;
  return previousKey.every(
    (part, index) => index === 3 || Object.is(part, nextKey[index]),
  );
}

// `tz` participates in every dashboard key so a Preferences change
// repoints the cache. Every series — token rollups and the
// atq.completed_at-based run-time / failure series — slices its day boundary
// in the viewer's tz, so all the dashboard tabs always agree.
export function dashboardUsageDailyOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.daily(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardUsageDaily({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    refetchInterval: REFETCH_INTERVAL,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function dashboardUsageByAgentOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.byAgent(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardUsageByAgent({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    refetchInterval: REFETCH_INTERVAL,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function dashboardAgentRunTimeOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.agentRuntime(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardAgentRunTime({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    refetchInterval: REFETCH_INTERVAL,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function dashboardRunTimeDailyOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.runTimeDaily(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardRunTimeDaily({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    refetchInterval: REFETCH_INTERVAL,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function dashboardFailuresDailyOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.failuresDaily(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardFailuresDaily({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    refetchInterval: REFETCH_INTERVAL,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function dashboardFailuresByAgentOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
) {
  const queryKey = dashboardKeys.failuresByAgent(wsId, days, projectId, tz);
  return queryOptions({
    queryKey,
    queryFn: () =>
      api.getDashboardFailuresByAgent({
        days,
        project_id: projectId ?? undefined,
        tz,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
    refetchInterval: REFETCH_INTERVAL,
    placeholderData: (previousData, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}
