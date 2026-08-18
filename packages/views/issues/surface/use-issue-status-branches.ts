"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  useQueries,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import { ALL_STATUSES } from "@multica/core/issues/config";
import { statusCategoryOfKey } from "@multica/core/issues";
import type { IssueStatusCatalog } from "@multica/core/issue-statuses";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import {
  issueKeys,
  issueTableRowPageOptions,
} from "@multica/core/issues/queries";
import type {
  Issue,
  IssueStatusCategory,
  IssueTableFacetsResponse,
  IssueTableQuerySpec,
  IssueTableRowsResponse,
} from "@multica/core/types";

export interface IssueStatusPageState {
  total: number;
  loaded: number;
  hasMore: boolean;
  isLoading: boolean;
  isFetching: boolean;
  isError: boolean;
  loadMore: () => void;
  retry: () => void;
}

export type IssueStatusPagination = Record<
  IssueStatusCategory,
  IssueStatusPageState
>;

interface StatusCursorState {
  identity: string;
  cursors: Record<IssueStatusCategory, Array<string | null>>;
}

interface StatusPageTarget {
  status: IssueStatusCategory;
  cursor: string | null;
}

interface StatusBranchData {
  rows: Issue[];
  nextCursor: string | null;
  isLoading: boolean;
  isFetching: boolean;
  isError: boolean;
  headUpdatedAt: number;
  headFetching: boolean;
  /** The head (cursor === null) page alone is still loading with nothing to
   * show. Distinct from `isLoading`, which also accumulates tail pages so the
   * per-branch load-more sentinel can spin. */
  headPending: boolean;
}

// Every "status" in this hook is a board COLUMN, and columns are categories:
// a workspace's custom statuses live inside their category's column rather than
// adding one of their own. (MUL-6243)
function statusGroupKey(status: IssueStatusCategory, byCategory: boolean) {
  return byCategory ? `status_category:${status}` : `status:${status}`;
}

/**
 * The grouping contract this hook pages with.
 *
 * `status_category` is a server contract this feature introduced, so it is only
 * sent once the workspace is KNOWN to have custom statuses — see
 * `IssueStatusCatalog.hasCustomStatuses` for why that is both the
 * rolling-deploy guard and the cold-load guard. Everyone else keeps the exact
 * request they made before. (MUL-6243)
 */
const CATEGORY_GROUP = { kind: "status_category" } as const;
const STATUS_GROUP = { kind: "status" } as const;

function initialCursorState(
  identity: string,
  statuses: readonly IssueStatusCategory[],
): StatusCursorState {
  const cursors = Object.fromEntries(
    ALL_STATUSES.map((status) => [
      status,
      statuses.includes(status) ? [null] : [],
    ]),
  ) as StatusCursorState["cursors"];
  return { identity, cursors };
}

function rebaseCursorState(
  state: StatusCursorState,
  identity: string,
  statuses: readonly IssueStatusCategory[],
) {
  const current =
    state.identity === identity ? state : initialCursorState(identity, statuses);
  let cursors: StatusCursorState["cursors"] | null = null;
  for (const status of statuses) {
    if (current.cursors[status].length > 0) continue;
    cursors ??= { ...current.cursors };
    cursors[status] = [null];
  }
  return cursors ? { ...current, cursors } : current;
}

/**
 * The column an issue belongs to.
 *
 * `status_category` is filled by the server for every issue payload, so this is
 * a plain read in the normal case. The fallback covers a row that predates the
 * field (an older backend, or an optimistic client-side patch): a built-in key
 * IS its own category, so it still resolves exactly, and a custom key with no
 * category lands in `todo` rather than disappearing.
 */
function rowCategory(issue: Issue): IssueStatusCategory {
  return issue.status_category ?? statusCategoryOfKey(issue.status);
}

