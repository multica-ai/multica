import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const agentUsageKeys = {
  all: ["agent-usage"] as const,
  planLimits: () => [...agentUsageKeys.all, "plan-limits"] as const,
};

// The broker polls Anthropic every ~5min and the values move slowly, so a 60s
// stale window plus a 5min background refetch keeps the toolbar bars fresh
// without hammering the endpoint. Not workspace-scoped: the OAuth broker holds
// a single account, so the snapshot is identical across workspaces.
const STALE_TIME = 60 * 1000;
const REFETCH_INTERVAL = 5 * 60 * 1000;

export function agentPlanLimitsOptions() {
  return queryOptions({
    queryKey: agentUsageKeys.planLimits(),
    queryFn: () => api.getAgentPlanLimits(),
    staleTime: STALE_TIME,
    refetchInterval: REFETCH_INTERVAL,
  });
}
