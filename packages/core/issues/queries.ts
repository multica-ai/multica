import {
  infiniteQueryOptions,
  keepPreviousData,
  queryOptions,
  type QueryClient,
} from "@tanstack/react-query";
import { api } from "../api";
import type {
  GroupedIssuesResponse,
  Issue,
  IssueStatus,
  ListGroupedIssuesParams,
  ListIssuesParams,
  ListIssuesCache,
} from "../types";
import { ALL_STATUSES } from "./config";

export interface IssueSortParam {
  sort_by?: ListIssuesParams["sort_by"];
  sort_direction?: ListIssuesParams["sort_direction"];
  date_field?: ListIssuesParams["date_field"];
  date_start?: ListIssuesParams["date_start"];
  date_end?: ListIssuesParams["date_end"];
  /** Server-side custom-property filter (definition id → accepted values).
   *  Lives in the sort/window bag so every list surface, query key, and
   *  load-more page carries it automatically. */
  properties?: ListIssuesParams["properties"];
}

export const issueKeys = {
  all: (wsId: string) => ["issues", wsId] as const,
  /** PREFIX for invalidation — no sort. */
  list: (wsId: string) => [...issueKeys.all(wsId), "list"] as const,
  /** FULL KEY for queryOptions — includes sort. */
  listSorted: (wsId: string, sort?: IssueSortParam) =>
    [...issueKeys.list(wsId), sort ?? {}] as const,
  flatAll: (wsId: string) => [...issueKeys.all(wsId), "flat"] as const,
  flat: (
    wsId: string,
    scope: string,
    filter: IssueFlatFilter,
    sort?: IssueSortParam,
  ) => [...issueKeys.flatAll(wsId), scope, filter, sort ?? {}] as const,
  flatExport: (
    wsId: string,
    scope: string,
    filter: IssueFlatFilter,
    sort?: IssueSortParam,
  ) => [...issueKeys.flatAll(wsId), "export", scope, filter, sort ?? {}] as const,
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
  /** Resolve a bare issue identifier (e.g. "MUL-123") to an issue. */
  identifier: (wsId: string, identifier: string) =>
    [...issueKeys.all(wsId), "identifier", identifier] as const,
  children: (wsId: string, id: string) =>
    [...issueKeys.all(wsId), "children", id] as const,
  /** Prefix for invalidating all batched-children queries in a workspace. */
  childrenByParentsAll: (wsId: string) =>
    [...issueKeys.all(wsId), "children-by-parents"] as const,
  /** Full key — includes sorted parent ids for cache stability. */
  childrenByParents: (wsId: string, parentIds: readonly string[]) =>
    [...issueKeys.childrenByParentsAll(wsId), parentIds] as const,
  childProgress: (wsId: string) =>
    [...issueKeys.all(wsId), "child-progress"] as const,
  /** Prefix-match keys for invalidating the per-issue caches below across
   *  all issues. These keys carry no wsId, so `issueKeys.all(wsId)` does NOT
   *  cover them — WS reconnect recovery must invalidate these `*All`
   *  prefixes explicitly, or missed events leave them stale forever under
   *  the staleTime: Infinity default (#3953). */
  timelineAll: () => ["issues", "timeline"] as const,
  /** Full-issue timeline (single TanStack Query, no cursor). */
  timeline: (issueId: string) =>
    [...issueKeys.timelineAll(), issueId] as const,
  /** Prefix across all issues — WS task lifecycle events invalidate here so
   *  an open composer's trigger preview refreshes when an agent's queue
   *  state changes (the dedup guard makes the answer queue-dependent). */
  commentTriggerPreviewAll: () => ["issues", "comment-trigger-preview"] as const,
  /** PREFIX for invalidation — the composer hook appends parent + content signature. */
  commentTriggerPreview: (issueId: string) =>
    [...issueKeys.commentTriggerPreviewAll(), issueId] as const,
  /** Prefix across all issue-trigger previews (assign/status/create/batch).
   *  WS task lifecycle events invalidate here so the answer revalidates when an
   *  agent's queue state changes (the status source's pending dedup makes it
   *  queue-dependent, mirroring commentTriggerPreviewAll). */
  issueTriggerPreviewAll: () => ["issues", "issue-trigger-preview"] as const,
  /** PREFIX — the picker hook appends a signature of the prospective write. */
  issueTriggerPreview: (signature: string) =>
    [...issueKeys.issueTriggerPreviewAll(), signature] as const,
  reactionsAll: () => ["issues", "reactions"] as const,
  reactions: (issueId: string) =>
    [...issueKeys.reactionsAll(), issueId] as const,
  subscribersAll: () => ["issues", "subscribers"] as const,
  subscribers: (issueId: string) =>
    [...issueKeys.subscribersAll(), issueId] as const,
  usageAll: () => ["issues", "usage"] as const,
  usage: (issueId: string) => [...issueKeys.usageAll(), issueId] as const,
  attachmentsAll: () => ["issues", "attachments"] as const,
  /** Issue-level attachments — used by the description editor so its
   *  inline file-card / image NodeViews can re-sign download URLs at
   *  click time. */
  attachments: (issueId: string) =>
    [...issueKeys.attachmentsAll(), issueId] as const,
  /** Prefix-match key for invalidating tasks across all issues — used by
   *  the global WS task: prefix path so any task lifecycle event refreshes
   *  every per-issue list, regardless of which issue is currently mounted. */
  tasksAll: () => ["issues", "tasks"] as const,
  /** Per-issue task list (issue-detail Execution log section). */
  tasks: (issueId: string) => [...issueKeys.tasksAll(), issueId] as const,
};

