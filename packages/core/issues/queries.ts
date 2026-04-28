import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { IssueStatus, ListIssuesParams, ListIssuesCache } from "../types";
import { BOARD_STATUSES } from "./config";

export const issueKeys = {
  all: (wsId: string) => ["issues", wsId] as const,
  /** Filter-keyed list. Empty filter is the canonical workspace-wide list. */
  list: (wsId: string, filter: IssueListFilter = EMPTY_FILTER) =>
    [...issueKeys.all(wsId), "list", normalizeFilter(filter)] as const,
  /** All "my issues" queries — use for bulk invalidation. */
  myAll: (wsId: string) => [...issueKeys.all(wsId), "my"] as const,
  /** Per-scope "my issues" list with filter identity baked into the key. */
  myList: (wsId: string, scope: string, filter: IssueListFilter) =>
    [...issueKeys.myAll(wsId), scope, normalizeFilter(filter)] as const,
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
  /** Prefix used by mutations to broadcast cache updates across all filters. */
  listPrefix: (wsId: string) => [...issueKeys.all(wsId), "list"] as const,
};

/**
 * Server-side filter passed to `GET /api/issues`. Status is excluded because
 * the cache buckets per-status — each bucket is fetched with its own status
 * query param (see {@link fetchFirstPages}).
 */
export type IssueListFilter = Omit<
  ListIssuesParams,
  "limit" | "offset" | "workspace_id" | "status" | "open_only"
>;

/** Backwards-compat alias — old name was specific to the My Issues page. */
export type MyIssuesFilter = IssueListFilter;

const EMPTY_FILTER: IssueListFilter = {};

/**
 * Normalizes a filter object so semantically-identical filters produce
 * identical query keys. Sorts arrays (so `[a, b]` and `[b, a]` cache to the
 * same key) and drops empty/falsy entries (so `{}` and `{ priorities: [] }`
 * are equivalent).
 */
function normalizeFilter(filter: IssueListFilter): IssueListFilter {
  const out: IssueListFilter = {};
  if (filter.priorities?.length) out.priorities = [...filter.priorities].sort();
  if (filter.assignee_ids?.length) out.assignee_ids = [...filter.assignee_ids].sort();
  if (filter.include_no_assignee) out.include_no_assignee = true;
  if (filter.creator_ids?.length) out.creator_ids = [...filter.creator_ids].sort();
  if (filter.project_ids?.length) out.project_ids = [...filter.project_ids].sort();
  if (filter.include_no_project) out.include_no_project = true;
  if (filter.label_ids?.length) out.label_ids = [...filter.label_ids].sort();
  return out;
}

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

async function fetchFirstPages(filter: IssueListFilter = {}): Promise<ListIssuesCache> {
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
 * Fetches the first page of each paginated status in parallel. Filter goes
 * into both the cache key and the request, so filter changes trigger a
 * fresh server-side fetch and don't share cache with other filters. Use
 * {@link useLoadMoreByStatus} to paginate a specific status.
 */
export function issueListOptions(wsId: string, filter: IssueListFilter = EMPTY_FILTER) {
  return queryOptions({
    queryKey: issueKeys.list(wsId, filter),
    queryFn: () => fetchFirstPages(filter),
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
  filter: IssueListFilter,
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
