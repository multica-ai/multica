"use client";

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import type { IssueView } from "../api/schemas";
import { pinListOptions } from "../pins/queries";
import type { PinnedItem } from "../types";
import { issueViewEverywhereOptions } from "./queries";
import { issueViewPreferenceOptions, useUpdateIssueViewPreference } from "./preferences";

/** The views every workspace has without saving anything. */
export type BuiltinViewKey = "my_issues" | "my_agents" | "all_issues";
export const BUILTIN_VIEW_KEYS: readonly BuiltinViewKey[] = ["my_issues", "my_agents", "all_issues"];

export const builtinViewItemId = (key: BuiltinViewKey) => `builtin:${key}`;
export const savedViewItemId = (viewId: string) => `view:${viewId}`;

export const SIDEBAR_VIEWS_SCOPE = { scope_type: "sidebar" } as const;

export type SidebarViewItem =
  | { id: string; kind: "builtin"; key: BuiltinViewKey }
  | { id: string; kind: "view"; view: IssueView };

const EMPTY_VIEWS: IssueView[] = [];
const EMPTY_PINS: PinnedItem[] = [];

/**
 * The starred list before the user has arranged it: the built-ins, the
 * user's own views, then views they had pinned under the old Pinned section.
 */
export function defaultSidebarViewOrder(
  views: readonly IssueView[],
  userId: string | undefined,
  pinnedViewIds: readonly string[],
): string[] {
  const ids = BUILTIN_VIEW_KEYS.map(builtinViewItemId);
  for (const view of views) {
    if (view.owner_id === userId) ids.push(savedViewItemId(view.id));
  }
  for (const viewId of pinnedViewIds) {
    const id = savedViewItemId(viewId);
    if (!ids.includes(id)) ids.push(id);
  }
  return ids;
}

/** Resolve starred ids to items, dropping views that were deleted or unshared. */
export function resolveSidebarViewItems(
  order: readonly string[],
  views: readonly IssueView[],
): SidebarViewItem[] {
  const byId = new Map(views.map((view) => [savedViewItemId(view.id), view]));
  const builtins = new Map(BUILTIN_VIEW_KEYS.map((key) => [builtinViewItemId(key), key]));
  const items: SidebarViewItem[] = [];
  const seen = new Set<string>();
  for (const id of order) {
    if (seen.has(id)) continue;
    seen.add(id);
    const key = builtins.get(id);
    if (key) {
      items.push({ id, kind: "builtin", key });
      continue;
    }
    const view = byId.get(id);
    if (view) items.push({ id, kind: "view", view });
  }
  return items;
}

/**
 * Puts the visible ids, in their new order, back into the slots visible ids
 * held, so starred views that do not resolve yet keep their place.
 */
export function reorderSidebarViewOrder(order: readonly string[], visibleIds: readonly string[]): string[] {
  const visible = new Set(visibleIds);
  const queue = [...visibleIds];
  const next = [...new Set(order)].map((id) => (visible.has(id) ? queue.shift()! : id));
  return [...next, ...queue];
}

/**
 * The user's starred views in sidebar order, with the writes that rearrange
 * them. Until the first write the list is derived, so views the user creates
 * elsewhere still show up.
 */
export function useSidebarViews(wsId: string, userId: string | undefined) {
  const viewsQuery = useQuery(issueViewEverywhereOptions(wsId));
  const views = viewsQuery.data ?? EMPTY_VIEWS;
  const preferenceQuery = useQuery(issueViewPreferenceOptions(wsId, SIDEBAR_VIEWS_SCOPE));
  const pinsQuery = useQuery({
    ...pinListOptions(wsId, userId ?? ""),
    enabled: !!wsId && !!userId,
  });
  const pins = pinsQuery.data ?? EMPTY_PINS;
  const save = useUpdateIssueViewPreference(wsId, SIDEBAR_VIEWS_SCOPE);

  const preference = preferenceQuery.data;
  const savedOrder = preference && preference.updated_at !== "" ? preference.prefs.order : null;
  const order = useMemo(
    () =>
      savedOrder ??
      defaultSidebarViewOrder(
        views,
        userId,
        pins.filter((pin) => pin.item_type === "view").map((pin) => pin.item_id),
      ),
    [savedOrder, views, userId, pins],
  );
  const items = useMemo(() => resolveSidebarViewItems(order, views), [order, views]);

  // A write replaces the whole stored order, so it waits until that order is
  // known: the saved one, or the default derived from views and pins.
  const ready =
    preferenceQuery.isSuccess &&
    (savedOrder !== null || (viewsQuery.isSuccess && (!userId || pinsQuery.isSuccess)));
  const write = useCallback(
    (next: string[]) => {
      if (ready) save({ hidden: [], order: next });
    },
    [ready, save],
  );

  return {
    items,
    /** Saved views are still loading; built-ins render regardless. */
    viewsPending: viewsQuery.isPending,
    isStarred: useCallback((id: string) => order.includes(id), [order]),
    star: useCallback((id: string) => {
      if (!order.includes(id)) write([...order, id]);
    }, [order, write]),
    unstar: useCallback((id: string) => write(order.filter((other) => other !== id)), [order, write]),
    /** Takes the visible items' ids in their new order. */
    reorder: useCallback((visibleIds: string[]) => write(reorderSidebarViewOrder(order, visibleIds)), [order, write]),
  };
}