export type MyIssuesFilter = Pick<
  ListIssuesParams,
  | "assignee_id"
  | "assignee_ids"
  | "assignee_types"
  | "creator_id"
  | "project_id"
  | "involves_user_id"
>;

/** Server-side contract for the flat table window. These facets must travel
 * with every offset page (and live in the query key); post-filtering a loaded
 * page can silently omit matching issues after the current offset. */
export type IssueFlatFilter = MyIssuesFilter &
  Pick<
    ListIssuesParams,
    | "q"
    | "statuses"
    | "priorities"
    | "assignee_filters"
    | "include_no_assignee"
    | "creator_filters"
    | "project_ids"
    | "include_no_project"
    | "label_ids"
    | "top_level_only"
    | "ids"
  >;

export type AssigneeGroupedIssuesFilter = Omit<
  ListGroupedIssuesParams,
  "group_by" | "limit" | "offset" | "group_assignee_type" | "group_assignee_id"
>;

/** Page size per status column. */
export const ISSUE_PAGE_SIZE = 50;
export const ISSUE_FLAT_PAGE_SIZE = 100;

/**
 * Statuses fetched and paginated into the list/board cache — every lifecycle
 * status, `cancelled` included. `cancelled` is a first-class default status
 * (MUL-4290), so it lives in the cache and renders like any other column;
 * there is no separate "visible board" subset. This constant governs
 * fetch/cache membership.
 */
export const PAGINATED_STATUSES: readonly IssueStatus[] = ALL_STATUSES;

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
const MERGE_PRIORITY_RANK: Record<string, number> = { urgent: 0, high: 1, medium: 2, low: 3, none: 4 };
const MERGE_STATUS_RANK: Record<string, number> = {
  backlog: 0,
  todo: 1,
  in_progress: 2,
  in_review: 3,
  done: 4,
  blocked: 5,
  cancelled: 6,
};

/**
 * Comparator mirroring the server's ORDER BY semantics (including
 * `property:<id>` sorts and missing-values-last). The merged "All" scope
 * concatenates three independently-ordered queries, so without a re-sort the
 * relation order (assigned → created → involves) would override the sort the
 * user picked — e.g. assigned=9 rendering before created=1 (review round 3).
 */