/**
 * Per-column totals from the server's status facet.
 *
 * The facet is keyed by concrete status KEY, while columns are categories, so
 * custom keys are folded into their category instead of being discarded —
 * dropping them made a column's total disagree with the cards it rendered.
 *
 * A key the LOADED catalog does not know is dropped rather than guessed into
 * `todo`: `categoryOf` cannot distinguish "custom status created seconds ago
 * elsewhere" from "not a status at all", and inflating an arbitrary column's
 * header is worse than a total that is briefly one card short. Before the
 * catalog loads there is nothing to fold, so only built-ins are counted — which
 * is exactly the pre-feature behavior. (MUL-6243)
 */
function statusCountsFromFacets(
  facets: IssueTableFacetsResponse | undefined,
  catalog: Pick<IssueStatusCatalog, "entryOf" | "isLoaded">,
) {
  const counts = new Map<IssueStatusCategory, number>();
  const statusFacet = facets?.facets.find((facet) => facet.kind === "status");
  for (const value of statusFacet?.values ?? []) {
    const builtIn = ALL_STATUSES.find((category) => category === value.key);
    const category = builtIn ?? (catalog.isLoaded ? catalog.entryOf(value.key)?.category : undefined);
    if (!category || !ALL_STATUSES.includes(category)) continue;
    counts.set(category, (counts.get(category) ?? 0) + value.count);
  }
  return counts;
}

export interface IssueStatusBranches {
  enabled: boolean;
  issues: Issue[];
  pagination: IssueStatusPagination;
  total: number;
  isTotalKnown: boolean;
  isLoading: boolean;
  isRefreshing: boolean;
}

/**
 * Server-authoritative status branches shared by List and status-grouped
 * Board. Every branch page is the same keyset-paged `/table/rows` contract
 * used by Table; status totals come from the disjunctive status facet, so
 * hidden/empty columns never depend on the loaded card window.
 */
