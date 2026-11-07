import { queryOptions, type Query, type QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "./queries";

/**
 * The ids of the issues the viewer subscribes to, for evaluating the
 * "subscribed" relation on rows the client filters itself. Kept outside
 * `issueKeys` so the broad issue invalidations do not refetch it; it changes
 * only with the viewer's own subscriptions.
 */
export const subscribedIssueKeys = {
  all: (wsId: string) => ["issue-subscribed", wsId] as const,
};

export function subscribedIssueIdsOptions(wsId: string) {
  return queryOptions({
    queryKey: subscribedIssueKeys.all(wsId),
    queryFn: () => api.listSubscribedIssueIds(),
  });
}

function filtersBySubscribed(query: Query): boolean {
  return JSON.stringify(query.queryKey).includes('"subscribed"');
}

/**
 * Re-reads what depends on the viewer's subscriptions after one changed: the
 * id set, and every issue query (rows, groups, facets, sidebar lists) whose
 * filter names the relation. Cancels the
 * set first so a change during its first load is not answered by the request
 * already on the wire (see `refreshIssueUnread`).
 */
export async function refreshSubscribedIssues(qc: QueryClient, wsId: string) {
  qc.invalidateQueries({ queryKey: issueKeys.all(wsId), predicate: filtersBySubscribed });
  const queryKey = subscribedIssueKeys.all(wsId);
  await qc.cancelQueries({ queryKey });
  await qc.invalidateQueries({ queryKey });
}
