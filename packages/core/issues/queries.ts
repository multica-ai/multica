import { keepPreviousData, queryOptions, type QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type {
  GroupedIssuesResponse,
  Issue,
  IssueStatus,
  ListGroupedIssuesParams,
  ListIssuesParams,
  ListIssuesCache,
  ViewFilters,
  IssueActorRef,
} from "../types";
import { resolveViewRequests } from "../views/resolver";
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
  /** All view-driven list queries — prefix for invalidation. */
  viewListAll: (wsId: string) => [...issueKeys.all(wsId), "view-list"] as const,
  /**
   * View-driven issue list, keyed on (wsId, viewId, filters, sort). viewId is
   * part of the key so two views with structurally identical filters still get
   * distinct cache entries (and `null` ad-hoc filters never collide with a
   * saved view). filters are embedded too so an edited-but-unsaved view still
   * re-fetches.
   */
  viewListSorted: (
    wsId: string,
    viewId: string | null,
    filters: ViewFilters,
    sort?: IssueSortParam,
  ) => [...issueKeys.viewListAll(wsId), viewId, filters, sort ?? {}] as const,
  assigneeGroupsAll: (wsId: string) =>
    [...issueKeys.all(wsId), "assignee-groups"] as const,
  assigneeGroups: (wsId: string, filter: AssigneeGroupedIssuesFilter) =>
    [...issueKeys.assigneeGroupsAll(wsId), filter] as const,
  /** All view-driven board queries — prefix for invalidation. */
  viewAssigneeGroupsAll: (wsId: string) =>
    [...issueKeys.all(wsId), "view-assignee-groups"] as const,
  viewAssigneeGroups: (
    wsId: string,
    viewId: string | null,
    filter: ViewFilters & IssueSortParam,
  ) => [...issueKeys.viewAssigneeGroupsAll(wsId), viewId, filter] as const,
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
  /** Prefix for invalidating all batched-children queries in a workspace. */
  childrenByParentsAll: (wsId: string) =>
    [...issueKeys.all(wsId), "children-by-parents"] as const,
  /** Full key — includes sorted parent ids for cache stability. */
  childrenByParents: (wsId: string, parentIds: readonly string[]) =>
    [...issueKeys.childrenByParentsAll(wsId), parentIds] as const,
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

async function fetchFirstPages(
  filter: Partial<ListIssuesParams> = {},
  sort?: IssueSortParam,
): Promise<ListIssuesCache> {
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
 * Union several {@link ListIssuesCache}es into one, deduping issues by id
 * within each status bucket and preserving first-seen order. `total` per
 * bucket becomes the merged length, not the server total — multi-source
 * unions don't have a meaningful server total to page against, so pagination
 * is disabled for them (the per-source first pages are what renders). A
 * single-element input passes through bucket-for-bucket with its server
 * totals intact, so single-source callers keep working pagination.
 */
export function mergeIssueCaches(caches: ListIssuesCache[]): ListIssuesCache {
  if (caches.length === 1) return caches[0]!;
  const byStatus: ListIssuesCache["byStatus"] = {};
  for (const status of PAGINATED_STATUSES) {
    const seen = new Set<string>();
    const merged: Issue[] = [];
    for (const cache of caches) {
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
 * View-driven list fetch. Resolves the view's filters into one request per
 * any_of branch (see resolveViewRequests), fetches the first page of each
 * paginated status for each branch in parallel, then unions the branches by
 * status (dedupe by id). A flat (single-branch) view goes straight through
 * fetchFirstPages with its server totals intact, so per-status pagination via
 * useLoadMoreByStatus keeps working. A multi-branch (any_of) view has no
 * single server total per status, so pagination is disabled for it — the
 * first page per status per branch is what renders.
 */
async function fetchViewFirstPages(
  filters: ViewFilters,
  sort?: IssueSortParam,
): Promise<ListIssuesCache> {
  const requests = resolveViewRequests(filters);
  const caches = await Promise.all(requests.map((req) => fetchFirstPages(req, sort)));
  return mergeIssueCaches(caches);
}

/**
 * Union several {@link GroupedIssuesResponse}es into one, merging groups by
 * (assignee_type, assignee_id) and deduping issues within each group. `total`
 * per group becomes the merged length. A single input passes through
 * unchanged (server totals intact) so single-source pagination keeps working.
 */
export function mergeAssigneeGroups(
  responses: GroupedIssuesResponse[],
): GroupedIssuesResponse {
  if (responses.length === 1) return responses[0]!;
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

async function fetchViewAssigneeGroups(
  filters: ViewFilters,
  sort?: IssueSortParam,
): Promise<GroupedIssuesResponse> {
  const requests = resolveViewRequests(filters);
  const responses = await Promise.all(
    requests.map((req) =>
      api.listGroupedIssues({
        group_by: "assignee",
        limit: ISSUE_PAGE_SIZE,
        offset: 0,
        ...sort,
        ...toGroupedFilter(req),
      }),
    ),
  );
  return mergeAssigneeGroups(responses);
}

/**
 * Project a resolved {@link ListIssuesParams} (string CSV `assignee_filters` /
 * `creator_filters` of the form `type:id`) onto the {@link ListGroupedIssuesParams}
 * shape, which carries those two filters as {@link IssueActorRef}[]. The
 * server-expanded `{me}` / `{my_agents}` / `{my_squads}` tokens have no `:`
 * and pass through as `{ type: token, id: "" }` — the backend re-expands them.
 */
export function viewFiltersToGroupedFilter(filters: ViewFilters): AssigneeGroupedIssuesFilter {
  const { any_of: _any_of, ...flat } = filters;
  return toGroupedFilter(flat) as AssigneeGroupedIssuesFilter;
}

/**
 * Constrain a view's filters to the assignee-grouped board's status scope.
 * The per-status list path enumerates {@link PAGINATED_STATUSES} itself, but
 * the board issues a single grouped fetch — so when a view sets no explicit
 * status filter it must default to {@link BOARD_STATUSES}, otherwise the board
 * pulls every status (including `cancelled`, which it never renders) and the
 * two surfaces disagree. An explicit status filter is left untouched.
 */
export function withBoardStatusScope(filters: ViewFilters): ViewFilters {
  return { ...filters, statuses: filters.statuses ?? [...BOARD_STATUSES] };
}

function toGroupedFilter(req: Partial<ListIssuesParams>): Partial<ListGroupedIssuesParams> {
  const { assignee_filters, creator_filters, ...rest } = req;
  const toRefs = (tokens: string[]): IssueActorRef[] =>
    tokens.map((token) => {
      const sep = token.indexOf(":");
      const type = (sep === -1 ? token : token.slice(0, sep)) as IssueActorRef["type"];
      const id = sep === -1 ? "" : token.slice(sep + 1);
      return { type, id };
    });
  return {
    ...rest,
    ...(assignee_filters ? { assignee_filters: toRefs(assignee_filters) } : {}),
    ...(creator_filters ? { creator_filters: toRefs(creator_filters) } : {}),
  };
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
  const caches = await Promise.all([
    fetchFirstPages({ assignee_id: userId }, sort),
    fetchFirstPages({ creator_id: userId }, sort),
    fetchFirstPages({ involves_user_id: userId }, sort),
  ]);
  return mergeIssueCaches(caches);
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
  return mergeAssigneeGroups(responses);
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

/**
 * View-driven list options. Replaces {@link issueListOptions} +
 * client-side scope filtering: the view's filters are pushed to the server.
 * Keyed on (wsId, viewId, filters, sort). `select` flattens the bucketed
 * cache to Issue[] for the list/board/swimlane consumers.
 *
 * Single-branch views paginate via {@link useLoadMoreByStatus} (the bucket
 * `total` is the server count). Multi-branch (any_of) views render the first
 * page per status per branch and do NOT paginate — see fetchViewFirstPages.
 */
export function viewIssueListOptions(
  wsId: string,
  viewId: string | null,
  filters: ViewFilters,
  sort?: IssueSortParam,
) {
  return queryOptions({
    queryKey: issueKeys.viewListSorted(wsId, viewId, filters, sort),
    queryFn: () => fetchViewFirstPages(filters, sort),
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
 * View-driven assignee-grouped board options. Sibling of
 * {@link viewIssueListOptions} for the board (group_by=assignee). Resolves the
 * view's any_of branches, runs one grouped fetch per branch, and merges groups
 * by (assignee_type, assignee_id). The board's own status/priority/etc filters
 * are folded into `filters` by the caller, so this takes a ViewFilters too.
 */
export function viewIssueAssigneeGroupsOptions(
  wsId: string,
  viewId: string | null,
  filters: ViewFilters,
  sort?: IssueSortParam,
) {
  return queryOptions<GroupedIssuesResponse>({
    queryKey: issueKeys.viewAssigneeGroups(wsId, viewId, { ...filters, ...sort }),
    queryFn: () => fetchViewAssigneeGroups(filters, sort),
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
