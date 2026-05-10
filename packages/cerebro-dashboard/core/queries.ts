import { queryOptions } from "@tanstack/react-query";
import { fetchDashboardOverview, type TimeRange } from "./api";
import type { ActorScope } from "./types";

export const dashboardKeys = {
  all: (wsId: string) => ["cerebro", "dashboard", wsId] as const,
  overview: (wsId: string, range: TimeRange, scope: ActorScope, actorId: string | null) =>
    [...dashboardKeys.all(wsId), "overview", range, scope, actorId] as const,
};

export function dashboardOverviewOptions(
  wsId: string,
  range: TimeRange,
  scope: ActorScope,
  actorId: string | null,
) {
  return queryOptions({
    queryKey: dashboardKeys.overview(wsId, range, scope, actorId),
    queryFn: () => fetchDashboardOverview(range, scope, actorId),
    enabled: !!wsId,
    staleTime: 30 * 1000,
    refetchOnWindowFocus: true,
  });
}
