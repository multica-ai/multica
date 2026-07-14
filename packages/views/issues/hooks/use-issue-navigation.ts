"use client";

import { useEffect, useMemo } from "react";
import {
  useIssueNavigationStore,
  getIssueSiblings,
  type IssueSiblings,
} from "@multica/core/issues/stores/issue-navigation-store";

/**
 * Publish the columns a list view is currently showing so the issue detail can
 * offer previous/next navigation within a column. Called by the board and list
 * views with the active workspace id and their displayed column map.
 *
 * Within a workspace the most recent caller wins, which is always the surface
 * the user is looking at. This relies on at most one board/list being mounted
 * per workspace at a time — true today since every view is a mutually exclusive
 * `viewMode` branch and the reuse sites (my-issues, projects, actor panels)
 * live on separate routes. A future split/peek layout co-mounting two lists in
 * one workspace would make this last-writer-wins across them.
 *
 * The snapshot intentionally outlives the list view: navigating into an issue
 * unmounts the list, but the detail still needs the columns the user just left.
 */
export function useRegisterIssueNavigation(
  wsId: string,
  columns: Record<string, string[]>,
): void {
  const setColumns = useIssueNavigationStore((s) => s.setColumns);
  useEffect(() => {
    setColumns(wsId, columns);
  }, [wsId, columns, setColumns]);
}

/**
 * Clear this workspace's published columns on mount. Views without a meaningful
 * column ordering (swimlane, gantt) call this so the detail won't offer
 * navigation that wouldn't match what the user was looking at when they opened
 * the issue.
 */
export function useClearIssueNavigation(wsId: string): void {
  const clear = useIssueNavigationStore((s) => s.clear);
  useEffect(() => {
    clear(wsId);
  }, [wsId, clear]);
}

/**
 * Resolve the previous/next issue for the detail view from the last list
 * published for this workspace. Selecting the stable per-workspace columns
 * reference (not a freshly built object) keeps this from re-rendering on every
 * store write, including writes scoped to other workspaces.
 */
export function useIssueSiblings(wsId: string, issueId: string): IssueSiblings {
  const columns = useIssueNavigationStore((s) => s.byWorkspace[wsId]);
  return useMemo(() => getIssueSiblings(columns, issueId), [columns, issueId]);
}
