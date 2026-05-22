import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export interface AgentRunDashboardParams {
  days: number;
  agentIds: string[];
  ownerId?: string | null;
  startHour: number;
  endHour: number;
  timezone: string;
  limit?: number;
}

export const agentRunDashboardKeys = {
  all: (wsId: string) => ["agent-run-dashboard", wsId] as const,
  overview: (wsId: string, params: AgentRunDashboardParams) =>
    [
      ...agentRunDashboardKeys.all(wsId),
      "overview",
      params.days,
      [...params.agentIds].sort().join(","),
      params.ownerId ?? "",
      params.startHour,
      params.endHour,
      params.timezone,
      params.limit ?? 50,
    ] as const,
  detail: (wsId: string, taskId: string) =>
    [...agentRunDashboardKeys.all(wsId), "run", taskId] as const,
};

const STALE_TIME = 30 * 1000;

export function agentRunDashboardOptions(
  wsId: string,
  params: AgentRunDashboardParams,
) {
  return queryOptions({
    queryKey: agentRunDashboardKeys.overview(wsId, params),
    queryFn: () =>
      api.getAgentRunDashboard({
        days: params.days,
        agent_ids: params.agentIds,
        owner_id: params.ownerId ?? undefined,
        start_hour: params.startHour,
        end_hour: params.endHour,
        tz: params.timezone,
        limit: params.limit ?? 50,
      }),
    enabled: !!wsId,
    staleTime: STALE_TIME,
  });
}

export function agentRunDashboardRunDetailOptions(
  wsId: string,
  taskId: string | null,
) {
  return queryOptions({
    queryKey: agentRunDashboardKeys.detail(wsId, taskId ?? ""),
    queryFn: () => api.getAgentRunDashboardRunDetail(taskId ?? ""),
    enabled: !!wsId && !!taskId,
    staleTime: STALE_TIME,
  });
}
