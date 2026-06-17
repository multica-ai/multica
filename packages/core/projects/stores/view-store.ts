"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

// Projects is the one dual-view list: a dense table (compact) and a card
// grid (comfortable), toggled by viewMode. Sort + filters feed both views;
// hiddenColumns only applies to the table. No scope (lead is optional and
// often an agent, so there's no strong personal axis; status is a 5-value
// lifecycle better expressed as a filter). Search stays session-local.
export type ProjectViewMode = "compact" | "comfortable";

export type ProjectSortField =
  | "name"
  | "priority"
  | "status"
  | "progress"
  | "created"
  | "created_at"
  | "updated_at"
  | "title";

export type ProjectSortDirection = "asc" | "desc";

export const PROJECT_SORT_OPTIONS: { value: ProjectSortField; label: string }[] = [
  { value: "priority", label: "Priority" },
  { value: "status", label: "Status" },
  { value: "created_at", label: "Created date" },
  { value: "updated_at", label: "Updated date" },
  { value: "title", label: "Title" },
];

export const PROJECT_SORT_DEFAULT_DIRECTION: Record<
  ProjectSortField,
  ProjectSortDirection
> = {
  name: "asc",
  priority: "desc",
  status: "asc",
  progress: "desc",
  created: "desc",
  created_at: "desc",
  updated_at: "desc",
  title: "asc",
};

/** Multi-select filters. Empty array per dimension = inactive. */
export interface ProjectListFilters {
  /** ProjectStatus values. */
  statuses: string[];
  /** ProjectPriority values. */
  priorities: string[];
  /** Composite "type:id" lead refs (member or agent). */
  leads: string[];
}

export const EMPTY_PROJECT_FILTERS: ProjectListFilters = {
  statuses: [],
  priorities: [],
  leads: [],
};

// Hideable table columns. Name + status are the always-visible core (status
// is the project's defining lifecycle field), so they're not in this set.
export type ProjectColumnKey = "priority" | "progress" | "lead" | "issues" | "created";

/** Issues count is opt-in; the rest show by default (matching the prior
 *  compact table). */
export const PROJECT_DEFAULT_HIDDEN_COLUMNS: ProjectColumnKey[] = ["issues"];

export interface ProjectViewState {
  /** Compatibility aliases for the previous Projects page implementation. */
  sortBy: ProjectSortField;
  setSortBy: (field: ProjectSortField) => void;
  viewMode: ProjectViewMode;
  sortField: ProjectSortField;
  sortDirection: ProjectSortDirection;
  hiddenColumns: ProjectColumnKey[];
  filters: ProjectListFilters;
  setViewMode: (mode: ProjectViewMode) => void;
  toggleSort: (field: ProjectSortField) => void;
  setSortField: (field: ProjectSortField) => void;
  setSortDirection: (direction: ProjectSortDirection) => void;
  toggleColumn: (key: ProjectColumnKey) => void;
  toggleFilter: (key: keyof ProjectListFilters, value: string) => void;
  clearFilters: () => void;
}

const DEFAULTS = {
  viewMode: "compact" as ProjectViewMode,
  sortField: "created" as ProjectSortField,
  sortDirection: PROJECT_SORT_DEFAULT_DIRECTION.created,
  hiddenColumns: PROJECT_DEFAULT_HIDDEN_COLUMNS,
  filters: EMPTY_PROJECT_FILTERS,
};

export const useProjectViewStore = create<ProjectViewState>()(
  persist(
    (set) => ({
      ...DEFAULTS,
      sortBy: DEFAULTS.sortField,
      setSortBy: (field) => set({ sortBy: field, sortField: field }),
      setViewMode: (mode) => set({ viewMode: mode }),
      toggleSort: (field) =>
        set((state) =>
          state.sortField === field
            ? { sortDirection: state.sortDirection === "asc" ? "desc" : "asc" }
            : {
                sortField: field,
                sortBy: field,
                sortDirection: PROJECT_SORT_DEFAULT_DIRECTION[field],
              },
        ),
      setSortField: (field) =>
        set((state) =>
          state.sortField === field
            ? {}
            : {
                sortField: field,
                sortBy: field,
                sortDirection: PROJECT_SORT_DEFAULT_DIRECTION[field],
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
      clearFilters: () => set({ filters: EMPTY_PROJECT_FILTERS }),
    }),
    {
      name: "multica_projects_view",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({
        sortBy: state.sortBy,
        viewMode: state.viewMode,
        sortField: state.sortField,
        sortDirection: state.sortDirection,
        hiddenColumns: state.hiddenColumns,
        filters: state.filters,
      }),
      // Deep-merge filters so a payload persisted before a filter dimension
      // existed still gets that key's default (avoids `.length` on
      // undefined). Same hardening as the other view stores.
      merge: (persisted, current) => {
        if (!persisted) return { ...current, ...DEFAULTS };
        const p = persisted as Partial<ProjectViewState>;
        const sortField = p.sortField ?? p.sortBy ?? current.sortField;
        return {
          ...current,
          ...p,
          sortField,
          sortBy: sortField,
          filters: { ...EMPTY_PROJECT_FILTERS, ...(p.filters ?? {}) },
        };
      },
    }
  )
);

registerForWorkspaceRehydration(() => useProjectViewStore.persist.rehydrate());
