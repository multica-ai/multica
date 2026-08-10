"use client";

import { create } from "zustand";
import type { FilterDimension } from "../issues/stores/view-store";
import type { IssueViewScope } from "./queries";

/**
 * Which saved view is open on each surface container, keyed by
 * `${wsId}:${scope_type}[:${scope_id}]`. Client/view state (zustand per the
 * state rules): NOT persisted — on web the URL `?view=` param is the durable
 * carrier, and resurrecting a deleted view from storage would strand users.
 */
export function issueViewContainerKey(wsId: string, scope: IssueViewScope): string {
  return scope.scope_id
    ? `${wsId}:${scope.scope_type}:${scope.scope_id}`
    : `${wsId}:${scope.scope_type}`;
}

interface ActiveIssueViewState {
  active: Record<string, string>;
  setActive: (containerKey: string, viewId: string | null) => void;
}

export const useActiveIssueViewStore = create<ActiveIssueViewState>((set) => ({
  active: {},
  setActive: (containerKey, viewId) =>
    set((state) => {
      const active = { ...state.active };
      if (viewId) active[containerKey] = viewId;
      else delete active[containerKey];
      return { active };
    }),
}));

/**
 * Dimensions fixed by a view's saved query. These render as locked chips
 * (no remove affordance) and their sections are disabled in the filter menu
 * — the view's identity cannot be edited in place, only narrowed around.
 */
export function lockedDimensionsFromQuery(
  query: Record<string, unknown>,
): Set<FilterDimension> {
  const locked = new Set<FilterDimension>();
  const nonEmptyArray = (v: unknown) => Array.isArray(v) && v.length > 0;
  if (nonEmptyArray(query.statusFilters)) locked.add("status");
  if (nonEmptyArray(query.priorityFilters)) locked.add("priority");
  if (nonEmptyArray(query.assigneeFilters) || query.includeNoAssignee === true) {
    locked.add("assignee");
  }
  if (nonEmptyArray(query.creatorFilters)) locked.add("creator");
  if (nonEmptyArray(query.projectFilters) || query.includeNoProject === true) {
    locked.add("project");
  }
  if (nonEmptyArray(query.labelFilters)) locked.add("label");
  const propertyFilters = query.propertyFilters;
  if (propertyFilters && typeof propertyFilters === "object") {
    for (const [id, selected] of Object.entries(propertyFilters as Record<string, unknown>)) {
      if (nonEmptyArray(selected)) locked.add(`property:${id}`);
    }
  }
  return locked;
}
