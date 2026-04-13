import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { ListIssuesParams } from "../types";

export const issueKeys = {
  all: (wsId: string) => ["issues", wsId] as const,
  list: (wsId: string) => [...issueKeys.all(wsId), "list"] as const,
  /** All "my issues" queries — use for bulk invalidation. */
  myAll: (wsId: string) => [...issueKeys.all(wsId), "my"] as const,
  /** Per-scope "my issues" list with filter identity baked into the key. */
  myList: (wsId: string, scope: string, filter: MyIssuesFilter) =>
    [...issueKeys.myAll(wsId), scope, filter] as const,
  detail: (wsId: string, id: string) =>
    [...issueKeys.all(wsId), "detail", id] as const,
  children: (wsId: string, id: string) =>
    [...issueKeys.all(wsId), "children", id] as const,
  childProgress: (wsId: string) =>
    [...issueKeys.all(wsId), "child-progress"] as const,
  timeline: (issueId: string) => ["issues", "timeline", issueId] as const,
  reactions: (issueId: string) => ["issues", "reactions", issueId] as const,
  subscribers: (issueId: string) =>
    ["issues", "subscribers", issueId] as const,
  usage: (issueId: string) => ["issues", "usage", issueId] as const,
};

export type MyIssuesFilter = Pick<ListIssuesParams, "assignee_id" | "assignee_ids" | "creator_id">;

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

/**
 * Server-filtered issue list for the My Issues page.
 * Each scope gets its own cache entry so switching tabs is instant after first load.
 */
export function myIssueListOptions(
  wsId: string,
  scope: string,
  filter: MyIssuesFilter,
) {
  return queryOptions({
    queryKey: issueKeys.myList(wsId, scope, filter),
    queryFn: async () => {
      const [openRes, closedRes] = await Promise.all([
        api.listIssues({ open_only: true, ...filter }),
        api.listIssues({
          status: "done",
          limit: CLOSED_PAGE_SIZE,
          offset: 0,
          ...filter,
        }),
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

export function childIssueProgressOptions(wsId: string) {
  return queryOptions({
    queryKey: issueKeys.childProgress(wsId),
    queryFn: () => api.getChildIssueProgress(),
    select: (data) => {
      const map = new Map<string, { done: number; total: number }>();
      for (const entry of data.progress) {
        map.set(entry.parent_issue_id, { done: entry.done, total: entry.total });
      }
      return map;
    },
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

export function issueUsageOptions(issueId: string) {
  return queryOptions({
    queryKey: issueKeys.usage(issueId),
    queryFn: () => api.getIssueUsage(issueId),
  });
}
