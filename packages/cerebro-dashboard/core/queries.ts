import { queryOptions } from "@tanstack/react-query";
import { fetchDashboardOverview, type TimeRange } from "./api";

export const dashboardKeys = {
  all: (wsId: string) => ["cerebro", "dashboard", wsId] as const,
  overview: (wsId: string, range: TimeRange) =>
    [...dashboardKeys.all(wsId), "overview", range] as const,
};

export function dashboardOverviewOptions(wsId: string, range: TimeRange) {
  return queryOptions({
    queryKey: dashboardKeys.overview(wsId, range),
    queryFn: () => fetchDashboardOverview(range),
    enabled: !!wsId,
    staleTime: 30 * 1000,
    refetchOnWindowFocus: true,
  });
}
