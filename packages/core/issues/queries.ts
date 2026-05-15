import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type {
  IssueStatus,
  ListIssuesParams,
  ListIssuesCache,
} from "../types";
import { BOARD_STATUSES } from "./config";

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
  /** Full-issue timeline (single TanStack Query, no cursor). */
  timeline: (issueId: string) =>
    ["issues", "timeline", issueId] as const,
  reactions: (issueId: string) => ["issues", "reactions", issueId] as const,
  subscribers: (issueId: string) =>
    ["issues", "subscribers", issueId] as const,
  usage: (issueId: string) => ["issues", "usage", issueId] as const,
  /** Issue-level attachments — used by the description editor so its
   *  inline file-card / image NodeViews can re-sign download URLs at
   *  click time. */
  attachments: (issueId: string) => ["issues", "attachments", issueId] as const,
  /** Per-issue task list (issue-detail Execution log section). */
  tasks: (issueId: string) => ["issues", "tasks", issueId] as const,
  /** Prefix-match key for invalidating tasks across all issues — used by
   *  the global WS task: prefix path so any task lifecycle event refreshes
   *  every per-issue list, regardless of which issue is currently mounted. */
  tasksAll: () => ["issues", "tasks"] as const,
};

export type MyIssuesFilter = Pick<
  ListIssuesParams,
  "assignee_id" | "assignee_ids" | "creator_id" | "project_id"
>;

/** Page size per status column. */
export const ISSUE_PAGE_SIZE = 50;

/** Statuses the issues/my-issues pages paginate. Cancelled is intentionally excluded — it has never been surfaced in the list/board views. */
export const PAGINATED_STATUSES: readonly IssueStatus[] = BOARD_STATUSES;

/** Flatten a bucketed response to a single Issue[] for consumers that want the whole list. */
export function flattenIssueBuckets(data: ListIssuesCache) {
  const out = [];
  for (const status of PAGINATED_STATUSES) {
    const bucket = data.byStatus[status];
    if (bucket) out.push(...bucket.issues);
  }
  return out;
}

async function fetchFirstPages(filter: MyIssuesFilter = {}): Promise<ListIssuesCache> {
  const responses = await Promise.all(
    PAGINATED_STATUSES.map((status) =>
      api.listIssues({ status, limit: ISSUE_PAGE_SIZE, offset: 0, ...filter }),
    ),
  );
  const byStatus: ListIssuesCache["byStatus"] = {};
  PAGINATED_STATUSES.forEach((status, i) => {
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
) {
  return queryOptions({
    queryKey: issueKeys.myList(wsId, scope, filter),
    queryFn: () => fetchFirstPages(filter),
    select: flattenIssueBuckets,
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

/**
 * Single-fetch timeline options. The endpoint returns the full ordered set of
 * comments + activities for an issue (server caps at 2000 as a safety net).
 * Cursor pagination was removed in #1929 — at observed data sizes (p99 ~30
 * entries per issue) it added complexity without a UX win and broke reply
 * threads at page boundaries.
 */
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

// Backs the description editor's fresh-sign download flow: NodeViews resolve
// an attachment id by matching the markdown URL against this list. The list
// is workspace-private metadata and lives on the same cache lifetime as the
// rest of the issue detail surface.
export function issueAttachmentsOptions(issueId: string) {
  return queryOptions({
    queryKey: issueKeys.attachments(issueId),
    queryFn: () => api.listAttachments(issueId),
  });
}
