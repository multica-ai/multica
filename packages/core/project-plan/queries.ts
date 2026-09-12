import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const projectPlanKeys = {
  all: (wsId: string) => ["project-plan", wsId] as const,
  active: (wsId: string, projectId: string) =>
    [...projectPlanKeys.all(wsId), "active", projectId] as const,
  version: (wsId: string, projectId: string, planId: string) =>
    [...projectPlanKeys.all(wsId), "version", projectId, planId] as const,
};

/**
 * The active plan for a project, or `null` for the genuine "no active plan"
 * 404 — distinct from a query error, which React Query surfaces separately.
 * See usePlanOverview for the state this feeds.
 */
export function projectPlanActiveOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: projectPlanKeys.active(wsId, projectId),
    queryFn: () => api.getActiveProjectPlan(projectId),
  });
}

/** A specific retained plan version, with live issue status. */
export function projectPlanVersionOptions(wsId: string, projectId: string, planId: string) {
  return queryOptions({
    queryKey: projectPlanKeys.version(wsId, projectId, planId),
    queryFn: () => api.getProjectPlan(projectId, planId),
    enabled: !!planId,
  });
}
