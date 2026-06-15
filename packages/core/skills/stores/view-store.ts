"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

// View preferences for the skills list page: sort, column visibility, and
// filters. Persisted per workspace (workspace-aware storage), per user/device
// (localStorage). Search text and row selection are deliberately NOT stored —
// they are session-scoped, and persisting them would greet returning users
// with an inexplicably narrowed list.

export type SkillSortField = "name" | "usedBy" | "updated" | "created";

export type SkillSortDirection = "asc" | "desc";

/** Per-field direction applied when the user switches TO that field. */
export const SKILL_SORT_DEFAULT_DIRECTION: Record<
  SkillSortField,
  SkillSortDirection
> = {
  name: "asc",
  usedBy: "desc",
  updated: "desc",
  created: "desc",
};

export type SkillOriginType =
  | "manual"
  | "runtime_local"
  | "clawhub"
  | "skills_sh"
  | "github";

/** Multi-select filter state. Empty array per dimension = inactive. */
export interface SkillListFilters {
  usage: ("used" | "unused")[];
  origins: SkillOriginType[];
  agents: string[];
  creators: string[];
}

export const EMPTY_SKILL_FILTERS: SkillListFilters = {
  usage: [],
  origins: [],
  agents: [],
  creators: [],
};

// User-hideable columns. Name and the structural columns (checkbox, kebab)
// are always visible.
export type SkillColumnKey =
  | "usedBy"
  | "source"
  | "creator"
  | "updated"
  | "created";

/** Source and created are opt-in: hidden until the user enables them. */
export const DEFAULT_HIDDEN_COLUMNS: SkillColumnKey[] = ["source", "created"];

export interface SkillsViewState {
  sortField: SkillSortField;
  sortDirection: SkillSortDirection;
  hiddenColumns: SkillColumnKey[];
  filters: SkillListFilters;
  /** Header click: toggles direction on the active field, otherwise switches
   *  to the field with its default direction. */
  toggleSort: (field: SkillSortField) => void;
  /** Display panel select: switches field (default direction), no toggle. */
  setSortField: (field: SkillSortField) => void;
  setSortDirection: (direction: SkillSortDirection) => void;
  toggleColumn: (key: SkillColumnKey) => void;
  toggleFilter: (key: keyof SkillListFilters, value: string) => void;
  clearFilters: () => void;
}

const DEFAULTS = {
  sortField: "updated" as SkillSortField,
  sortDirection: SKILL_SORT_DEFAULT_DIRECTION.updated,
  hiddenColumns: DEFAULT_HIDDEN_COLUMNS,
  filters: EMPTY_SKILL_FILTERS,
};

export const useSkillsViewStore = create<SkillsViewState>()(
  persist(
    (set) => ({
      ...DEFAULTS,
      toggleSort: (field) =>
        set((state) =>
          state.sortField === field
            ? {
                sortDirection: state.sortDirection === "asc" ? "desc" : "asc",
              }
            : {
                sortField: field,
                sortDirection: SKILL_SORT_DEFAULT_DIRECTION[field],
              },
        ),
      setSortField: (field) =>
        set((state) =>
          state.sortField === field
            ? {}
            : {
                sortField: field,
                sortDirection: SKILL_SORT_DEFAULT_DIRECTION[field],
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
      clearFilters: () => set({ filters: EMPTY_SKILL_FILTERS }),
    }),
    {
      name: "multica_skills_view",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({
        sortField: state.sortField,
        sortDirection: state.sortDirection,
        hiddenColumns: state.hiddenColumns,
        filters: state.filters,
      }),
      // On rehydrate, if the new workspace has no persisted value, reset to
      // the defaults instead of leaving the previous workspace's in-memory
      // view state in place (same rationale as the agents view store).
      merge: (persisted, current) => {
        if (!persisted) return { ...current, ...DEFAULTS };
        const p = persisted as Partial<SkillsViewState>;
        // Deep-merge filters so a payload persisted before a new filter
        // dimension existed still gets that key's default instead of
        // dropping it to undefined (which crashes `.length` reads).
        return {
          ...current,
          ...p,
          filters: { ...EMPTY_SKILL_FILTERS, ...(p.filters ?? {}) },
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useSkillsViewStore.persist.rehydrate());
