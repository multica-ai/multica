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
import { statusColumnKeys } from "@multica/core/issues";
import {
  isBuiltInIssueStatus,
} from "@multica/core/issue-statuses";
import type { IssueStatusCatalog } from "@multica/core/issue-statuses";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import {
  issueKeys,
  issueTableRowPageOptions,
} from "@multica/core/issues/queries";
import type {
  Issue,
  IssueStatus,
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

export type IssueStatusPagination = Record<IssueStatus, IssueStatusPageState>;

interface StatusCursorState {
  identity: string;
  cursors: Record<IssueStatus, Array<string | null>>;
}

interface StatusPageTarget {
  status: IssueStatus;
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

// Page each concrete status independently, including custom statuses.
function statusGroupKey(status: IssueStatus) {
  return `status:${status}`;
}

/** Exact-key grouping keeps sibling statuses in independent branches. */
const STATUS_GROUP = { kind: "status" } as const;

function initialCursorState(
  identity: string,
  statuses: readonly IssueStatus[],
): StatusCursorState {
  const cursors = Object.fromEntries(
    statuses.map((status) => [
      status,
      [null],
    ]),
  ) as StatusCursorState["cursors"];
  return { identity, cursors };
}

function rebaseCursorState(
  state: StatusCursorState,
  identity: string,
  statuses: readonly IssueStatus[],
) {
  const current =
    state.identity === identity ? state : initialCursorState(identity, statuses);
  let cursors: StatusCursorState["cursors"] | null = null;
  for (const status of statuses) {
    if ((current.cursors[status]?.length ?? 0) > 0) continue;
    cursors ??= { ...current.cursors };
    cursors[status] = [null];
  }
  return cursors ? { ...current, cursors } : current;
}

/**
 * Per-status totals, never folded into lifecycle categories. The facet is
 * disjunctive, so narrow it to the selected keys when a status filter is active.
 * Unknown keys wait for catalog refresh instead of inflating another column.
 */
function statusCountsFromFacets(
  facets: IssueTableFacetsResponse | undefined,
  catalog: Pick<IssueStatusCatalog, "entryOf" | "isLoaded">,
  selectedStatuses: readonly string[] | undefined,
) {
  const counts = new Map<IssueStatus, number>();
  const selected =
    selectedStatuses && selectedStatuses.length > 0
      ? new Set(selectedStatuses)
      : null;
  const statusFacet = facets?.facets.find((facet) => facet.kind === "status");
  for (const value of statusFacet?.values ?? []) {
    if (selected && !selected.has(value.key)) continue;
    if (!isBuiltInIssueStatus(value.key) && !catalog.entryOf(value.key)) continue;
    counts.set(value.key, value.count);
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
  statuses: readonly IssueStatus[];
  facets: IssueTableFacetsResponse | undefined;
  facetsPending: boolean;
  facetsFetching: boolean;
  enabled: boolean;
}): IssueStatusBranches {
  const queryClient = useQueryClient();
  const catalog = useIssueStatuses(wsId);
  const group = STATUS_GROUP;
  const identity = useMemo(
    () => JSON.stringify({ wsId, query, group: group.kind }),
    [group.kind, query, wsId],
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
            (activeCursorState.cursors[status] ?? []).map((cursor) => ({
              status,
              cursor,
            })),
          )
        : [],
    [activeCursorState.cursors, enabled, statuses],
  );
  const headPlaceholderRef = useRef(
    new Map<IssueStatus, IssueTableRowsResponse>(),
  );
  const placeholderWorkspaceRef = useRef(wsId);
  if (placeholderWorkspaceRef.current !== wsId) {
    headPlaceholderRef.current.clear();
    placeholderWorkspaceRef.current = wsId;
  }
  const pageQueries = useMemo(
    () =>
      pageTargets.map(({ status, cursor }) => {
        const placeholder =
          cursor === null ? headPlaceholderRef.current.get(status) : undefined;
        return {
          ...issueTableRowPageOptions(wsId, {
            query,
            group,
            group_key: statusGroupKey(status),
            hierarchy: { enabled: false },
            parent_id: null,
            page: { limit: 50, cursor },
          }),
          // useQueries replaces observers when the query hash changes, so its
          // built-in keepPreviousData cannot bridge a filter/sort transition.
          // Retain only the last settled HEAD per concrete status branch. Tails
          // are deliberately detached; exact facets remain server-owned.
          ...(placeholder ? { placeholderData: () => placeholder } : {}),
          enabled,
        };
      }),
    [enabled, group, pageTargets, query, wsId],
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
    const result = new Map<IssueStatus, StatusBranchData>();
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

    const headFetching = new Set<IssueStatus>();
    for (let index = 0; index < pageTargets.length; index += 1) {
      const target = pageTargets[index];
      const queryResult = pageResults[index];
      if (
        target?.cursor === null &&
        queryResult?.isFetching &&
        (activeCursorState.cursors[target.status]?.length ?? 0) > 1
      ) {
        headFetching.add(target.status);
      }
    }

    const seenByStatus = new Map<IssueStatus, Set<string>>();
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
          // patched card under a column whose exact key no longer matches.
          if (row.issue.status !== target.status) continue;
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
    revisions: Partial<Record<IssueStatus, number>>;
  }>({ identity, revisions: {} });
  useEffect(() => {
    const previous =
      headRevisionRef.current.identity === identity
        ? headRevisionRef.current.revisions
        : {};
    const next: Partial<Record<IssueStatus, number>> = {};
    const trim = new Set<IssueStatus>();
    for (const status of statuses) {
      const branch = branchData.get(status);
      if (!branch || branch.headUpdatedAt === 0) continue;
      next[status] = branch.headUpdatedAt;
      const seen = previous[status];
      if (
        (activeCursorState.cursors[status]?.length ?? 0) > 1 &&
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
    () => statusCountsFromFacets(facets, catalog, query.filters.statuses),
    [facets, catalog, query.filters.statuses],
  );
  const loadMore = useCallback(
    (status: IssueStatus) => {
      const cursor = branchData.get(status)?.nextCursor;
      if (!cursor) return;
      setCursorState((previous) => {
        if (previous.identity !== identity) return previous;
        const current = previous.cursors[status] ?? [null];
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
    (status: IssueStatus) => {
      void queryClient.refetchQueries({
        queryKey: issueKeys.tableRows(
          wsId,
          query,
          group,
          statusGroupKey(status),
          false,
          null,
        ),
        exact: false,
        type: "active",
      });
    },
    [group, query, queryClient, wsId],
  );

  const pagination = useMemo<IssueStatusPagination>(() => {
    return Object.fromEntries(
      statusColumnKeys(catalog).map((status) => {
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
  }, [branchData, catalog, counts, enabled, loadMore, retry]);

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
