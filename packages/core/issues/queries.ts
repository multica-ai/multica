import {
  infiniteQueryOptions,
  keepPreviousData,
  queryOptions,
  type QueryClient,
} from "@tanstack/react-query";
import { api, ApiError } from "../api";
import type {
  Issue,
  IssueAssigneeType,
  IssueStatusCategory,
  IssueTableFacetsRequest,
  IssueTableGroupSpec,
  IssueTableGroupsRequest,
  IssueTableQuerySpec,
  IssueTableRowsRequest,
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
  tableAll: (wsId: string) => [...issueKeys.all(wsId), "table-query"] as const,
  tableGroups: (
    wsId: string,
    query: IssueTableQuerySpec,
    group: IssueTableGroupsRequest["group"],
  ) => [...issueKeys.tableAll(wsId), "groups", query, group] as const,
  tableFacets: (
    wsId: string,
    request: IssueTableFacetsRequest,
  ) => [...issueKeys.tableAll(wsId), "facets", request] as const,
  tableRows: (
    wsId: string,
    query: IssueTableQuerySpec,
    group: IssueTableGroupSpec,
    groupKey: string | null,
    hierarchy: boolean,
    parentId: string | null,
  ) => [
    ...issueKeys.tableAll(wsId),
    "rows",
    query,
    group,
    groupKey,
    { hierarchy, parentId },
  ] as const,
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
  projectGantt: (
    wsId: string,
    projectId: string,
    assigneeTypes?: IssueAssigneeType[],
  ) =>
    [
      ...issueKeys.projectGanttAll(wsId),
      projectId,
      assigneeTypes ?? null,
    ] as const,
  detail: (wsId: string, id: string) =>
    [...issueKeys.all(wsId), "detail", id] as const,
  /** Resolve a bare issue identifier (e.g. "MUL-123") to an issue. */
  identifier: (wsId: string, identifier: string) =>
    [...issueKeys.all(wsId), "identifier", identifier] as const,
  /** Prefix for every per-parent children query in a workspace. */
  childrenAll: (wsId: string) =>
    [...issueKeys.all(wsId), "children"] as const,
  children: (wsId: string, id: string) =>
    [...issueKeys.childrenAll(wsId), id] as const,
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

/**
 * CATEGORIES fetched and paginated into the list/board cache — all 7,
 * `cancelled` included. `cancelled` is a first-class default (MUL-4290), so it
 * lives in the cache and renders like any other column; there is no separate
 * "visible board" subset. This constant governs fetch/cache membership.
 *
 * Keyed on category, not on status key (MUL-6243). A workspace can define any
 * number of custom statuses, and bucketing by status would mean one more
 * parallel `listIssues` request on every board load per status added. Bucketing
 * by category keeps the fan-out fixed at 7 forever; a custom status appears in
 * the column of the category it inherits, and the card's own badge is what
 * shows which specific status it is on.
 */
export const PAGINATED_CATEGORIES: readonly IssueStatusCategory[] = ALL_STATUSES;

/** Flatten a bucketed response to a single Issue[] for consumers that want the whole list. */
export function flattenIssueBuckets(data: ListIssuesCache) {
  const out = [];
  for (const status of PAGINATED_CATEGORIES) {
    const bucket = data.byStatus[status];
    if (bucket) out.push(...bucket.issues);
  }
  return out;
}

async function fetchFirstPages(filter: MyIssuesFilter = {}, sort?: IssueSortParam): Promise<ListIssuesCache> {
  const responses = await Promise.all(
    PAGINATED_CATEGORIES.map((category) =>
      api.listIssues({ status_category: category, limit: ISSUE_PAGE_SIZE, offset: 0, ...sort, ...filter }),
    ),
  );
  const byStatus: ListIssuesCache["byStatus"] = {};
  PAGINATED_CATEGORIES.forEach((status: IssueStatusCategory, i: number) => {
    const res = responses[i]!;
    byStatus[status] = { issues: res.issues, total: res.total };
  });
  return { byStatus };
}

export function issueTableGroupsOptions(
  wsId: string,
  query: IssueTableQuerySpec,
  group: IssueTableGroupsRequest["group"],
) {
  return infiniteQueryOptions({
    queryKey: issueKeys.tableGroups(wsId, query, group),
    initialPageParam: null as string | null,
    queryFn: ({ pageParam }) =>
      api.listIssueTableGroups({
        query,
        group,
        page: { limit: 100, cursor: pageParam },
    }),
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    placeholderData: keepPreviousData,
    retry: false,
  });
}

/** One independently-addressable Table branch page.
 *
 * Table keeps every cursor page under its own query key so a refreshed head
 * can detach stale tail cursors before their responses land. Keep the request,
 * key shape, transition placeholder, and retry policy together here — the
 * dynamic branch graph in views should not duplicate this API contract. */
export function issueTableRowPageOptions(
  wsId: string,
  request: IssueTableRowsRequest,
) {
  const cursor = request.page?.cursor ?? null;
  return queryOptions({
    queryKey: [
      ...issueKeys.tableRows(
        wsId,
        request.query,
        request.group,
        request.group_key,
        request.hierarchy.enabled,
        request.parent_id,
      ),
      "page",
      cursor,
    ] as const,
    queryFn: () => api.listIssueTableRows(request),
    placeholderData: keepPreviousData,
    retry: false,
    // Dynamic useQueries observers detach/reinstall as sibling branches enter
    // and leave the viewport. Keep an errored page errored until the user hits
    // Retry — but still let a successful-but-invalidated page refetch on
    // reattach, so a row page that gets WS-invalidated while its observer is
    // detached doesn't stay stranded stale under the global `staleTime: Infinity`
    // default (correct count, missing issue).
    //
    // Both guards are needed: `retryOnMount` covers a first-load error (no
    // data), while the `refetchOnMount` guard covers a background-refetch error
    // on a page that still holds data — TanStack flags such a page
    // `isInvalidated`, so a plain `refetchOnMount: true` would re-fire the
    // failing request on every reattach instead of waiting for explicit Retry.
    retryOnMount: false,
    refetchOnMount: (query) => query.state.status !== "error",
  });
}

export function issueTableFacetsOptions(
  wsId: string,
  request: IssueTableFacetsRequest,
) {
  return queryOptions({
    queryKey: issueKeys.tableFacets(wsId, request),
    queryFn: () => api.listIssueTableFacets(request),
  });
}

/**
 * CACHE SHAPE NOTE: The raw cache stores {@link ListIssuesCache} (buckets keyed
 * by status, each with `{ issues, total }`), and `select` flattens it to
 * `Issue[]` for consumers. Mutations and ws-updaters must use
 * `setQueryData<ListIssuesCache>(...)` and preserve the byStatus shape.
 *
 * Fetches the first page of each paginated status in parallel.
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

async function fetchProjectGanttIssues(
  projectId: string,
  assigneeTypes?: IssueAssigneeType[],
) {
  const issues = [];
  let offset = 0;
  while (offset < PROJECT_GANTT_MAX_ISSUES) {
    const res = await api.listIssues({
      project_id: projectId,
      scheduled: true,
      ...(assigneeTypes?.length ? { assignee_types: assigneeTypes } : {}),
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
export function projectGanttIssuesOptions(
  wsId: string,
  projectId: string,
  // The page's assignee-type tab narrows the Gantt exactly like every
  // other mode — same scope, same single mapping upstream.
  assigneeTypes?: IssueAssigneeType[],
) {
  return queryOptions({
    queryKey: issueKeys.projectGantt(wsId, projectId, assigneeTypes),
    queryFn: () => fetchProjectGanttIssues(projectId, assigneeTypes),
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
 * Backs the Linear-style autolink. This is an EXACT lookup, so it goes to
 * `GET /api/issues/{identifier}` — the server parses `PREFIX-NUMBER`, checks
 * the prefix against the workspace's own `issue_prefix`, and reads the issue
 * through the unique `(workspace_id, number)` index. A non-existent or
 * wrong-prefix identifier 404s, which maps to `null` here and renders as
 * plain text — the same contract the previous implementation had.
 *
 * It deliberately does NOT use `/api/issues/search`: that endpoint runs the
 * workspace-wide full-text query (title/description/comment `LIKE`, ranking,
 * snippet subquery, `COUNT(*) OVER()`) which is orders of magnitude more
 * expensive than a point read, and autolink resolution was the dominant
 * caller of it (MUL-6268).
 *
 * Server state → TanStack Query; the key includes `wsId` and the identifier,
 * so identical identifiers across the app share one request. Caller gates
 * `enabled` (identifier shape + workspace prefix).
 */
export function issueIdentifierOptions(wsId: string, identifier: string) {
  return queryOptions({
    queryKey: issueKeys.identifier(wsId, identifier),
    queryFn: async ({ signal }) => {
      try {
        return await api.getIssue(identifier, { signal });
      } catch (err) {
        // Unknown identifier / wrong workspace prefix → render as plain text.
        // Any other failure (401/5xx/abort) must keep propagating so the query
        // is retried or cancelled instead of being cached as "no such issue".
        if (err instanceof ApiError && err.status === 404) return null;
        throw err;
      }
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
      const map = new Map<string, {
        done: number;
        total: number;
        visibleDone: number;
        visibleTotal: number;
        hiddenTotal: number;
      }>();
      for (const entry of data.progress) {
        map.set(entry.parent_issue_id, {
          done: entry.done,
          total: entry.total,
          visibleDone: entry.visible_done ?? entry.done,
          visibleTotal: entry.visible_total ?? entry.total,
          hiddenTotal: entry.hidden_total ?? 0,
        });
      }
      return map;
    },
  });
}

export function childIssuesOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: issueKeys.children(wsId, id),
    queryFn: () => api.listChildIssues(id).then((r) => r.issues),
    // Child creation can happen while this workspace is not the active
    // realtime subscription (for example, an agent creates it while a
    // desktop tab is showing another workspace). The global Infinity
    // staleTime would otherwise reuse an incomplete children snapshot when
    // the parent is opened again, with no later event guaranteed to heal it.
    refetchOnMount: "always",
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
