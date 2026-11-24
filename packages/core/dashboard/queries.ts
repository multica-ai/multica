import { keepPreviousData, queryOptions, type QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { DashboardRequestParams } from "../api/client";
import {
  DashboardHistoryLimitTightenedError,
  isDashboardHistoryLimitTightened,
  isStricterHistoryLimit,
  reportDashboardHistoryLimit,
} from "./history-limit";

/**
 * The plan's history limit a dashboard query is made under: a day count, null
 * for none, or undefined while it is still loading. Every key carries it, so a
 * plan change is a new key: no cached result or placeholder from the old limit
 * is shown under the new one (MUL-9490).
 */
export type DashboardHistoryLimitParam = number | null | undefined;

type DashboardKeyPart =
  | "daily"
  | "by-agent"
  | "agent-runtime"
  | "runtime-daily"
  | "failures-daily"
  | "failures-by-agent";

const dashboardKey = (
  wsId: string,
  part: DashboardKeyPart,
  days: number,
  projectId: string | null,
  tz: string,
  historyLimit: DashboardHistoryLimitParam = null,
) => ["dashboard", wsId, part, days, projectId, tz, historyLimit ?? null] as const;

export const dashboardKeys = {
  all: (wsId: string) => ["dashboard", wsId] as const,
  daily: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
    historyLimit?: DashboardHistoryLimitParam,
  ) => dashboardKey(wsId, "daily", days, projectId, tz, historyLimit),
  byAgent: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
    historyLimit?: DashboardHistoryLimitParam,
  ) => dashboardKey(wsId, "by-agent", days, projectId, tz, historyLimit),
  agentRuntime: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
    historyLimit?: DashboardHistoryLimitParam,
  ) => dashboardKey(wsId, "agent-runtime", days, projectId, tz, historyLimit),
  runTimeDaily: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
    historyLimit?: DashboardHistoryLimitParam,
  ) => dashboardKey(wsId, "runtime-daily", days, projectId, tz, historyLimit),
  failuresDaily: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
    historyLimit?: DashboardHistoryLimitParam,
  ) => dashboardKey(wsId, "failures-daily", days, projectId, tz, historyLimit),
  failuresByAgent: (
    wsId: string,
    days: number,
    projectId: string | null,
    tz: string,
    historyLimit?: DashboardHistoryLimitParam,
  ) => dashboardKey(wsId, "failures-by-agent", days, projectId, tz, historyLimit),
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
// projects, report kinds, timezones or history limits would briefly display
// the wrong data — under a tightened limit, a range the plan no longer covers.
function isSameDashboardScope(
  previousKey: readonly unknown[] | undefined,
  nextKey: readonly unknown[],
): boolean {
  if (!previousKey || previousKey.length !== nextKey.length) return false;
  return previousKey.every(
    (part, index) => index === 3 || Object.is(part, nextKey[index]),
  );
}

type DashboardFetcher<T> = (params: DashboardRequestParams) => Promise<T[]>;

// `tz` participates in every dashboard key so a Preferences change
// repoints the cache. Every series — token rollups and the
// atq.completed_at-based run-time / failure series — slices its day boundary
// in the viewer's tz, so all the dashboard tabs always agree.
//
// A query waits until the history limit is known (`historyLimit` defined), so
// the first load is not fetched once under no limit and again under the real
// one. Each response reports the limit it was served under; one that differs
// from the limit this query was made under means the plan changed while the
// page was open, and reportDashboardHistoryLimit refreshes it. A response cut
// to a stricter limit than its key fails the query rather than caching rows
// that cover less than the key says.
function dashboardOptions<T>(
  part: DashboardKeyPart,
  fetcher: DashboardFetcher<T>,
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
  historyLimit: DashboardHistoryLimitParam,
) {
  const queryKey = dashboardKey(wsId, part, days, projectId, tz, historyLimit);
  return queryOptions({
    queryKey,
    queryFn: async ({ client }: { client: QueryClient }) => {
      const requestedUnder = historyLimit ?? null;
      let served: number | null | undefined;
      const rows = await fetcher({
        days,
        project_id: projectId ?? undefined,
        tz,
        onHistoryLimit: (limit) => {
          served = limit;
        },
      });
      if (served !== undefined) {
        reportDashboardHistoryLimit(client, wsId, requestedUnder, served);
        // Rows cut to a stricter limit than this key promises must not be
        // cached under it, or restoring the looser limit would show them.
        if (served !== null && isStricterHistoryLimit(served, requestedUnder)) {
          throw new DashboardHistoryLimitTightenedError(requestedUnder, served);
        }
      }
      return rows;
    },
    enabled: !!wsId && historyLimit !== undefined,
    // The app default retries once; a tightened limit is not transient, and
    // the page re-queries under the stricter key on its own.
    retry: (failureCount: number, error: Error) =>
      !isDashboardHistoryLimitTightened(error) && failureCount < 1,
    staleTime: STALE_TIME,
    refetchInterval: REFETCH_INTERVAL,
    placeholderData: (previousData: T[] | undefined, previousQuery) =>
      isSameDashboardScope(previousQuery?.queryKey, queryKey)
        ? keepPreviousData(previousData)
        : undefined,
  });
}

export function dashboardUsageDailyOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
  historyLimit: DashboardHistoryLimitParam,
) {
  return dashboardOptions(
    "daily",
    (params) => api.getDashboardUsageDaily(params),
    wsId,
    days,
    projectId,
    tz,
    historyLimit,
  );
}

export function dashboardUsageByAgentOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
  historyLimit: DashboardHistoryLimitParam,
) {
  return dashboardOptions(
    "by-agent",
    (params) => api.getDashboardUsageByAgent(params),
    wsId,
    days,
    projectId,
    tz,
    historyLimit,
  );
}

export function dashboardAgentRunTimeOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
  historyLimit: DashboardHistoryLimitParam,
) {
  return dashboardOptions(
    "agent-runtime",
    (params) => api.getDashboardAgentRunTime(params),
    wsId,
    days,
    projectId,
    tz,
    historyLimit,
  );
}

export function dashboardRunTimeDailyOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
  historyLimit: DashboardHistoryLimitParam,
) {
  return dashboardOptions(
    "runtime-daily",
    (params) => api.getDashboardRunTimeDaily(params),
    wsId,
    days,
    projectId,
    tz,
    historyLimit,
  );
}

export function dashboardFailuresDailyOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
  historyLimit: DashboardHistoryLimitParam,
) {
  return dashboardOptions(
    "failures-daily",
    (params) => api.getDashboardFailuresDaily(params),
    wsId,
    days,
    projectId,
    tz,
    historyLimit,
  );
}

export function dashboardFailuresByAgentOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  tz: string,
  historyLimit: DashboardHistoryLimitParam,
) {
  return dashboardOptions(
    "failures-by-agent",
    (params) => api.getDashboardFailuresByAgent(params),
    wsId,
    days,
    projectId,
    tz,
    historyLimit,
  );
}
