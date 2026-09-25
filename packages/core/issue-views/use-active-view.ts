"use client";

import { useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import type { IssueView } from "../api/schemas";
import { issueViewListOptions, type IssueViewScope } from "./queries";
import {
  issueViewContainerKey,
  useActiveIssueViewStore,
} from "./active-view-store";

const EMPTY_VIEWS: IssueView[] = [];

/**
 * The open saved view for one surface container, resolved against the
 * container's list query. `missing` turns true when an id is active but the
 * settled list no longer carries it (deleted / access revoked) — callers
 * exit the view and toast.
 */
export function useActiveIssueView(
  wsId: string,
  scope: IssueViewScope | null,
) {
  const containerKey = scope ? issueViewContainerKey(wsId, scope) : "";
  const activeViewId = useActiveIssueViewStore((s) =>
    containerKey ? (s.active[containerKey] ?? null) : null,
  );
  const setActiveInStore = useActiveIssueViewStore((s) => s.setActive);

  const listQuery = useQuery({
    ...issueViewListOptions(wsId, scope ?? { scope_type: "workspace" }),
    enabled: !!scope && !!wsId,
  });
  const views = listQuery.data ?? EMPTY_VIEWS;
  const activeView = activeViewId
    ? (views.find((v) => v.id === activeViewId) ?? null)
    : null;
  // "Missing" must never fire off a STALE list: right after a create the
  // active id is set before the invalidated list lands, and treating that
  // gap as "view deleted" bounces the surface back to the default tab.
  const missing =
    !!activeViewId &&
    listQuery.isSuccess &&
    !listQuery.isFetching &&
    !activeView;

  const setActive = useCallback(
    (viewId: string | null) => {
      if (containerKey) setActiveInStore(containerKey, viewId);
    },
    [containerKey, setActiveInStore],
  );

  return {
    containerKey,
    activeViewId,
    activeView,
    views,
    // Preference writes prune against this list — callers must not persist
    // a prune until it has actually loaded (an empty in-flight list would
    // read as "every view was deleted").
    viewsReady: listQuery.isSuccess,
    setActive,
    missing,
  };
}
