"use client";

import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { IssueViewPreference } from "../api/schemas";
import type { IssueViewScope } from "./queries";

export interface ViewBarPrefs {
  hidden: string[];
  order: string[];
}

export const EMPTY_VIEW_BAR_PREFS: ViewBarPrefs = { hidden: [], order: [] };

export const issueViewPrefKeys = {
  all: (wsId: string) => ["issue-view-prefs", wsId] as const,
  scope: (wsId: string, scope: IssueViewScope) =>
    [...issueViewPrefKeys.all(wsId), scope.scope_type, scope.scope_id ?? null] as const,
};

export function issueViewPreferenceOptions(wsId: string, scope: IssueViewScope) {
  return queryOptions({
    queryKey: issueViewPrefKeys.scope(wsId, scope),
    queryFn: () => api.getIssueViewPreference(scope),
    enabled: !!wsId,
  });
}

/**
 * Whole-document upsert with an optimistic patch: show/hide toggles and
 * drag-reorder are the canonical optimistic case (locally predictable, same
 * screen, trivial rollback).
 */
export function useUpdateIssueViewPreference(wsId: string, scope: IssueViewScope) {
  const queryClient = useQueryClient();
  const queryKey = issueViewPrefKeys.scope(wsId, scope);
  return useMutation({
    mutationFn: (prefs: ViewBarPrefs) =>
      api.putIssueViewPreference({ ...scope, prefs }),
    onMutate: async (prefs) => {
      await queryClient.cancelQueries({ queryKey });
      const previous = queryClient.getQueryData<IssueViewPreference>(queryKey);
      const optimistic: IssueViewPreference = {
        scope_type: scope.scope_type,
        scope_id: scope.scope_id ?? null,
        updated_at: previous?.updated_at ?? "",
        prefs: { ...prefs },
      };
      queryClient.setQueryData<IssueViewPreference>(queryKey, optimistic);
      return { previous };
    },
    onError: (_err, _prefs, context) => {
      if (context?.previous) queryClient.setQueryData(queryKey, context.previous);
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey });
    },
  });
}

/**
 * Compose the view bar: apply the user's order, drop hidden items, keep the
 * anchor built-in ("builtin:" first item) always visible so nobody locks
 * themselves out of a surface. Unknown ids in prefs (deleted views) are
 * ignored; items absent from `order` append in their natural position.
 */
export function applyViewBarPrefs<T extends { barItemId: string }>(
  items: T[],
  prefs: ViewBarPrefs | undefined,
  anchorId: string,
): { visible: T[]; hiddenSet: Set<string>; ordered: T[] } {
  const order = prefs?.order ?? [];
  const hiddenSet = new Set(prefs?.hidden ?? []);
  hiddenSet.delete(anchorId);

  const byId = new Map(items.map((item) => [item.barItemId, item]));
  const ordered: T[] = [];
  for (const id of order) {
    const item = byId.get(id);
    if (item) {
      ordered.push(item);
      byId.delete(id);
    }
  }
  for (const item of items) {
    if (byId.has(item.barItemId)) ordered.push(item);
  }

  return {
    ordered,
    hiddenSet,
    visible: ordered.filter((item) => !hiddenSet.has(item.barItemId)),
  };
}
