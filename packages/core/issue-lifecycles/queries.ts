import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const issueLifecycleKeys = {
  all: (wsId: string) => ["issue-lifecycles", wsId] as const,
  detail: (wsId: string, lifecycleId: string) =>
    [...issueLifecycleKeys.all(wsId), "detail", lifecycleId] as const,
  executions: (wsId: string, issueId: string) =>
    [...issueLifecycleKeys.all(wsId), "executions", issueId] as const,
  effective: (wsId: string, projectId: string | null) =>
    [...issueLifecycleKeys.all(wsId), "effective", projectId] as const,
};

export function issueLifecycleOptions(wsId: string, lifecycleId: string) {
  return queryOptions({
    queryKey: issueLifecycleKeys.detail(wsId, lifecycleId),
    queryFn: () => api.getIssueLifecycle(lifecycleId),
    enabled: lifecycleId.length > 0,
    staleTime: 5 * 60_000,
  });
}

export function issueAutomationExecutionsOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: issueLifecycleKeys.executions(wsId, issueId),
    queryFn: () => api.listIssueAutomationExecutions(issueId),
  });
}

/**
 * The lifecycle a newly-created issue will bind to. Existing issues use their
 * own pinned lifecycle_id and must not be resolved through this query.
 */
export function effectiveIssueLifecycleOptions(
  wsId: string,
  projectId: string | null,
  includeArchived = false,
) {
  return queryOptions({
    queryKey: [...issueLifecycleKeys.effective(wsId, projectId), { includeArchived }] as const,
    queryFn: () => api.getEffectiveIssueLifecycle(projectId, includeArchived),
    staleTime: 5 * 60_000,
  });
}
