"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

// View preferences for the squads list page: scope, sort, column visibility.
// Persisted per workspace, per user/device. No filters (the set is tiny);
// no search (scope-bearing list). Mirrors the agents/skills view stores.

// Scope is the ownership lens (creator-based). No "archived" scope: the
// list endpoint hard-filters archived squads and there is no restore
// endpoint, so archived squads can't be surfaced or managed.
export type SquadsScope = "mine" | "all";

export const SQUAD_SCOPES: SquadsScope[] = ["mine", "all"];

export type SquadSortField = "name" | "members" | "created";

export type SquadSortDirection = "asc" | "desc";

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

export interface SquadsViewState {
  scope: SquadsScope;
  sortField: SquadSortField;
  sortDirection: SquadSortDirection;
  hiddenColumns: SquadColumnKey[];
  filters: SquadListFilters;
  setScope: (scope: SquadsScope) => void;
  /** Header click: toggles direction on the active field, otherwise switches
   *  to the field with its default direction. */
  toggleSort: (field: SquadSortField) => void;
  /** Display panel select: switches field (default direction), no toggle. */
  setSortField: (field: SquadSortField) => void;
  setSortDirection: (direction: SquadSortDirection) => void;
  toggleColumn: (key: SquadColumnKey) => void;
  toggleFilter: (key: keyof SquadListFilters, value: string) => void;
  clearFilters: () => void;
}

const DEFAULTS = {
  scope: "mine" as SquadsScope,
  sortField: "name" as SquadSortField,
  sortDirection: SQUAD_SORT_DEFAULT_DIRECTION.name,
  hiddenColumns: SQUAD_DEFAULT_HIDDEN_COLUMNS,
  filters: EMPTY_SQUAD_FILTERS,
};

export const useSquadsViewStore = create<SquadsViewState>()(
  persist(
    (set) => ({
      ...DEFAULTS,
      setScope: (scope) => set({ scope }),
      toggleSort: (field) =>
        set((state) =>
          state.sortField === field
            ? {
                sortDirection: state.sortDirection === "asc" ? "desc" : "asc",
              }
            : {
                sortField: field,
                sortDirection: SQUAD_SORT_DEFAULT_DIRECTION[field],
              },
        ),
      setSortField: (field) =>
        set((state) =>
          state.sortField === field
            ? {}
            : {
                sortField: field,
                sortDirection: SQUAD_SORT_DEFAULT_DIRECTION[field],
              },
        ),
      setSortDirection: (direction) => set({ sortDirection: direction }),
      toggleColumn: (key) =>
        set((state) => ({
          hiddenColumns: state.hiddenColumns.includes(key)
            ? state.hiddenColumns.filter((k) => k !== key)
            : [...state.hiddenColumns, key],
        })),
      toggleFilter: (key, value) =>
        set((state) => {
          const list = state.filters[key] as string[];
          const next = list.includes(value)
            ? list.filter((v) => v !== value)
            : [...list, value];
          return { filters: { ...state.filters, [key]: next } };
        }),
      clearFilters: () => set({ filters: EMPTY_SQUAD_FILTERS }),
    }),
    {
      name: "multica_squads_view",
      storage: createJSONStorage(() =>
        createWorkspaceAwareStorage(defaultStorage),
      ),
      partialize: (state) => ({
        scope: state.scope,
        sortField: state.sortField,
        sortDirection: state.sortDirection,
        hiddenColumns: state.hiddenColumns,
        filters: state.filters,
      }),
      // On rehydrate, if the new workspace has no persisted value, reset to
      // the defaults instead of leaking the previous workspace's state.
      // Deep-merge filters so a pre-filters payload backfills defaults.
      merge: (persisted, current) => {
        if (!persisted) return { ...current, ...DEFAULTS };
        const p = persisted as Partial<SquadsViewState>;
        return {
          ...current,
          ...p,
          filters: { ...EMPTY_SQUAD_FILTERS, ...(p.filters ?? {}) },
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useSquadsViewStore.persist.rehydrate());
