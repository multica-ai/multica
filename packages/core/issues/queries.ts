import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { IssueStatus, ListIssuesParams, ListIssuesCache } from "../types";
import { BOARD_STATUSES } from "./config";

export const issueKeys = {
  all: (wsId: string) => ["issues", wsId] as const,
  list: (wsId: string) => [...issueKeys.all(wsId), "list"] as const,
  /** All "my issues" queries — use for bulk invalidation. */
  myAll: (wsId: string) => [...issueKeys.all(wsId), "my"] as const,
  /** Per-scope "my issues" list with filter identity baked into the key. */
  myList: (
    wsId: string,
    scope: string,
    filter: MyIssuesFilter,
    statuses: readonly string[] = PAGINATED_STATUSES,
  ) => [...issueKeys.myAll(wsId), scope, filter, [...statuses]] as const,
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

export type MyIssuesFilter = Pick<
  ListIssuesParams,
  "assignee_id" | "assignee_ids" | "creator_id" | "project_id" | "pipeline_id"
>;

/** Page size per status column. */
export const ISSUE_PAGE_SIZE = 50;

/** Statuses the issues/my-issues pages paginate. Cancelled is intentionally excluded — it has never been surfaced in the list/board views. */
export const PAGINATED_STATUSES: readonly IssueStatus[] = BOARD_STATUSES;

/** Flatten a bucketed response to a single Issue[] for consumers that want the whole list. */
export function flattenIssueBuckets(data: ListIssuesCache) {
  const out = [];
  for (const bucket of Object.values(data.byStatus)) {
    if (bucket) {
      out.push(...bucket.issues);
    }
  }
  return out;
}

async function fetchFirstPages(
  filter: MyIssuesFilter = {},
  statuses: readonly string[] = PAGINATED_STATUSES,
): Promise<ListIssuesCache> {
  const responses = await Promise.all(
    statuses.map((status) =>
      api.listIssues({ status, limit: ISSUE_PAGE_SIZE, offset: 0, ...filter }),
    ),
  );
  const byStatus: ListIssuesCache["byStatus"] = {};
  statuses.forEach((status, i) => {
    const res = responses[i]!;
    byStatus[status] = { issues: res.issues, total: res.total };
  });
  return { byStatus };
}

/**
 * CACHE SHAPE NOTE: The raw cache stores {@link ListIssuesCache} (buckets keyed
 * by status, each with `{ issues, total }`), and `select` flattens it to
 * `Issue[]` for consumers. Mutations and ws-updaters must use
 * `setQueryData<ListIssuesCache>(...)` and preserve the byStatus shape.
 *
 * Fetches the first page of each paginated status in parallel. Use
 * {@link useLoadMoreByStatus} to paginate a specific status into the cache.
 */
export function issueListOptions(wsId: string) {
  return queryOptions({
    queryKey: issueKeys.list(wsId),
    queryFn: () => fetchFirstPages(),
    select: flattenIssueBuckets,
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
  statuses: readonly string[] = PAGINATED_STATUSES,
  enabled = true,
) {
  return queryOptions({
    queryKey: issueKeys.myList(wsId, scope, filter, statuses),
    queryFn: () => fetchFirstPages(filter, statuses),
    select: flattenIssueBuckets,
    enabled: Boolean(wsId) && statuses.length > 0 && enabled,
  });
}

/** Fetches issues for a specific pipeline, per column status_key. Optionally scoped to a project. */
export function pipelineIssueListOptions(wsId: string, pipelineId: string, columnStatusKeys: string[], projectId?: string) {
  return queryOptions({
    queryKey: [...issueKeys.all(wsId), "pipeline", pipelineId, columnStatusKeys, projectId ?? null] as const,
    queryFn: async () => {
      if (!columnStatusKeys.length) return [];
      const responses = await Promise.all(
        columnStatusKeys.map((status) =>
          api.listIssues({ pipeline_id: pipelineId, status, limit: 100, offset: 0, ...(projectId ? { project_id: projectId } : {}) }),
        ),
      );
      return responses.flatMap((r) => r.issues);
    },
    enabled: Boolean(wsId) && Boolean(pipelineId) && columnStatusKeys.length > 0,
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
