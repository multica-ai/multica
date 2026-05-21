import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Workspace dashboard query options. All three endpoints share the same
// (wsId, days, projectId) key shape so workspace switching, time-range
// changes, and the project filter each invalidate the cache cleanly.
//
// The cache key includes `wsId` explicitly: TanStack Query already isolates
// per workspace via the key, but threading wsId into the queryFn lets
// callers fail fast (return [] on empty wsId) instead of issuing a request
// the server would reject.
//
// `projectId` is normalised to `null` (not undefined / "all") so the
// queryKey shape is stable across renders even when the dropdown sits on
// "all projects".

export const dashboardKeys = {
  all: (wsId: string) => ["dashboard", wsId] as const,
  daily: (wsId: string, days: number, projectId: string | null) =>
    [...dashboardKeys.all(wsId), "daily", days, projectId] as const,
  byAgent: (wsId: string, days: number, projectId: string | null) =>
    [...dashboardKeys.all(wsId), "by-agent", days, projectId] as const,
  agentRuntime: (wsId: string, days: number, projectId: string | null) =>
    [...dashboardKeys.all(wsId), "agent-runtime", days, projectId] as const,
  runTimeDaily: (wsId: string, days: number, projectId: string | null) =>
    [...dashboardKeys.all(wsId), "runtime-daily", days, projectId] as const,
};

// 60s staleTime matches the per-runtime usage queries — the data is rollup-
// driven on the server (5-min rollup cadence) and the dashboard isn't a
// real-time view, so background refetches every minute are plenty.
const STALE_TIME = 60 * 1000;

export function dashboardUsageDailyOptions(
  wsId: string,
  days: number,
  projectId: string | null,
) {
  return queryOptions({
    queryKey: dashboardKeys.daily(wsId, days, projectId),
    queryFn: () =>
      api.getDashboardUsageDaily({ days, project_id: projectId ?? undefined }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });
}

export function dashboardUsageByAgentOptions(
  wsId: string,
  days: number,
  projectId: string | null,
) {
  return queryOptions({
    queryKey: dashboardKeys.byAgent(wsId, days, projectId),
    queryFn: () =>
      api.getDashboardUsageByAgent({ days, project_id: projectId ?? undefined }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });
}

export function dashboardAgentRunTimeOptions(
  wsId: string,
  days: number,
  projectId: string | null,
) {
  return queryOptions({
    queryKey: dashboardKeys.agentRuntime(wsId, days, projectId),
    queryFn: () =>
      api.getDashboardAgentRunTime({ days, project_id: projectId ?? undefined }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });
}

export function dashboardRunTimeDailyOptions(
  wsId: string,
  days: number,
  projectId: string | null,
) {
  return queryOptions({
    queryKey: dashboardKeys.runTimeDaily(wsId, days, projectId),
    queryFn: () =>
      api.getDashboardRunTimeDaily({ days, project_id: projectId ?? undefined }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });
}
