/**
 * Workspace usage analytics queries — mirrors packages/core/dashboard/queries.ts
 * but binds to mobile's api client + key shape (per mobile CLAUDE.md
 * "Mobile-owned updaters" rule: don't import web queries, copy the design).
 *
 * Keys are workspace-scoped (wsId) plus days/projectId/tz, so switching
 * workspace, scope, or the viewer's timezone all repoint the cache instead
 * of silently reusing a stale slice.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const usageKeys = {
  all: (wsId: string | null) => ["usage", wsId] as const,
  daily: (wsId: string | null, days: number, projectId: string | null, tz: string) =>
    [...usageKeys.all(wsId), "daily", days, projectId, tz] as const,
  byAgent: (wsId: string | null, days: number, projectId: string | null, tz: string) =>
    [...usageKeys.all(wsId), "by-agent", days, projectId, tz] as const,
  agentRuntime: (wsId: string | null, days: number, projectId: string | null, tz: string) =>
    [...usageKeys.all(wsId), "agent-runtime", days, projectId, tz] as const,
  runtimeDaily: (wsId: string | null, days: number, projectId: string | null, tz: string) =>
    [...usageKeys.all(wsId), "runtime-daily", days, projectId, tz] as const,
};

// 60s: matches web's dashboardKeys STALE_TIME (packages/core/dashboard/queries.ts) —
// the server rolls up usage on a 5-min cadence, so sub-minute refetches
// would just repeat the same numbers.
const STALE_TIME = 60 * 1000;

export const dashboardUsageDailyOptions = (
  wsId: string | null,
  days: number,
  projectId: string | null,
  tz: string,
) =>
  queryOptions({
    queryKey: usageKeys.daily(wsId, days, projectId, tz),
    queryFn: ({ signal }) =>
      api.getDashboardUsageDaily({ days, project_id: projectId, tz }, { signal }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });

export const dashboardUsageByAgentOptions = (
  wsId: string | null,
  days: number,
  projectId: string | null,
  tz: string,
) =>
  queryOptions({
    queryKey: usageKeys.byAgent(wsId, days, projectId, tz),
    queryFn: ({ signal }) =>
      api.getDashboardUsageByAgent({ days, project_id: projectId, tz }, { signal }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });

export const dashboardAgentRunTimeOptions = (
  wsId: string | null,
  days: number,
  projectId: string | null,
  tz: string,
) =>
  queryOptions({
    queryKey: usageKeys.agentRuntime(wsId, days, projectId, tz),
    queryFn: ({ signal }) =>
      api.getDashboardAgentRunTime({ days, project_id: projectId, tz }, { signal }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });

export const dashboardRunTimeDailyOptions = (
  wsId: string | null,
  days: number,
  projectId: string | null,
  tz: string,
) =>
  queryOptions({
    queryKey: usageKeys.runtimeDaily(wsId, days, projectId, tz),
    queryFn: ({ signal }) =>
      api.getDashboardRunTimeDaily({ days, project_id: projectId, tz }, { signal }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });
