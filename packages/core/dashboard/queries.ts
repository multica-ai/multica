import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Workspace dashboard query options. All four endpoints share the same
// (wsId, days, projectId, squadId) key shape so workspace switching,
// time-range changes, the project filter, and the squad filter each
// invalidate the cache cleanly.
//
// `projectId` and `squadId` are normalised to `null` (not undefined /
// "all") so the queryKey shape is stable across renders when either
// dropdown sits on its "all" sentinel.

export const dashboardKeys = {
  all: (wsId: string) => ["dashboard", wsId] as const,
  daily: (
    wsId: string,
    days: number,
    projectId: string | null,
    squadId: string | null,
  ) => [...dashboardKeys.all(wsId), "daily", days, projectId, squadId] as const,
  byAgent: (
    wsId: string,
    days: number,
    projectId: string | null,
    squadId: string | null,
  ) =>
    [...dashboardKeys.all(wsId), "by-agent", days, projectId, squadId] as const,
  agentRuntime: (
    wsId: string,
    days: number,
    projectId: string | null,
    squadId: string | null,
  ) =>
    [
      ...dashboardKeys.all(wsId),
      "agent-runtime",
      days,
      projectId,
      squadId,
    ] as const,
  runTimeDaily: (
    wsId: string,
    days: number,
    projectId: string | null,
    squadId: string | null,
  ) =>
    [
      ...dashboardKeys.all(wsId),
      "runtime-daily",
      days,
      projectId,
      squadId,
    ] as const,
};

const STALE_TIME = 60 * 1000;

export function dashboardUsageDailyOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  squadId: string | null,
) {
  return queryOptions({
    queryKey: dashboardKeys.daily(wsId, days, projectId, squadId),
    queryFn: () =>
      api.getDashboardUsageDaily({
        days,
        project_id: projectId ?? undefined,
        squad_id: squadId ?? undefined,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });
}

export function dashboardUsageByAgentOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  squadId: string | null,
) {
  return queryOptions({
    queryKey: dashboardKeys.byAgent(wsId, days, projectId, squadId),
    queryFn: () =>
      api.getDashboardUsageByAgent({
        days,
        project_id: projectId ?? undefined,
        squad_id: squadId ?? undefined,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });
}

export function dashboardAgentRunTimeOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  squadId: string | null,
) {
  return queryOptions({
    queryKey: dashboardKeys.agentRuntime(wsId, days, projectId, squadId),
    queryFn: () =>
      api.getDashboardAgentRunTime({
        days,
        project_id: projectId ?? undefined,
        squad_id: squadId ?? undefined,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });
}

export function dashboardRunTimeDailyOptions(
  wsId: string,
  days: number,
  projectId: string | null,
  squadId: string | null,
) {
  return queryOptions({
    queryKey: dashboardKeys.runTimeDaily(wsId, days, projectId, squadId),
    queryFn: () =>
      api.getDashboardRunTimeDaily({
        days,
        project_id: projectId ?? undefined,
        squad_id: squadId ?? undefined,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });
}