export function compareIssuesForSort(a: Issue, b: Issue, sort?: IssueSortParam): number {
  const by = sort?.sort_by ?? "position";
  const dir = by !== "position" && sort?.sort_direction === "desc" ? -1 : 1;
  // created_at DESC then id DESC, mirroring the server's unique ORDER BY
  // suffix — ids disambiguate bulk-created issues that share a timestamp.
  const tieBreak = () =>
    new Date(b.created_at).getTime() - new Date(a.created_at).getTime() ||
    (a.id < b.id ? 1 : a.id > b.id ? -1 : 0);

  const missingAware = (av: string | null, bv: string | null): number => {
    if (!av && !bv) return tieBreak();
    if (!av) return 1;
    if (!bv) return -1;
    return dir * av.localeCompare(bv) || tieBreak();
  };

  if (by.startsWith("property:")) {
    const propertyId = by.slice("property:".length);
    const av = a.properties?.[propertyId];
    const bv = b.properties?.[propertyId];
    const aMissing = av === undefined || Array.isArray(av) || typeof av === "boolean";
    const bMissing = bv === undefined || Array.isArray(bv) || typeof bv === "boolean";
    if (aMissing && bMissing) return tieBreak();
    if (aMissing) return 1;
    if (bMissing) return -1;
    if (typeof av === "number" && typeof bv === "number") return dir * (av - bv) || tieBreak();
    return dir * String(av).localeCompare(String(bv)) || tieBreak();
  }
  switch (by) {
    case "status":
      return dir * ((MERGE_STATUS_RANK[a.status] ?? 9) - (MERGE_STATUS_RANK[b.status] ?? 9)) || tieBreak();
    case "priority":
      return dir * ((MERGE_PRIORITY_RANK[a.priority] ?? 9) - (MERGE_PRIORITY_RANK[b.priority] ?? 9)) || tieBreak();
    case "title":
      return dir * a.title.localeCompare(b.title) || tieBreak();
    case "created_at":
      return dir * (new Date(a.created_at).getTime() - new Date(b.created_at).getTime()) || tieBreak();
    case "updated_at":
      return dir * (new Date(a.updated_at).getTime() - new Date(b.updated_at).getTime()) || tieBreak();
    case "start_date":
      return missingAware(a.start_date, b.start_date);
    case "due_date":
      return missingAware(a.due_date, b.due_date);
    case "position":
    default:
      return a.position - b.position || tieBreak();
  }
}

async function fetchAllFlatPages(
  filter: IssueFlatFilter,
  sort?: IssueSortParam,
): Promise<Issue[]> {
  const issues: Issue[] = [];
  const seenIds = new Set<string>();
  let offset = 0;
  while (true) {
    const response = await api.listIssues({
      ...filter,
      ...sort,
      limit: ISSUE_FLAT_PAGE_SIZE,
      offset,
    });
    let added = 0;
    for (const issue of response.issues) {
      if (seenIds.has(issue.id)) continue;
      seenIds.add(issue.id);
      issues.push(issue);
      added += 1;
    }
    if (issues.length >= response.total) break;
    if (response.issues.length === 0 || added === 0) {
      throw new Error("Issue export pagination did not advance");
    }
    // Advance by what the server actually returned. This guarantees progress
    // even if an older server clamps the requested page size differently.
    offset += response.issues.length;
  }
  return issues;
}

async function fetchAllMyFlatIssues(
  userId: string,
  filter: IssueFlatFilter,
  sort?: IssueSortParam,
): Promise<Issue[]> {
  const relations = await Promise.all([
    fetchAllFlatPages({ ...filter, assignee_id: userId }, sort),
    fetchAllFlatPages({ ...filter, creator_id: userId }, sort),
    fetchAllFlatPages({ ...filter, involves_user_id: userId }, sort),
  ]);
  const byId = new Map<string, Issue>();
  for (const issues of relations) {
    for (const issue of issues) byId.set(issue.id, issue);
  }
  return [...byId.values()].sort((a, b) => compareIssuesForSort(a, b, sort));
}

export function issueFlatListOptions(
  wsId: string,
  scope: string,
  filter: IssueFlatFilter,
  userId?: string,
  sort?: IssueSortParam,
) {
  const allMyIssues = scope === "all" && !!userId;
  return infiniteQueryOptions({
    queryKey: issueKeys.flat(wsId, scope, filter, sort),
    initialPageParam: 0,
    queryFn: async ({ pageParam }) => {
      if (allMyIssues) {
        const issues = await fetchAllMyFlatIssues(userId, filter, sort);
        return { issues, total: issues.length };
      }
      return api.listIssues({
        ...filter,
        ...sort,
        limit: ISSUE_FLAT_PAGE_SIZE,
        offset: pageParam,
      });
    },
    getNextPageParam: (lastPage, allPages) => {
      if (allMyIssues) return undefined;
      const loaded = allPages.reduce((count, page) => count + page.issues.length, 0);
      return loaded < lastPage.total ? loaded : undefined;
    },
    placeholderData: keepPreviousData,
  });
}

export function issueFlatExportOptions(
  wsId: string,
  scope: string,
  filter: IssueFlatFilter,
  userId?: string,
  sort?: IssueSortParam,
) {
  return queryOptions({
    queryKey: issueKeys.flatExport(wsId, scope, filter, sort),
    queryFn: () =>
      scope === "all" && userId
        ? fetchAllMyFlatIssues(userId, filter, sort)
        : fetchAllFlatPages(filter, sort),
    staleTime: 0,
  });
}

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
    merged.sort((a, b) => compareIssuesForSort(a, b, sort));
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
  const groups = [...merged.values()];
  for (const group of groups) {
    group.issues.sort((a, b) => compareIssuesForSort(a, b, sort));
  }
  return { groups };
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