export function useIssueStatusBranches({
  wsId,
  query,
  statuses,
  facets,
  facetsPending,
  facetsFetching,
  enabled,
}: {
  wsId: string;
  query: IssueTableQuerySpec;
  statuses: readonly IssueStatusCategory[];
  facets: IssueTableFacetsResponse | undefined;
  facetsPending: boolean;
  facetsFetching: boolean;
  enabled: boolean;
}): IssueStatusBranches {
  const queryClient = useQueryClient();
  const catalog = useIssueStatuses(wsId);
  const { hasCustomStatuses } = catalog;
  const group = hasCustomStatuses ? CATEGORY_GROUP : STATUS_GROUP;
  // The grouping contract is part of the cursor identity, not just the query:
  // it flips from `status` to `status_category` the moment the catalog lands,
  // and a cursor minted against the old contract is meaningless to the new one.
  // Without this, catalog arrival carried stale `status:` cursors into
  // `status_category:` requests. (MUL-6243)
  const identity = useMemo(
    () => JSON.stringify({ query, group: group.kind }),
    [group.kind, query],
  );
  const [cursorState, setCursorState] = useState<StatusCursorState>(() =>
    initialCursorState(identity, statuses),
  );
  const activeCursorState = rebaseCursorState(
    cursorState,
    identity,
    statuses,
  );

  useEffect(() => {
    if (cursorState !== activeCursorState) {
      setCursorState(activeCursorState);
    }
  }, [activeCursorState, cursorState]);

  const pageTargets = useMemo<StatusPageTarget[]>(
    () =>
      enabled
        ? statuses.flatMap((status) =>
            activeCursorState.cursors[status].map((cursor) => ({
              status,
              cursor,
            })),
          )
        : [],
    [activeCursorState.cursors, enabled, statuses],
  );
  const headPlaceholderRef = useRef(
    new Map<IssueStatusCategory, IssueTableRowsResponse>(),
  );
  const pageQueries = useMemo(
    () =>
      pageTargets.map(({ status, cursor }) => {
        const placeholder =
          cursor === null ? headPlaceholderRef.current.get(status) : undefined;
        return {
          ...issueTableRowPageOptions(wsId, {
            query,
            group,
            group_key: statusGroupKey(status, hasCustomStatuses),
            hierarchy: { enabled: false },
            parent_id: null,
            page: { limit: 50, cursor },
          }),
          // useQueries replaces observers when the query hash changes, so its
          // built-in keepPreviousData cannot bridge a filter/sort transition.
          // Retain only the last settled HEAD per fixed status branch. Tails
          // are deliberately detached; exact facets remain server-owned.
          ...(placeholder ? { placeholderData: () => placeholder } : {}),
          enabled,
        };
      }),
    [enabled, group, hasCustomStatuses, pageTargets, query, wsId],
  );
  const pageResults = useQueries({ queries: pageQueries }) as Array<
    UseQueryResult<IssueTableRowsResponse, Error>
  >;
  useEffect(() => {
    const next = new Map(headPlaceholderRef.current);
    for (let index = 0; index < pageTargets.length; index += 1) {
      const target = pageTargets[index];
      const result = pageResults[index];
      if (
        target?.cursor !== null ||
        !result?.data ||
        result.isPlaceholderData ||
        result.isError
      ) {
        continue;
      }
      next.set(target.status, result.data);
    }
    headPlaceholderRef.current = next;
  }, [pageResults, pageTargets]);

  const branchData = useMemo(() => {
    const result = new Map<IssueStatusCategory, StatusBranchData>();
    for (const status of statuses) {
      result.set(status, {
        rows: [],
        nextCursor: null,
        isLoading: false,
        isFetching: false,
        isError: false,
        headUpdatedAt: 0,
        headFetching: false,
        headPending: false,
      });
    }

    const headFetching = new Set<IssueStatusCategory>();
    for (let index = 0; index < pageTargets.length; index += 1) {
      const target = pageTargets[index];
      const queryResult = pageResults[index];
      if (
        target?.cursor === null &&
        queryResult?.isFetching &&
        activeCursorState.cursors[target.status].length > 1
      ) {
        headFetching.add(target.status);
      }
    }

    const seenByStatus = new Map<IssueStatusCategory, Set<string>>();
    for (let index = 0; index < pageTargets.length; index += 1) {
      const target = pageTargets[index];
      const queryResult = pageResults[index];
      if (!target || !queryResult) continue;
      // A broad invalidation makes every cursor stale as soon as the head
      // starts refreshing. Hide detached tails immediately; the effect below
      // trims their cursor observers before their responses can re-enter.
      if (target.cursor !== null && headFetching.has(target.status)) continue;

      const current = result.get(target.status);
      if (!current) continue;
      const page = queryResult.data;
      if (page) {
        const seen = seenByStatus.get(target.status) ?? new Set<string>();
        for (const row of page.rows) {
          // Realtime can patch an issue's status before the broad query
          // invalidation has moved it between branch caches. Never render a
          // patched card under a column it no longer belongs to. The comparison
          // is against the CATEGORY, so a custom status stays in its column
          // instead of being dropped for not equalling the column key.
          if (rowCategory(row.issue) !== target.status) continue;
          if (seen.has(row.issue.id)) continue;
          seen.add(row.issue.id);
          current.rows.push(row.issue);
        }
        seenByStatus.set(target.status, seen);
        current.nextCursor = page.next_cursor;
      }
      current.isLoading ||= queryResult.isPending;
      current.isFetching ||= queryResult.isFetching;
      current.isError ||= queryResult.isError;
      if (target.cursor === null) {
        current.headUpdatedAt = queryResult.dataUpdatedAt;
        current.headFetching = queryResult.isFetching;
        current.headPending = queryResult.isPending;
      }
    }
    return result;
  }, [
    activeCursorState.cursors,
    pageResults,
    pageTargets,
    statuses,
  ]);

  // Once a head page refreshes, its old cursor chain no longer belongs to the
  // current snapshot. Match Table's branch behavior by dropping every tail.
  const headRevisionRef = useRef<{
    identity: string;
    revisions: Partial<Record<IssueStatusCategory, number>>;
  }>({ identity, revisions: {} });
  useEffect(() => {
    const previous =
      headRevisionRef.current.identity === identity
        ? headRevisionRef.current.revisions
        : {};
    const next: Partial<Record<IssueStatusCategory, number>> = {};
    const trim = new Set<IssueStatusCategory>();
    for (const status of statuses) {
      const branch = branchData.get(status);
      if (!branch || branch.headUpdatedAt === 0) continue;
      next[status] = branch.headUpdatedAt;
      const seen = previous[status];
      if (
        activeCursorState.cursors[status].length > 1 &&
        (branch.headFetching ||
          (seen !== undefined && seen !== branch.headUpdatedAt))
      ) {
        trim.add(status);
      }
    }
    headRevisionRef.current = { identity, revisions: next };
    if (trim.size === 0) return;
    setCursorState((previousState) => {
      if (previousState.identity !== identity) return previousState;
      const cursors = { ...previousState.cursors };
      for (const status of trim) cursors[status] = [null];
      return { ...previousState, cursors };
    });
  }, [
    activeCursorState.cursors,
    branchData,
    identity,
    statuses,
  ]);

  const counts = useMemo(
    () => statusCountsFromFacets(facets, catalog),
    [facets, catalog],
  );
  const loadMore = useCallback(
    (status: IssueStatusCategory) => {
      const cursor = branchData.get(status)?.nextCursor;
      if (!cursor) return;
      setCursorState((previous) => {
        if (previous.identity !== identity) return previous;
        const current = previous.cursors[status];
        if (current.includes(cursor)) return previous;
        return {
          ...previous,
          cursors: {
            ...previous.cursors,
            [status]: [...current, cursor],
          },
        };
      });
    },
    [branchData, identity],
  );
  const retry = useCallback(
    (status: IssueStatusCategory) => {
      void queryClient.refetchQueries({
        queryKey: issueKeys.tableRows(
          wsId,
          query,
          group,
          statusGroupKey(status, hasCustomStatuses),
          false,
          null,
        ),
        exact: false,
        type: "active",
      });
    },
    [group, hasCustomStatuses, query, queryClient, wsId],
  );

  const pagination = useMemo<IssueStatusPagination>(() => {
    return Object.fromEntries(
      ALL_STATUSES.map((status) => {
        const branch = branchData.get(status);
        const loaded = branch?.rows.length ?? 0;
        const total = counts.get(status) ?? loaded;
        return [
          status,
          {
            total,
            loaded,
            hasMore: enabled && !!branch?.nextCursor,
            isLoading: enabled && (branch?.isLoading ?? false),
            isFetching: enabled && (branch?.isFetching ?? false),
            isError: enabled && (branch?.isError ?? false),
            loadMore: () => loadMore(status),
            retry: () => retry(status),
          },
        ];
      }),
    ) as IssueStatusPagination;
  }, [branchData, counts, enabled, loadMore, retry]);

  const issues = useMemo(
    () => statuses.flatMap((status) => branchData.get(status)?.rows ?? []),
    [branchData, statuses],
  );
  const isTotalKnown = facets !== undefined;
  const total = facets?.total ?? issues.length;
  // Surface-level loading reflects only each branch's HEAD page. A tail page
  // (load-more) is pending/fetching while the already-rendered rows stay on
  // screen, so it must never re-trigger the full-surface skeleton or the
  // global refreshing indicator — the per-branch sentinel owns "loading more".
  const headsPending = statuses.some(
    (status) => branchData.get(status)?.headPending,
  );
  const headsFetching = statuses.some(
    (status) => branchData.get(status)?.headFetching,
  );

  return {
    enabled,
    issues,
    pagination,
    total,
    isTotalKnown,
    isLoading: enabled && (facetsPending || headsPending),
    isRefreshing:
      enabled &&
      !facetsPending &&
      !headsPending &&
      (facetsFetching || headsFetching),
  };
}
