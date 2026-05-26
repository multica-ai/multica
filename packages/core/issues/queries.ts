import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type {
  GroupedIssuesResponse,
  Issue,
  IssueStatus,
  ListGroupedIssuesParams,
  ListIssuesParams,
  ListIssuesCache,
} from "../types";
import { BOARD_STATUSES } from "./config";

export interface IssueSortParam {
  sort_by?: ListIssuesParams["sort_by"];
  sort_direction?: ListIssuesParams["sort_direction"];
}

export const issueKeys = {
  all: (wsId: string) => ["issues", wsId] as const,
  /** PREFIX for invalidation — no sort. */
  list: (wsId: string) => [...issueKeys.all(wsId), "list"] as const,
  /** FULL KEY for queryOptions — includes sort. */
  listSorted: (wsId: string, sort?: IssueSortParam) =>
    [...issueKeys.list(wsId), sort ?? {}] as const,
  assigneeGroupsAll: (wsId: string) =>
    [...issueKeys.all(wsId), "assignee-groups"] as const,
  assigneeGroups: (wsId: string, filter: AssigneeGroupedIssuesFilter) =>
    [...issueKeys.assigneeGroupsAll(wsId), filter] as const,
  /** All "my issues" queries — use for bulk invalidation. */
  myAll: (wsId: string) => [...issueKeys.all(wsId), "my"] as const,
  /** PREFIX for per-scope invalidation — no sort. */
  myList: (wsId: string, scope: string, filter: MyIssuesFilter) =>
    [...issueKeys.myAll(wsId), scope, filter] as const,
  /** FULL KEY for queryOptions — includes sort. */
  myListSorted: (wsId: string, scope: string, filter: MyIssuesFilter, sort?: IssueSortParam) =>
    [...issueKeys.myList(wsId, scope, filter), sort ?? {}] as const,
  myAssigneeGroupsAll: (wsId: string) =>
    [...issueKeys.myAll(wsId), "assignee-groups"] as const,
  myAssigneeGroups: (
    wsId: string,
    scope: string,
    filter: AssigneeGroupedIssuesFilter,
  ) => [...issueKeys.myAssigneeGroupsAll(wsId), scope, filter] as const,
  /** All Project Gantt queries — prefix-match key for cross-project invalidation. */
  projectGanttAll: (wsId: string) =>
    [...issueKeys.all(wsId), "project-gantt"] as const,
  /**
   * Per-project Gantt issue list (scheduled-only). Uses its own cache key
   * rather than reusing the bucketed `myList` cache so WS handlers and
   * cache helpers don't have to special-case a non-bucketed shape under
   * the `my` prefix.
   */
  projectGantt: (wsId: string, projectId: string) =>
    [...issueKeys.projectGanttAll(wsId), projectId] as const,
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
  "assignee_id" | "assignee_ids" | "creator_id" | "project_id" | "involves_user_id"
>;

export type AssigneeGroupedIssuesFilter = Omit<
  ListGroupedIssuesParams,
  "group_by" | "limit" | "offset" | "group_assignee_type" | "group_assignee_id"
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

async function fetchFirstPages(filter: MyIssuesFilter = {}, sort?: IssueSortParam): Promise<ListIssuesCache> {
  const responses = await Promise.all(
    PAGINATED_STATUSES.map((status) =>
      api.listIssues({ status, limit: ISSUE_PAGE_SIZE, offset: 0, ...sort, ...filter }),
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
 * "All my issues" — union of three server filters:
 *   assignee_id=me OR creator_id=me OR involves_user_id=me
 *
 * The backend has no OR-across-user-filters today, so we run the three
 * existing single-filter fetches in parallel and dedupe on the client by
 * issue id within each status bucket. Order within each bucket preserves
 * the first-seen position (each sub-fetch is already server-sorted).
 *
 * Personal lists are bounded (tens to a few hundred issues across all
 * three relations), so 3× the request count is acceptable — a single
 * fetchFirstPages already runs 7 status fetches in parallel, so the total
 * here is 21 small parallel requests. Easy enough; no need to add a new
 * backend query just for this scope.
 *
 * `total` per bucket is set to the merged length, not the true server
 * total — pagination on the "All" scope is out of scope; the first
 * 50-per-status × 3 widening (deduped) is what the page renders.
 */
async function fetchAllMyFirstPages(userId: string, sort?: IssueSortParam): Promise<ListIssuesCache> {
  const [byAssignee, byCreator, byInvolves] = await Promise.all([
    fetchFirstPages({ assignee_id: userId }, sort),
    fetchFirstPages({ creator_id: userId }, sort),
    fetchFirstPages({ involves_user_id: userId }, sort),
  ]);
  const byStatus: ListIssuesCache["byStatus"] = {};
  for (const status of PAGINATED_STATUSES) {
    const seen = new Set<string>();
    const merged: Issue[] = [];
    for (const cache of [byAssignee, byCreator, byInvolves]) {
      const bucket = cache.byStatus[status];
      if (!bucket) continue;
      for (const issue of bucket.issues) {
        if (seen.has(issue.id)) continue;
        seen.add(issue.id);
        merged.push(issue);
      }
    }
    byStatus[status] = { issues: merged, total: merged.length };
  }
  return { byStatus };
}

/**
 * Sibling of {@link fetchAllMyFirstPages} for the assignee-grouped board
 * view. Runs the three single-filter grouped queries in parallel and
 * merges groups by (assignee_type, assignee_id), deduping issues within
 * each group. Extra filters from the page (statuses, priorities, etc.)
 * pass through unchanged.
 */
async function fetchAllMyAssigneeGroups(
  userId: string,
  filter: AssigneeGroupedIssuesFilter,
  sort?: IssueSortParam,
): Promise<GroupedIssuesResponse> {
  const variants: AssigneeGroupedIssuesFilter[] = [
    { ...filter, assignee_id: userId },
    { ...filter, creator_id: userId },
    { ...filter, involves_user_id: userId },
  ];
  const responses = await Promise.all(
    variants.map((f) =>
      api.listGroupedIssues({
        group_by: "assignee",
        limit: ISSUE_PAGE_SIZE,
        offset: 0,
        ...sort,
        ...f,
      }),
    ),
  );
  const groupKey = (g: GroupedIssuesResponse["groups"][number]) =>
    `${g.assignee_type ?? "_"}::${g.assignee_id ?? "_"}`;
  const merged = new Map<string, GroupedIssuesResponse["groups"][number]>();
  for (const res of responses) {
    for (const group of res.groups) {
      const key = groupKey(group);
      const existing = merged.get(key);
      if (!existing) {
        merged.set(key, {
          ...group,
          issues: [...group.issues],
          total: group.issues.length,
        });
        continue;
      }
      const seen = new Set(existing.issues.map((i) => i.id));
      for (const issue of group.issues) {
        if (seen.has(issue.id)) continue;
        seen.add(issue.id);
        existing.issues.push(issue);
      }
      existing.total = existing.issues.length;
    }
  }
  return { groups: [...merged.values()] };
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
export function issueListOptions(wsId: string, sort?: IssueSortParam) {
  return queryOptions({
    queryKey: issueKeys.listSorted(wsId, sort),
    queryFn: () => fetchFirstPages({}, sort),
    select: flattenIssueBuckets,
    placeholderData: keepPreviousData,
  });
}

export function issueAssigneeGroupsOptions(
  wsId: string,
  filter: AssigneeGroupedIssuesFilter,
  sort?: IssueSortParam,
) {
  return queryOptions<GroupedIssuesResponse>({
    queryKey: issueKeys.assigneeGroups(wsId, { ...filter, ...sort }),
    queryFn: () =>
      api.listGroupedIssues({
        group_by: "assignee",
        limit: ISSUE_PAGE_SIZE,
        offset: 0,
        ...sort,
        ...filter,
      }),
    placeholderData: keepPreviousData,
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
  // Required when scope === "all" — the user id whose three relations
  // (assignee, creator, agents+squads) we union over. For every other
  // scope the filter object already carries the relevant id and userId
  // is ignored.
  userId?: string,
  sort?: IssueSortParam,
) {
  return queryOptions({
    queryKey: issueKeys.myListSorted(wsId, scope, filter, sort),
    queryFn: () =>
      scope === "all" && userId
        ? fetchAllMyFirstPages(userId, sort)
        : fetchFirstPages(filter, sort),
    select: flattenIssueBuckets,
    placeholderData: keepPreviousData,
  });
}

/**
 * Page size for the scheduled-issue fetch. The Gantt view always pulls every
 * scheduled issue (no client pagination), so this is just the chunk size we
 * use to walk the server's `(limit, offset)` window until we hit `total`.
 */
export const PROJECT_GANTT_PAGE_LIMIT = 500;

/**
 * Paranoia cap on the loop in {@link fetchProjectGanttIssues}. Real projects
 * shouldn't come close to this — a single project carrying 50k scheduled
 * issues is already a product problem, not a Gantt-rendering one — but the
 * guard prevents a buggy server `total` from spinning the loop forever.
 */
export const PROJECT_GANTT_MAX_ISSUES = 10_000;

async function fetchProjectGanttIssues(projectId: string) {
  const issues = [];
  let offset = 0;
  while (offset < PROJECT_GANTT_MAX_ISSUES) {
    const res = await api.listIssues({
      project_id: projectId,
      scheduled: true,
      limit: PROJECT_GANTT_PAGE_LIMIT,
      offset,
    });
    issues.push(...res.issues);
    if (res.issues.length < PROJECT_GANTT_PAGE_LIMIT) break;
    if (issues.length >= res.total) break;
    offset += PROJECT_GANTT_PAGE_LIMIT;
  }
  return issues;
}

/**
 * One-shot fetch of every scheduled issue (`start_date` or `due_date` set)
 * for a project. The Project Gantt view consumes this directly — no status
 * bucketing, no client-side pagination, no Load-all affordance — because
 * the scheduled subset is bounded enough to come back in a small handful of
 * requests.
 *
 * Backed by `GET /api/issues?scheduled=true&project_id=…`; the SQL filter
 * mirrors the same `(start_date IS NOT NULL OR due_date IS NOT NULL)`
 * predicate the Gantt view applies on the client. Pages are walked until
 * `total` is reached so an oversized project can't silently lose bars past
 * the first page.
 */
export function projectGanttIssuesOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: issueKeys.projectGantt(wsId, projectId),
    queryFn: () => fetchProjectGanttIssues(projectId),
  });
}

export function myIssueAssigneeGroupsOptions(
  wsId: string,
  scope: string,
  filter: AssigneeGroupedIssuesFilter,
  // See myIssueListOptions for the userId contract — only consulted when
  // scope === "all", and powers the 3-fetch grouped union.
  userId?: string,
  sort?: IssueSortParam,
) {
  return queryOptions<GroupedIssuesResponse>({
    queryKey: issueKeys.myAssigneeGroups(wsId, scope, { ...filter, ...sort }),
    queryFn: () =>
      scope === "all" && userId
        ? fetchAllMyAssigneeGroups(userId, filter, sort)
        : api.listGroupedIssues({
            group_by: "assignee",
            limit: ISSUE_PAGE_SIZE,
            offset: 0,
            ...sort,
            ...filter,
          }),
    placeholderData: keepPreviousData,
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
