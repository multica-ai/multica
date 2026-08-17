"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  useInfiniteQuery,
  useQueries,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import {
  issueKeys,
  issueTableGroupsOptions,
  issueTableRowPageOptions,
} from "@multica/core/issues/queries";
import type {
  Issue,
  IssueTableGroupDescriptor,
  IssueTableGroupsRequest,
  IssueTableQuerySpec,
  IssueTableRowsResponse,
} from "@multica/core/types";

export interface IssueGroupPageState {
  total: number;
  loaded: number;
  hasMore: boolean;
  isLoading: boolean;
  isFetching: boolean;
  isError: boolean;
  loadMore: () => void;
  retry: () => void;
}

export interface IssueGroupBranches {
  enabled: boolean;
  descriptors: IssueTableGroupDescriptor[];
  issues: Issue[];
  pagination: Record<string, IssueGroupPageState>;
  total: number;
  isLoading: boolean;
  isRefreshing: boolean;
  isError: boolean;
  hasMoreGroups: boolean;
  isLoadingMoreGroups: boolean;
  loadMoreGroups: () => void;
  retryGroups: () => void;
}

interface CursorState {
  identity: string;
  cursors: Record<string, Array<string | null>>;
}

interface PageTarget {
  key: string;
  cursor: string | null;
}

interface BranchData {
  rows: Issue[];
  nextCursor: string | null;
  isLoading: boolean;
  isFetching: boolean;
  isError: boolean;
  headUpdatedAt: number;
  headFetching: boolean;
}

function branchDescriptors(
  descriptors: readonly IssueTableGroupDescriptor[],
  secondaryValues?: readonly string[],
): IssueTableGroupDescriptor[] {
  const allowed = secondaryValues ? new Set(secondaryValues) : null;
  return descriptors.flatMap((descriptor) =>
    descriptor.secondary_groups?.length
      ? descriptor.secondary_groups.filter((secondary) => {
          if (!allowed || secondary.value.kind !== "status") return true;
          return allowed.has(secondary.value.status);
        })
      : [descriptor],
  );
}

function issueMatchesDescriptor(
  issue: Issue,
  descriptor: IssueTableGroupDescriptor,
  primary?: IssueTableGroupDescriptor,
) {
  const value = descriptor.value;
  if (value.kind === "status" && issue.status !== value.status) return false;
  const owner = primary?.value ?? value;
  switch (owner.kind) {
    case "assignee":
      return owner.actor
        ? issue.assignee_type === owner.actor.type &&
            issue.assignee_id === owner.actor.id
        : issue.assignee_type === null && issue.assignee_id === null;
    case "project":
      return issue.project_id === owner.project_id;
    case "parent":
      return issue.parent_issue_id === owner.parent_id;
    case "property": {
      const propertyValue = issue.properties?.[owner.property_id];
      if (owner.value_state === "unset") return propertyValue === undefined;
      if (owner.value_state === "value") return propertyValue === owner.value;
      return owner.value !== undefined
        ? propertyValue === owner.value
        : propertyValue !== undefined;
    }
    case "status":
      return issue.status === owner.status;
  }
}

/**
 * Cursor-paged server branches shared by non-status Board and Swimlane.
 * `/groups` owns the complete group catalog and exact counts; every group (or
 * compound lane×status cell) owns an independent `/rows` cursor chain.
 */
