import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const issueLifecycleKeys = {
  all: (wsId: string) => ["issue-lifecycles", wsId] as const,
  effective: (wsId: string, projectId: string | null) =>
    [...issueLifecycleKeys.all(wsId), "effective", projectId] as const,
};

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
