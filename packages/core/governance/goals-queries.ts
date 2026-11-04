import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { GoalFilters, MetricReadingFilters } from "./goals-types";

export const goalKeys = {
  all: (wsId: string) => ["goals", wsId] as const,
  list: (wsId: string, filters: GoalFilters = {}) =>
    [...goalKeys.all(wsId), "list", filters.scope_type ?? "", filters.scope_id ?? "", filters.status ?? ""] as const,
  detail: (wsId: string, id: string) => [...goalKeys.all(wsId), "detail", id] as const,
  metrics: (wsId: string) => [...goalKeys.all(wsId), "metrics"] as const,
  readings: (wsId: string, key: string, filters: MetricReadingFilters = {}) =>
    [...goalKeys.all(wsId), "readings", key, filters.scope_type ?? "", filters.scope_id ?? ""] as const,
};

/** The workspace's goals, with progress and confidence. */
export function goalsOptions(wsId: string, filters: GoalFilters = {}) {
  return queryOptions({
    queryKey: goalKeys.list(wsId, filters),
    queryFn: () => api.listGoals(filters),
    enabled: !!wsId,
  });
}

/** One goal with its key results, series and child goals. */
export function goalDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: goalKeys.detail(wsId, id),
    queryFn: () => api.getGoal(id),
    enabled: !!wsId && !!id,
  });
}

/** The metrics a key result or a reading can name, built-in ones included. */
export function metricsOptions(wsId: string) {
  return queryOptions({
    queryKey: goalKeys.metrics(wsId),
    queryFn: () => api.listMetrics(),
    enabled: !!wsId,
  });
}

/** The readings of one metric at one scope, newest first. */
export function metricReadingsOptions(wsId: string, key: string, filters: MetricReadingFilters = {}) {
  return queryOptions({
    queryKey: goalKeys.readings(wsId, key, filters),
    queryFn: () => api.listMetricReadings(key, filters),
    enabled: !!wsId && !!key,
  });
}
