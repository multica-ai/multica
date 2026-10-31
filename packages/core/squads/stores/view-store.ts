"use client";

import {
  createCollectionViewStore,
  type CollectionSortDirection,
  type CollectionViewCore,
} from "../../collections/view-store";

// View preferences for the squads list page: scope, sort, column visibility.
// Persisted per workspace, per user/device. No filters (the set is tiny);
// no search (scope-bearing list). Mirrors the agents/skills view stores.

// Scope is the ownership lens (creator-based). No "archived" scope: the
// list endpoint hard-filters archived squads and there is no restore
// endpoint, so archived squads can't be surfaced or managed.
export type SquadsScope = "mine" | "all";

export const SQUAD_SCOPES: SquadsScope[] = ["mine", "all"];

export type SquadSortField = "name" | "members" | "created";

export type SquadSortDirection = CollectionSortDirection;

/** Per-field direction applied when the user switches TO that field. */
export const SQUAD_SORT_DEFAULT_DIRECTION: Record<
  SquadSortField,
  SquadSortDirection
> = {
  name: "asc",
  members: "desc",
  created: "desc",
};

// User-hideable columns. Name and leader (the squad's defining relationship)
// are always visible.
export type SquadColumnKey = "members" | "creator" | "created";

/** Created (date) is opt-in. Creator ("Created by") is shown by default —
 *  the user wants to see who made each squad. Note it's "Created by", NOT
 *  "Owner": the squad creator holds no management rights (archiving is
 *  workspace-admin only), so labelling it Owner would mislead. */
export const SQUAD_DEFAULT_HIDDEN_COLUMNS: SquadColumnKey[] = ["created"];

/** Multi-select filters — the categorical columns (leader, creator). Empty
 *  array per dimension = inactive. */
export interface SquadListFilters {
  /** Leader agent ids. */
  leaders: string[];
  /** Creator member user ids. */
  creators: string[];
}

export const EMPTY_SQUAD_FILTERS: SquadListFilters = {
  leaders: [],
  creators: [],
};

export interface SquadsViewState
  extends CollectionViewCore<SquadSortField, SquadColumnKey, SquadListFilters> {
  scope: SquadsScope;
  setScope: (scope: SquadsScope) => void;
}

export const useSquadsViewStore = createCollectionViewStore<
  SquadSortField,
  SquadColumnKey,
  SquadListFilters,
  { scope: SquadsScope },
  { setScope: (scope: SquadsScope) => void }
>({
  name: "multica_squads_view",
  defaultSortField: "name",
  sortDefaultDirection: SQUAD_SORT_DEFAULT_DIRECTION,
  defaultHiddenColumns: SQUAD_DEFAULT_HIDDEN_COLUMNS,
  emptyFilters: EMPTY_SQUAD_FILTERS,
  extraState: { scope: "mine" },
  extraActions: (set) => ({ setScope: (scope) => set({ scope }) }),
});