export function useIssueGroupBranches({
  wsId,
  query,
  group,
  secondaryValues,
  observeEmptyBranches = false,
  enabled,
}: {
  wsId: string;
  query: IssueTableQuerySpec;
  group: IssueTableGroupsRequest["group"];
  /** For compound groups, keep exact lane descriptors/counts while observing
   * rows only for the currently visible secondary buckets. */
  secondaryValues?: readonly string[];
  /** Compound and include-empty property responses carry zero-count keys.
   * Activate those when their mounted sentinel becomes visible so drag
   * targets have live heads. */
  observeEmptyBranches?: boolean;
  enabled: boolean;
}): IssueGroupBranches {
  const queryClient = useQueryClient();
  const identity = useMemo(
    () => JSON.stringify({ query, group }),
    [group, query],
  );
  const groupsQuery = useInfiniteQuery({
    ...issueTableGroupsOptions(wsId, query, group),
    enabled,
  });

  const descriptors = useMemo(
    () => groupsQuery.data?.pages.flatMap((page) => page.groups) ?? [],
    [groupsQuery.data?.pages],
  );
  const branches = useMemo(
    () => branchDescriptors(descriptors, secondaryValues),
    [descriptors, secondaryValues],
  );
  const branchKeys = useMemo(
    () => branches.map((descriptor) => descriptor.key),
    [branches],
  );
  const [cursorState, setCursorState] = useState<CursorState>({
    identity,
    cursors: {},
  });
  const activeCursorState = useMemo<CursorState>(() => {
    return cursorState.identity === identity
      ? cursorState
      : { identity, cursors: {} };
  }, [cursorState, identity]);
  useEffect(() => {
    if (activeCursorState !== cursorState) {
      setCursorState(activeCursorState);
    }
  }, [activeCursorState, cursorState]);

  const pageTargets = useMemo<PageTarget[]>(
    () =>
      enabled
        ? branchKeys.flatMap((key) =>
            (activeCursorState.cursors[key] ?? []).map((cursor) => ({
              key,
              cursor,
            })),
          )
        : [],
    [activeCursorState.cursors, branchKeys, enabled],
  );
  const headPlaceholderRef = useRef(
    new Map<string, IssueTableRowsResponse>(),
  );
  const pageQueries = useMemo(
    () =>
      pageTargets.map(({ key, cursor }) => {
        const placeholder =
          cursor === null ? headPlaceholderRef.current.get(key) : undefined;
        return {
          ...issueTableRowPageOptions(wsId, {
            query,
            group,
            group_key: key,
            hierarchy: { enabled: false },
            parent_id: null,
            page: { limit: 50, cursor },
          }),
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
      next.set(target.key, result.data);
    }
    headPlaceholderRef.current = next;
  }, [pageResults, pageTargets]);

  const primaryByBranch = useMemo(() => {
    const map = new Map<string, IssueTableGroupDescriptor>();
    for (const descriptor of descriptors) {
      for (const secondary of descriptor.secondary_groups ?? []) {
        map.set(secondary.key, descriptor);
      }
    }
    return map;
  }, [descriptors]);
  const descriptorByKey = useMemo(
    () => new Map(branches.map((descriptor) => [descriptor.key, descriptor])),
    [branches],
  );
  const branchData = useMemo(() => {
    const result = new Map<string, BranchData>();
    for (const key of branchKeys) {
      result.set(key, {
        rows: [],
        nextCursor: null,
        isLoading: false,
        isFetching: false,
        isError: false,
        headUpdatedAt: 0,
        headFetching: false,
      });
    }
    const headFetching = new Set<string>();
    for (let index = 0; index < pageTargets.length; index += 1) {
      const target = pageTargets[index];
      const page = pageResults[index];
      if (
        target?.cursor === null &&
        page?.isFetching &&
        (activeCursorState.cursors[target.key]?.length ?? 0) > 1
      ) {
        headFetching.add(target.key);
      }
    }
    const seenByKey = new Map<string, Set<string>>();
    for (let index = 0; index < pageTargets.length; index += 1) {
      const target = pageTargets[index];
      const pageResult = pageResults[index];
      if (!target || !pageResult) continue;
      if (target.cursor !== null && headFetching.has(target.key)) continue;
      const current = result.get(target.key);
      const descriptor = descriptorByKey.get(target.key);
      if (!current || !descriptor) continue;
      if (pageResult.data) {
        const seen = seenByKey.get(target.key) ?? new Set<string>();
        for (const row of pageResult.data.rows) {
          if (
            !issueMatchesDescriptor(
              row.issue,
              descriptor,
              primaryByBranch.get(target.key),
            ) ||
            seen.has(row.issue.id)
          ) {
            continue;
          }
          seen.add(row.issue.id);
          current.rows.push(row.issue);
        }
        seenByKey.set(target.key, seen);
        current.nextCursor = pageResult.data.next_cursor;
      }
      current.isLoading ||= pageResult.isPending;
      current.isFetching ||= pageResult.isFetching;
      current.isError ||= pageResult.isError;
      if (target.cursor === null) {
        current.headUpdatedAt = pageResult.dataUpdatedAt;
        current.headFetching = pageResult.isFetching;
      }
    }
    return result;
  }, [
    activeCursorState.cursors,
    branchKeys,
    descriptorByKey,
    pageResults,
    pageTargets,
    primaryByBranch,
  ]);

  const headRevisionRef = useRef<{
    identity: string;
    revisions: Record<string, number>;
  }>({ identity, revisions: {} });
  useEffect(() => {
    const previous =
      headRevisionRef.current.identity === identity
        ? headRevisionRef.current.revisions
        : {};
    const next: Record<string, number> = {};
    const trim = new Set<string>();
    for (const key of branchKeys) {
      const branch = branchData.get(key);
      if (!branch || branch.headUpdatedAt === 0) continue;
      next[key] = branch.headUpdatedAt;
      const seen = previous[key];
      if (
        (activeCursorState.cursors[key]?.length ?? 0) > 1 &&
        (branch.headFetching ||
          (seen !== undefined && seen !== branch.headUpdatedAt))
      ) {
        trim.add(key);
      }
    }
    headRevisionRef.current = { identity, revisions: next };
    if (trim.size === 0) return;
    setCursorState((previousState) => {
      if (previousState.identity !== identity) return previousState;
      const cursors = { ...previousState.cursors };
      for (const key of trim) cursors[key] = [null];
      return { ...previousState, cursors };
    });
  }, [
    activeCursorState.cursors,
    branchData,
    branchKeys,
    identity,
  ]);

  const loadMore = useCallback(
    (key: string) => {
      const cursor = branchData.get(key)?.nextCursor;
      setCursorState((previous) => {
        if (previous.identity !== identity) return previous;
        const current = previous.cursors[key] ?? [];
        // A descriptor does not install a row observer by itself. The first
        // visible sentinel activates the head; subsequent calls append the
        // server cursor. This bounds Swimlane's first paint to mounted lanes
        // instead of lanes × statuses across the complete catalog.
        if (current.length === 0) {
          return {
            ...previous,
            cursors: { ...previous.cursors, [key]: [null] },
          };
        }
        if (!cursor) return previous;
        if (current.includes(cursor)) return previous;
        return {
          ...previous,
          cursors: { ...previous.cursors, [key]: [...current, cursor] },
        };
      });
    },
    [branchData, identity],
  );
  const retry = useCallback(
    (key: string) => {
      void queryClient.refetchQueries({
        queryKey: issueKeys.tableRows(
          wsId,
          query,
          group,
          key,
          false,
          null,
        ),
        exact: false,
        type: "active",
      });
    },
    [group, query, queryClient, wsId],
  );
  const pagination = useMemo(
    () =>
      Object.fromEntries(
        branches.map((descriptor) => {
          const branch = branchData.get(descriptor.key);
          const active =
            (activeCursorState.cursors[descriptor.key]?.length ?? 0) > 0;
          return [
            descriptor.key,
            {
              total: descriptor.count,
              loaded: branch?.rows.length ?? 0,
              hasMore:
                enabled &&
                (active
                  ? !!branch?.nextCursor
                  : descriptor.count > 0 || observeEmptyBranches),
              isLoading: enabled && (branch?.isLoading ?? false),
              isFetching: enabled && (branch?.isFetching ?? false),
              isError: enabled && (branch?.isError ?? false),
              loadMore: () => loadMore(descriptor.key),
              retry: () => retry(descriptor.key),
            },
          ];
        }),
      ) as Record<string, IssueGroupPageState>,
    [
      activeCursorState.cursors,
      branchData,
      branches,
      enabled,
      loadMore,
      observeEmptyBranches,
      retry,
    ],
  );
  const issues = useMemo(
    () => branchKeys.flatMap((key) => branchData.get(key)?.rows ?? []),
    [branchData, branchKeys],
  );
  const rowsPending = branchKeys.some(
    (key) => branchData.get(key)?.isLoading,
  );
  const rowsFetching = branchKeys.some(
    (key) => branchData.get(key)?.isFetching,
  );
  const fetchNextGroupPage = groupsQuery.fetchNextPage;
  const refetchGroups = groupsQuery.refetch;
  const hasNextGroupPage = groupsQuery.hasNextPage;
  const isFetchingNextGroupPage = groupsQuery.isFetchingNextPage;
  const loadMoreGroups = useCallback(() => {
    if (hasNextGroupPage && !isFetchingNextGroupPage) {
      void fetchNextGroupPage();
    }
  }, [
    fetchNextGroupPage,
    hasNextGroupPage,
    isFetchingNextGroupPage,
  ]);
  const retryGroups = useCallback(() => {
    void refetchGroups();
  }, [refetchGroups]);

  return {
    enabled,
    descriptors,
    issues,
    pagination,
    // `/groups` owns the exact query-wide visible total. Deriving it from
    // loaded descriptors made a hidden-only first group page look globally
    // empty even when a later page contained visible cards.
    total: groupsQuery.data?.pages[0]?.total ?? issues.length,
    isLoading: enabled && groupsQuery.isPending,
    isRefreshing:
      enabled &&
      !groupsQuery.isPending &&
      (groupsQuery.isFetching || rowsPending || rowsFetching),
    isError:
      enabled &&
      (groupsQuery.isError ||
        branchKeys.some((key) => branchData.get(key)?.isError)),
    hasMoreGroups: enabled && !!hasNextGroupPage,
    isLoadingMoreGroups: enabled && isFetchingNextGroupPage,
    loadMoreGroups,
    retryGroups,
  };
}
