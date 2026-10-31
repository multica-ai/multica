"use client";

import type { ProjectLifecycleView } from "../lifecycle";

import {
  createCollectionViewStore,
  type CollectionSortDirection,
  type CollectionViewCore,
} from "../../collections/view-store";

// Projects is the one dual-view list: a dense table (compact) and a card
// grid (comfortable), toggled by viewMode. Sort + filters feed both views;
// hiddenColumns only applies to the table. Lifecycle scopes and explicit
// status filters share one persisted preference. Search stays session-local.
export type ProjectViewMode = "compact" | "comfortable";

export type ProjectSortField = "name" | "priority" | "status" | "progress" | "created";

export type ProjectSortDirection = CollectionSortDirection;

export const PROJECT_SORT_DEFAULT_DIRECTION: Record<
  ProjectSortField,
  ProjectSortDirection
> = {
  name: "asc",
  priority: "desc",
  status: "asc",
  progress: "desc",
  created: "desc",
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

export interface ProjectViewState
  extends CollectionViewCore<
    ProjectSortField,
    ProjectColumnKey,
    ProjectListFilters
  > {
  lifecycleView: ProjectLifecycleView;
  setLifecycleView: (view: ProjectLifecycleView) => void;
  viewMode: ProjectViewMode;
  setViewMode: (mode: ProjectViewMode) => void;
}

export const useProjectViewStore = createCollectionViewStore<
  ProjectSortField,
  ProjectColumnKey,
  ProjectListFilters,
  { viewMode: ProjectViewMode; lifecycleView: ProjectLifecycleView },
  { setViewMode: (mode: ProjectViewMode) => void; setLifecycleView: (view: ProjectLifecycleView) => void }
>({
  name: "multica_projects_view",
  version: 1,
  migrate: (persisted) => {
    const p = persisted as Omit<Partial<ProjectViewState>, "lifecycleView"> & { lifecycleView?: string };
    return {
      ...p,
      lifecycleView: p.lifecycleView === "active" || p.lifecycleView === "all"
        ? p.lifecycleView
        : p.lifecycleView === "closed" || p.lifecycleView === "completed"
          ? "closed"
          : p.filters?.statuses?.length ? "all" : "active",
    };
  },
  defaultSortField: "created",
  sortDefaultDirection: PROJECT_SORT_DEFAULT_DIRECTION,
  defaultHiddenColumns: PROJECT_DEFAULT_HIDDEN_COLUMNS,
  emptyFilters: EMPTY_PROJECT_FILTERS,
  extraState: { viewMode: "compact", lifecycleView: "active" },
  onToggleFilter: (_state, key) => key === "statuses" ? { lifecycleView: "all" } : {},
  extraActions: (set) => ({
    setViewMode: (mode) => set({ viewMode: mode }),
    setLifecycleView: (lifecycleView) => set((state) => ({
      lifecycleView,
      filters: { ...state.filters, statuses: [] },
    })),
  }),
});
