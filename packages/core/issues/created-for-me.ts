import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/**
 * The ids of the issues an agent quick-created for the viewer, the agent-filed
 * half of the "created" relation, for evaluating it on rows the client filters
 * itself (#4673). Kept outside `issueKeys` so the broad issue invalidations do
 * not refetch it; it grows only when an agent files an issue for the viewer,
 * which also subscribes them with reason "creator".
 */
export const createdForMeIssueKeys = {
  all: (wsId: string) => ["issue-created-for-me", wsId] as const,
};

export function createdForMeIssueIdsOptions(wsId: string) {
  return queryOptions({
    queryKey: createdForMeIssueKeys.all(wsId),
    queryFn: () => api.listIssueIdsCreatedForMe(),
    enabled: !!wsId,
  });
}
