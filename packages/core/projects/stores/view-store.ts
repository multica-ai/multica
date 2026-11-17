"use client";

import type { ProjectLifecycleView } from "../lifecycle";

import {
  createCollectionViewStore,
  type CollectionSortDirection,
  type CollectionViewCore,
} from "../../collections/view-store";

export type ProjectScope = ProjectLifecycleView | "archived";

// Projects has two layouts: a table and a board. Sort, filters, search and
// grouping feed both; hiddenColumns only applies to the table. In the table
// grouping splits the rows into sections; on the board it picks the columns,
// so the board always has one (status unless another is chosen). Lifecycle
// scope and explicit status filters share a preference. Search stays session-local.
export type ProjectLayout = "table" | "board";

export type ProjectGroupBy = "none" | "status" | "priority" | "lead";

export type ProjectSortField =
  | "name"
  | "priority"
  | "status"
  | "progress"
  | "start_date"
  | "due_date"
  | "last_activity"
  | "created";

export type ProjectSortDirection = CollectionSortDirection;

export const PROJECT_SORT_DEFAULT_DIRECTION: Record<
  ProjectSortField,
  ProjectSortDirection
> = {
  name: "asc",
  priority: "desc",
  status: "asc",
  progress: "desc",
  start_date: "asc",
  due_date: "asc",
  last_activity: "desc",
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
export type ProjectColumnKey =
  | "priority"
  | "progress"
  | "lead"
  | "start_date"
  | "due_date"
  | "last_activity"
  | "issues"
  | "created";

export const PROJECT_COLUMN_KEYS: readonly ProjectColumnKey[] = [
  "priority",
  "progress",
  "lead",
  "start_date",
  "due_date",
  "last_activity",
  "issues",
  "created",
];

export const PROJECT_GROUP_BY_VALUES: readonly ProjectGroupBy[] = ["none", "status", "priority", "lead"];

/** Issues count and start date are opt-in; the rest show by default. */
export const PROJECT_DEFAULT_HIDDEN_COLUMNS: ProjectColumnKey[] = ["issues", "start_date"];

/** The grouping the board shows: it has no ungrouped form. */
export function boardGroupBy(groupBy: ProjectGroupBy): Exclude<ProjectGroupBy, "none"> {
  return groupBy === "none" ? "status" : groupBy;
}

export interface ProjectViewState
  extends CollectionViewCore<
    ProjectSortField,
    ProjectColumnKey,
    ProjectListFilters
  > {
  lifecycleView: ProjectScope;
  setLifecycleView: (view: ProjectScope) => void;
  layout: ProjectLayout;
  groupBy: ProjectGroupBy;
  setLayout: (layout: ProjectLayout) => void;
  setGroupBy: (groupBy: ProjectGroupBy) => void;
}

/**
 * Version 0 stored `viewMode` ("compact" table, "comfortable" cards). The
 * board replaced the cards, so a saved card view opens as the board. Start
 * date became a column after those preferences were saved; it starts hidden
 * for them too, as it does for everyone else.
 */
export function migrateProjectViewState(persisted: unknown, version: number): unknown {
  if (version >= 2 || !persisted || typeof persisted !== "object") return persisted;
  const { viewMode, ...rest } = persisted as Omit<Partial<ProjectViewState>, "lifecycleView"> & {
    viewMode?: unknown;
    lifecycleView?: string;
  };
  const hidden = Array.isArray(rest.hiddenColumns) ? rest.hiddenColumns : null;
  const layout = rest.layout ?? (viewMode === "comfortable" ? "board" : "table");
  const lifecycleView = rest.lifecycleView === "active" || rest.lifecycleView === "all" || rest.lifecycleView === "archived"
    ? rest.lifecycleView
    : rest.lifecycleView === "closed" || rest.lifecycleView === "completed"
      ? "closed"
      : rest.filters?.statuses?.length ? "all" : "active";
  return {
    ...rest,
    layout,
    lifecycleView,
    ...(hidden && (version === 0 || viewMode !== undefined)
      ? { hiddenColumns: hidden.includes("start_date") ? hidden : [...hidden, "start_date"] }
      : {}),
  };
}

export const useProjectViewStore = createCollectionViewStore<
  ProjectSortField,
  ProjectColumnKey,
  ProjectListFilters,
  { layout: ProjectLayout; groupBy: ProjectGroupBy; lifecycleView: ProjectScope },
  {
    setLifecycleView: (view: ProjectScope) => void;
    setLayout: (layout: ProjectLayout) => void;
    setGroupBy: (groupBy: ProjectGroupBy) => void;
  }
>({
  name: "multica_projects_view",
  version: 2,
  migrate: migrateProjectViewState,
  defaultSortField: "last_activity",
  sortDefaultDirection: PROJECT_SORT_DEFAULT_DIRECTION,
  defaultHiddenColumns: PROJECT_DEFAULT_HIDDEN_COLUMNS,
  emptyFilters: EMPTY_PROJECT_FILTERS,
  extraState: { layout: "table", groupBy: "none", lifecycleView: "active" },
  onToggleFilter: (_state, key) => key === "statuses" ? { lifecycleView: "all" } : {},
  extraActions: (set) => ({
    setLifecycleView: (lifecycleView) => set((state) => ({
      lifecycleView,
      filters: { ...state.filters, statuses: [] },
    })),
    setLayout: (layout) => set({ layout }),
    setGroupBy: (groupBy) => set({ groupBy }),
  }),
});
