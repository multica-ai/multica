import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const issueKeys = {
  all: (wsId: string) => ["issues", wsId] as const,
  list: (wsId: string) => [...issueKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...issueKeys.all(wsId), "detail", id] as const,
  children: (wsId: string, id: string) =>
    [...issueKeys.all(wsId), "children", id] as const,
  timeline: (issueId: string) => ["issues", "timeline", issueId] as const,
  reactions: (issueId: string) => ["issues", "reactions", issueId] as const,
  subscribers: (issueId: string) =>
    ["issues", "subscribers", issueId] as const,
};

export const CLOSED_PAGE_SIZE = 50;

/**
 * CACHE SHAPE NOTE: The raw cache stores ListIssuesResponse ({ issues, total, doneTotal }),
 * but `select` transforms it to Issue[] for consumers. Mutations and ws-updaters
 * must use setQueryData<ListIssuesResponse>(...) — NOT setQueryData<Issue[]>.
 *
 * Fetches all open issues + first page of done issues. Use useLoadMoreDoneIssues()
 * to paginate additional done items into the cache.
 */
export function issueListOptions(wsId: string) {
  return queryOptions({
    queryKey: issueKeys.list(wsId),
    queryFn: async () => {
      const [openRes, closedRes] = await Promise.all([
        api.listIssues({ open_only: true }),
        api.listIssues({ status: "done", limit: CLOSED_PAGE_SIZE, offset: 0 }),
      ]);
      return {
        issues: [...openRes.issues, ...closedRes.issues],
        total: openRes.total + closedRes.total,
        doneTotal: closedRes.total,
      };
    },
    select: (data) => data.issues,
  });
}

export function issueDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: issueKeys.detail(wsId, id),
    queryFn: () => api.getIssue(id),
  });
}

export function childIssuesOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: issueKeys.children(wsId, id),
    queryFn: () => api.listChildIssues(id).then((r) => r.issues),
  });
}

export function issueTimelineOptions(issueId: string) {
  return queryOptions({
    queryKey: issueKeys.timeline(issueId),
    queryFn: () => api.listTimeline(issueId),
  });
}

export function issueReactionsOptions(issueId: string) {
  return queryOptions({
    queryKey: issueKeys.reactions(issueId),
    queryFn: async () => {
      const issue = await api.getIssue(issueId);
      return issue.reactions ?? [];
    },
  });
}

export function issueSubscribersOptions(issueId: string) {
  return queryOptions({
    queryKey: issueKeys.subscribers(issueId),
    queryFn: () => api.listIssueSubscribers(issueId),
  });
}