/**
 * Resolve a bare issue identifier ("MUL-123") to its issue, or `null`.
 *
 * Backs the Linear-style autolink: the backend `q` search matches an
 * identifier on issue NUMBER only (prefix-agnostic — `MUL-123` and `TES-123`
 * both hit number 123), so the exact `identifier === value` filter here is
 * what enforces the workspace prefix. A non-existent or wrong-prefix
 * identifier resolves to `null` and renders as plain text.
 *
 * Server state → TanStack Query; the key includes `wsId` and the identifier,
 * so identical identifiers across the app share one request. Caller gates
 * `enabled` (identifier shape + workspace prefix).
 */
export function issueIdentifierOptions(wsId: string, identifier: string) {
  return queryOptions({
    queryKey: issueKeys.identifier(wsId, identifier),
    queryFn: async ({ signal }) => {
      const res = await api.searchIssues({
        q: identifier,
        limit: 10,
        include_closed: true,
        signal,
      });
      return res.issues.find((i) => i.identifier === identifier) ?? null;
    },
    // Identifier→issue mapping is effectively immutable; avoid refetch churn
    // when the same key renders across many comments/messages.
    staleTime: 5 * 60_000,
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
 * Server cap on parent_ids per `GET /api/issues/children` request — must
 * match `listChildrenByParentsLimit` in server/internal/handler/issue.go.
 * Exceeding it returns 400, so the client chunks larger requests.
 */
export const CHILDREN_BY_PARENTS_CHUNK_SIZE = 200;

/**
 * Batched variant of {@link childIssuesOptions}: fetches children for all
 * given parents in `GET /api/issues/children?parent_ids=…` requests, chunked
 * to {@link CHILDREN_BY_PARENTS_CHUNK_SIZE} parents each. The queryFn also
 * hydrates each parent's per-parent issueKeys.children cache so other
 * surfaces (issue-detail sub-issues panel, set-parent modal) hit the primed
 * cache instead of re-fetching. Hydration happens in queryFn (not a
 * useEffect) to avoid the setQueryData → re-render → effect loop.
 *
 * Used by SwimLaneView to resolve parent lanes without an N-request fan-out.
 * parentIds must be sorted + deduplicated by the caller for a stable cache key.
 */
async function fetchAndHydrateChildrenByParents(
  qc: QueryClient,
  wsId: string,
  parentIds: readonly string[],
) {
  // Chunk to respect the server cap (parallel, since chunks are independent).
  const chunks: string[][] = [];
  for (let i = 0; i < parentIds.length; i += CHILDREN_BY_PARENTS_CHUNK_SIZE) {
    chunks.push([...parentIds.slice(i, i + CHILDREN_BY_PARENTS_CHUNK_SIZE)]);
  }
  const responses = await Promise.all(chunks.map((c) => api.listChildrenByParents(c)));
  const grouped = new Map<string, Issue[]>();
  for (const response of responses) {
    for (const issue of response.issues) {
      if (!issue.parent_issue_id) continue;
      const bucket = grouped.get(issue.parent_issue_id);
      if (bucket) {
        bucket.push(issue);
      } else {
        grouped.set(issue.parent_issue_id, [issue]);
      }
    }
  }
  for (const [parentId, children] of grouped) {
    // Only hydrate if the per-parent cache is empty — don't overwrite a
    // fresher result that another query (e.g. issue-detail) may have written.
    // This relies on useUpdateIssue.onMutate writing into the per-parent
    // cache (not creating an empty one) — if that contract changes, batch
    // hydration here would silently stop seeding new lanes.
    const existing = qc.getQueryData<Issue[]>(issueKeys.children(wsId, parentId));
    if (!existing || existing.length === 0) {
      qc.setQueryData(issueKeys.children(wsId, parentId), children);
    }
  }
  return grouped;
}

export function childrenByParentsOptions(
  wsId: string,
  parentIds: readonly string[],
  qc: QueryClient,
) {
  return queryOptions({
    queryKey: issueKeys.childrenByParents(wsId, parentIds),
    queryFn: () => fetchAndHydrateChildrenByParents(qc, wsId, parentIds),
    enabled: parentIds.length > 0,
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
