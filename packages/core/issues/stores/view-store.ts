"use client";

import { useEffect, useRef } from "react";
import { create } from "zustand";
import { createStore, type StoreApi } from "zustand/vanilla";
import { createJSONStorage, persist } from "zustand/middleware";
import type { IssueStatus, IssuePriority } from "../../types";
import { ALL_STATUSES } from "../config";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

export type ViewMode = "board" | "list" | "table" | "gantt" | "swimlane";
export type GanttZoom = "day" | "week" | "month";
/**
 * Board grouping. Besides the two built-ins, a select-type custom property
 * groups columns by its options via the `property:<definitionId>` form.
 * Persisted values may reference a since-archived definition — consumers must
 * fall back to "status" when the definition can't be resolved.
 */
export type IssueGrouping = "status" | "assignee" | `property:${string}`;
export type SwimlaneGrouping = "parent" | "project" | "assignee";
/**
 * Sort key. `property:<definitionId>` is resolved server-side against the
 * active property catalog; stale or unsupported definitions degrade to
 * position order.
 */
export type SortField =
  | "position"
  | "status"
  | "priority"
  | "start_date"
  | "due_date"
  | "created_at"
  | "updated_at"
  | "title"
  | `property:${string}`;
export type SortDirection = "asc" | "desc";
export type IssueDateField = "created_at" | "updated_at";

export type TableSystemColumnKey =
  | "title"
  | "identifier"
  | "status"
  | "priority"
  | "assignee"
  | "labels"
  | "project"
  | "start_date"
  | "due_date"
  | "created_at"
  | "updated_at"
  | "child_progress"
  | "creator";
export type TableColumnKey = TableSystemColumnKey | `property:${string}`;
export interface TableColumnConfig {
  key: TableColumnKey;
  width?: number;
}
export type TableGrouping = "none" | "status" | "assignee" | `property:${string}`;
export type TableCalculation = "none" | "sum" | "average" | "count";

export const TABLE_SYSTEM_COLUMNS: readonly TableSystemColumnKey[] = [
  "title",
  "identifier",
  "status",
  "priority",
  "assignee",
  "labels",
  "project",
  "start_date",
  "due_date",
  "created_at",
  "updated_at",
  "child_progress",
  "creator",
];

export const DEFAULT_TABLE_COLUMNS: readonly TableColumnConfig[] = [
  { key: "title", width: 360 },
  { key: "status", width: 150 },
  { key: "priority", width: 130 },
  { key: "assignee", width: 180 },
  { key: "due_date", width: 140 },
  { key: "labels", width: 220 },
];

export interface IssueDateFilter {
  field: IssueDateField;
  from: string;
  to: string;
}

export const SWIMLANE_GROUPINGS: SwimlaneGrouping[] = ["parent", "project", "assignee"];

export interface CardProperties {
  priority: boolean;
  description: boolean;
  assignee: boolean;
  startDate: boolean;
  dueDate: boolean;
  project: boolean;
  childProgress: boolean;
  labels: boolean;
}

export interface ActorFilterValue {
  type: "member" | "agent" | "squad";
  id: string;
}

export const PROPERTY_VIEW_PREFIX = "property:";

export function propertyIdFromViewKey(key: string): string | null {
  return key.startsWith(PROPERTY_VIEW_PREFIX) ? key.slice(PROPERTY_VIEW_PREFIX.length) : null;
}

export type StaticSortField = Exclude<SortField, `property:${string}`>;
export type StaticIssueGrouping = Exclude<IssueGrouping, `property:${string}`>;

export const SORT_OPTIONS: { value: StaticSortField; label: string }[] = [
  { value: "position", label: "Manual" },
  { value: "status", label: "Status" },
  { value: "priority", label: "Priority" },
  { value: "start_date", label: "Start date" },
  { value: "due_date", label: "Due date" },
  { value: "created_at", label: "Created date" },
  { value: "updated_at", label: "Updated date" },
  { value: "title", label: "Title" },
];

export const GROUPING_OPTIONS: { value: StaticIssueGrouping; label: string }[] = [
  { value: "status", label: "Status" },
  { value: "assignee", label: "Assignee" },
];

export const CARD_PROPERTY_OPTIONS: { key: keyof CardProperties; label: string }[] = [
  { key: "priority", label: "Priority" },
  { key: "description", label: "Description" },
  { key: "assignee", label: "Assignee" },
  { key: "startDate", label: "Start date" },
  { key: "dueDate", label: "Due date" },
  { key: "project", label: "Project" },
  { key: "labels", label: "Labels" },
  { key: "childProgress", label: "Sub-issue progress" },
];

export interface IssueViewState {
  viewMode: ViewMode;
  grouping: IssueGrouping;
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
  /**
   * Custom-property filters: definition id → selected option ids (checkbox
   * definitions use the pseudo-options "true"/"false"). Empty array = no
   * filter for that definition; matching is OR within a definition and AND
   * across definitions, mirroring the other filter groups.
   */
  propertyFilters: Record<string, string[]>;
  dateFilter: IssueDateFilter | null;
  // When true, the list only shows issues that currently have at least one
  // agent task in `running` status. Drives the workspace "agents working"
  // quick filter chip in the issues header. Not persisted across reloads —
  // running state changes second-to-second, a persisted toggle would let
  // users return to an empty list with no obvious cause.
  agentRunningFilter: boolean;
  sortBy: SortField;
  sortDirection: SortDirection;
  cardProperties: CardProperties;
  /** Custom property definition ids whose values render on board/list cards. */
  cardPropertyIds: string[];
  // When false, issues that have a parent (sub-issues) are hidden from the
  // board / list / swimlane so users can focus on top-level parent issues.
  // Purely a display filter — it never touches the parent/child relationship.
  showSubIssues: boolean;
  listCollapsedStatuses: IssueStatus[];
  ganttZoom: GanttZoom;
  ganttShowCompleted: boolean;
  /** Active swimlane grouping dimension. */
  swimlaneGrouping: SwimlaneGrouping;
  /** Persisted lane order, keyed by grouping. Entries are raw lane ids
   *  (parent issue id, project id, or `<assigneeType>:<assigneeId>`). */
  swimlaneOrders: Record<SwimlaneGrouping, string[]>;
  /** Persisted collapsed lanes, keyed by grouping. Same id space as
   *  `swimlaneOrders`, plus the sentinel `"none"` for the pinned
   *  no-X lane and `"__orphans__"` for the parent-grouping fallback. */
  collapsedSwimlanes: Record<SwimlaneGrouping, string[]>;
  /** Ordered table columns. Title is mandatory and normalized to the front. */
  tableColumns: TableColumnConfig[];
  tableGrouping: TableGrouping;
  tableCollapsedGroups: string[];
  tableCollapsedParents: string[];
  tableHierarchy: boolean;
  tableCalculation: TableCalculation;
  setViewMode: (mode: ViewMode) => void;
  setGanttZoom: (zoom: GanttZoom) => void;
  toggleGanttShowCompleted: () => void;
  setGrouping: (grouping: IssueGrouping) => void;
  toggleStatusFilter: (status: IssueStatus) => void;
  togglePriorityFilter: (priority: IssuePriority) => void;
  toggleAssigneeFilter: (value: ActorFilterValue) => void;
  toggleNoAssignee: () => void;
  toggleCreatorFilter: (value: ActorFilterValue) => void;
  toggleProjectFilter: (projectId: string) => void;
  toggleNoProject: () => void;
  toggleLabelFilter: (labelId: string) => void;
  togglePropertyFilter: (propertyId: string, optionId: string) => void;
  setDateFilter: (filter: IssueDateFilter | null) => void;
  toggleAgentRunningFilter: () => void;
  hideStatus: (status: IssueStatus) => void;
  showStatus: (status: IssueStatus) => void;
  clearFilters: () => void;
  setSortBy: (field: SortField) => void;
  setSortDirection: (dir: SortDirection) => void;
  toggleCardProperty: (key: keyof CardProperties) => void;
  toggleCardPropertyId: (propertyId: string) => void;
  toggleShowSubIssues: () => void;
  toggleListCollapsed: (status: IssueStatus) => void;
  setSwimlaneGrouping: (grouping: SwimlaneGrouping) => void;
  /** Update the lane order for the currently active swimlane grouping. */
  setSwimlaneOrder: (order: string[]) => void;
  /** Toggle a lane key in the currently active swimlane grouping. */
  toggleSwimlaneCollapsed: (key: string) => void;
  toggleTableColumn: (key: TableColumnKey) => void;
  reorderTableColumn: (active: TableColumnKey, over: TableColumnKey) => void;
  setTableColumnWidth: (key: TableColumnKey, width?: number) => void;
  setTableGrouping: (grouping: TableGrouping) => void;
  toggleTableGroupCollapsed: (key: string) => void;
  toggleTableParentCollapsed: (issueId: string) => void;
  toggleTableHierarchy: () => void;
  setTableCalculation: (calculation: TableCalculation) => void;
}

export const viewStoreSlice = (set: StoreApi<IssueViewState>["setState"]): IssueViewState => ({
  viewMode: "board",
  grouping: "status",
  statusFilters: [],
  priorityFilters: [],
  assigneeFilters: [],
  includeNoAssignee: false,
  creatorFilters: [],
  projectFilters: [],
  includeNoProject: false,
  labelFilters: [],
  propertyFilters: {},
  dateFilter: null,
  agentRunningFilter: false,
  sortBy: "position",
  sortDirection: "asc",
  cardProperties: {
    priority: true,
    description: true,
    assignee: true,
    startDate: true,
    dueDate: true,
    project: true,
    childProgress: true,
    labels: true,
  },
  cardPropertyIds: [],
  showSubIssues: true,
  listCollapsedStatuses: [],
  ganttZoom: "week",
  ganttShowCompleted: false,
  swimlaneGrouping: "assignee",
  swimlaneOrders: { parent: [], project: [], assignee: [] },
  collapsedSwimlanes: { parent: [], project: [], assignee: [] },
  tableColumns: DEFAULT_TABLE_COLUMNS.map((column) => ({ ...column })),
  tableGrouping: "none",
  tableCollapsedGroups: [],
  tableCollapsedParents: [],
  tableHierarchy: true,
  tableCalculation: "none",

  setViewMode: (mode) => set({ viewMode: mode }),
  setGanttZoom: (zoom) => set({ ganttZoom: zoom }),
  toggleGanttShowCompleted: () =>
    set((state) => ({ ganttShowCompleted: !state.ganttShowCompleted })),
  setGrouping: (grouping) => set({ grouping }),
  toggleStatusFilter: (status) =>
    set((state) => ({
      statusFilters: state.statusFilters.includes(status)
        ? state.statusFilters.filter((s) => s !== status)
        : [...state.statusFilters, status],
    })),
  togglePriorityFilter: (priority) =>
    set((state) => ({
      priorityFilters: state.priorityFilters.includes(priority)
        ? state.priorityFilters.filter((p) => p !== priority)
        : [...state.priorityFilters, priority],
    })),
  toggleAssigneeFilter: (value) =>
    set((state) => {
      const exists = state.assigneeFilters.some(
        (f) => f.type === value.type && f.id === value.id,
      );
      return {
        assigneeFilters: exists
          ? state.assigneeFilters.filter(
              (f) => !(f.type === value.type && f.id === value.id),
            )
          : [...state.assigneeFilters, value],
      };
    }),
  toggleNoAssignee: () =>
    set((state) => ({ includeNoAssignee: !state.includeNoAssignee })),
  toggleCreatorFilter: (value) =>
    set((state) => {
      const exists = state.creatorFilters.some(
        (f) => f.type === value.type && f.id === value.id,
      );
      return {
        creatorFilters: exists
          ? state.creatorFilters.filter(
              (f) => !(f.type === value.type && f.id === value.id),
            )
          : [...state.creatorFilters, value],
      };
    }),
  toggleProjectFilter: (projectId) =>
    set((state) => ({
      projectFilters: state.projectFilters.includes(projectId)
        ? state.projectFilters.filter((id) => id !== projectId)
        : [...state.projectFilters, projectId],
    })),
  toggleNoProject: () =>
    set((state) => ({ includeNoProject: !state.includeNoProject })),
  toggleLabelFilter: (labelId) =>
    set((state) => ({
      labelFilters: state.labelFilters.includes(labelId)
        ? state.labelFilters.filter((id) => id !== labelId)
        : [...state.labelFilters, labelId],
    })),
  togglePropertyFilter: (propertyId, optionId) =>
    set((state) => {
      const current = state.propertyFilters[propertyId] ?? [];
      const next = current.includes(optionId)
        ? current.filter((id) => id !== optionId)
        : [...current, optionId];
      const propertyFilters = { ...state.propertyFilters };
      if (next.length === 0) delete propertyFilters[propertyId];
      else propertyFilters[propertyId] = next;
      return { propertyFilters };
    }),
  setDateFilter: (filter) => set({ dateFilter: filter }),
  toggleAgentRunningFilter: () =>
    set((state) => ({ agentRunningFilter: !state.agentRunningFilter })),
  hideStatus: (status) =>
    set((state) => {
      // If no filter active, activate filter with all EXCEPT this one
      if (state.statusFilters.length === 0) {
        return { statusFilters: ALL_STATUSES.filter((s) => s !== status) };
      }
      return {
        statusFilters: state.statusFilters.filter((s) => s !== status),
      };
    }),
  showStatus: (status) =>
    set((state) => {
      if (state.statusFilters.length === 0) return state;
      if (state.statusFilters.includes(status)) return state;
      return { statusFilters: [...state.statusFilters, status] };
    }),
  clearFilters: () =>
    set({
      statusFilters: [],
      priorityFilters: [],
      assigneeFilters: [],
      includeNoAssignee: false,
      creatorFilters: [],
      projectFilters: [],
      includeNoProject: false,
      labelFilters: [],
      propertyFilters: {},
      dateFilter: null,
      agentRunningFilter: false,
    }),
  setSortBy: (field) => set({ sortBy: field }),
  setSortDirection: (dir) => set({ sortDirection: dir }),
  toggleCardProperty: (key) =>
    set((state) => ({
      cardProperties: {
        ...state.cardProperties,
        [key]: !state.cardProperties[key],
      },
    })),
  toggleCardPropertyId: (propertyId) =>
    set((state) => ({
      cardPropertyIds: state.cardPropertyIds.includes(propertyId)
        ? state.cardPropertyIds.filter((id) => id !== propertyId)
        : [...state.cardPropertyIds, propertyId],
    })),
  toggleShowSubIssues: () =>
    set((state) => ({ showSubIssues: !state.showSubIssues })),
  toggleListCollapsed: (status) =>
    set((state) => ({
      listCollapsedStatuses: state.listCollapsedStatuses.includes(status)
        ? state.listCollapsedStatuses.filter((s) => s !== status)
        : [...state.listCollapsedStatuses, status],
    })),
  setSwimlaneGrouping: (grouping) => set({ swimlaneGrouping: grouping }),
  setSwimlaneOrder: (order) =>
    set((state) => ({
      swimlaneOrders: { ...state.swimlaneOrders, [state.swimlaneGrouping]: order },
    })),
  toggleSwimlaneCollapsed: (key) =>
    set((state) => {
      const grouping = state.swimlaneGrouping;
      const current = state.collapsedSwimlanes[grouping];
      const next = current.includes(key)
        ? current.filter((k) => k !== key)
        : [...current, key];
      return {
        collapsedSwimlanes: { ...state.collapsedSwimlanes, [grouping]: next },
      };
    }),
  toggleTableColumn: (key) =>
    set((state) => {
      if (key === "title") return state;
      const exists = state.tableColumns.some((column) => column.key === key);
      return {
        tableColumns: exists
          ? state.tableColumns.filter((column) => column.key !== key)
          : [...state.tableColumns, { key }],
      };
    }),
  reorderTableColumn: (active, over) =>
    set((state) => {
      if (active === "title" || over === "title" || active === over) return state;
      const from = state.tableColumns.findIndex((column) => column.key === active);
      const to = state.tableColumns.findIndex((column) => column.key === over);
      if (from < 0 || to < 0) return state;
      const tableColumns = [...state.tableColumns];
      const [moved] = tableColumns.splice(from, 1);
      if (!moved) return state;
      tableColumns.splice(to, 0, moved);
      return { tableColumns };
    }),
  setTableColumnWidth: (key, width) =>
    set((state) => ({
      tableColumns: state.tableColumns.map((column) =>
        column.key === key
          ? { ...column, ...(width === undefined ? { width: undefined } : { width }) }
          : column,
      ),
    })),
  setTableGrouping: (tableGrouping) => set({ tableGrouping }),
  toggleTableGroupCollapsed: (key) =>
    set((state) => ({
      tableCollapsedGroups: state.tableCollapsedGroups.includes(key)
        ? state.tableCollapsedGroups.filter((item) => item !== key)
        : [...state.tableCollapsedGroups, key],
    })),
  toggleTableParentCollapsed: (issueId) =>
    set((state) => ({
      tableCollapsedParents: state.tableCollapsedParents.includes(issueId)
        ? state.tableCollapsedParents.filter((id) => id !== issueId)
        : [...state.tableCollapsedParents, issueId],
    })),
  toggleTableHierarchy: () =>
    set((state) => ({ tableHierarchy: !state.tableHierarchy })),
  setTableCalculation: (tableCalculation) => set({ tableCalculation }),
});

export const viewStorePersistOptions = (name: string) => ({
  name,
  storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
  partialize: (state: IssueViewState) => ({
    // NOTE: `agentRunningFilter` is intentionally NOT persisted — running
    // state changes second-to-second, and a stored toggle would let users
    // return to an unexplained empty list. Keep it ephemeral. See the
    // field comment on IssueViewState.
    // `dateFilter` is also intentionally not persisted: relative presets such
    // as Today would otherwise become stale after a calendar-day rollover.
    viewMode: state.viewMode,
    grouping: state.grouping,
    statusFilters: state.statusFilters,
    priorityFilters: state.priorityFilters,
    assigneeFilters: state.assigneeFilters,
    includeNoAssignee: state.includeNoAssignee,
    creatorFilters: state.creatorFilters,
    projectFilters: state.projectFilters,
    includeNoProject: state.includeNoProject,
    labelFilters: state.labelFilters,
    propertyFilters: state.propertyFilters,
    sortBy: state.sortBy,
    sortDirection: state.sortDirection,
    cardProperties: state.cardProperties,
    cardPropertyIds: state.cardPropertyIds,
    showSubIssues: state.showSubIssues,
    listCollapsedStatuses: state.listCollapsedStatuses,
    ganttZoom: state.ganttZoom,
    ganttShowCompleted: state.ganttShowCompleted,
    swimlaneGrouping: state.swimlaneGrouping,
    swimlaneOrders: state.swimlaneOrders,
    collapsedSwimlanes: state.collapsedSwimlanes,
    tableColumns: state.tableColumns,
    tableGrouping: state.tableGrouping,
    tableCollapsedGroups: state.tableCollapsedGroups,
    tableCollapsedParents: state.tableCollapsedParents,
    tableHierarchy: state.tableHierarchy,
    tableCalculation: state.tableCalculation,
  }),
  // Default Zustand merge is shallow, so a persisted `cardProperties` snapshot
  // saved before a new toggle was introduced wins entirely and the new key is
  // missing — the dropdown switch then reads `undefined` and renders unchecked
  // even though defaults treat it as on. Deep-merge `cardProperties` so newly
  // added toggles inherit their default value for existing users.
  merge: mergeViewStatePersisted,
});

/**
 * Reusable persist `merge` for view-state stores. Generic over T so the same
 * deep-merge for `cardProperties` works for both the issues view store and
 * the my-issues view store (which extends IssueViewState).
 */
export function mergeViewStatePersisted<T extends IssueViewState>(
  persisted: unknown,
  current: T,
): T {
  const p = (persisted ?? {}) as Partial<T>;
  // `collapsedSwimlanes` changed shape from `string[]` to
  // `Record<SwimlaneGrouping, string[]>`. A snapshot saved in the old
  // shape would otherwise overwrite the default record with an array
  // and crash on first read — fall back to the default when the
  // persisted value isn't a plain object.
  const isRecord = (v: unknown): v is Record<string, unknown> =>
    v !== null && typeof v === "object" && !Array.isArray(v);
  const persistedTableColumns = Array.isArray(p.tableColumns)
    ? p.tableColumns.filter(
        (column): column is TableColumnConfig =>
          !!column &&
          typeof column === "object" &&
          typeof (column as TableColumnConfig).key === "string",
      )
    : current.tableColumns;
  const dedupedTableColumns = Array.from(
    new Map(persistedTableColumns.map((column) => [column.key, column])).values(),
  ).filter((column) => column.key !== "title");
  const persistedTitle = persistedTableColumns.find(
    (column) => column.key === "title",
  );
  return {
    ...current,
    ...p,
    cardProperties: {
      ...current.cardProperties,
      ...(p.cardProperties ?? {}),
    },
    swimlaneOrders: isRecord(p.swimlaneOrders)
      ? { ...current.swimlaneOrders, ...p.swimlaneOrders }
      : current.swimlaneOrders,
    collapsedSwimlanes: isRecord(p.collapsedSwimlanes)
      ? { ...current.collapsedSwimlanes, ...p.collapsedSwimlanes }
      : current.collapsedSwimlanes,
    tableColumns: [
      persistedTitle ?? current.tableColumns[0] ?? { key: "title" },
      ...dedupedTableColumns,
    ],
    tableCollapsedGroups: Array.isArray(p.tableCollapsedGroups)
      ? p.tableCollapsedGroups
      : current.tableCollapsedGroups,
    tableCollapsedParents: Array.isArray(p.tableCollapsedParents)
      ? p.tableCollapsedParents
      : current.tableCollapsedParents,
  };
}

/** Factory: creates a vanilla StoreApi for use with React Context. */
export function createIssueViewStore(persistKey: string): StoreApi<IssueViewState> {
  const store = createStore<IssueViewState>()(
    persist(viewStoreSlice, viewStorePersistOptions(persistKey))
  );
  registerForWorkspaceRehydration(() => store.persist.rehydrate());
  return store;
}

/** Global singleton for the /issues page. */
export const useIssueViewStore = create<IssueViewState>()(
  persist(viewStoreSlice, viewStorePersistOptions("multica_issues_view"))
);

registerForWorkspaceRehydration(() => useIssueViewStore.persist.rehydrate());

/**
 * Clears the given view store's filters whenever the workspace id changes.
 *
 * URL-driven: wsId arrives from `useWorkspaceId()` (Context fed by the
 * `[workspaceSlug]` route). We track the previous id via ref so the first
 * render doesn't wipe persisted filters — clearing only fires on transitions
 * from one defined workspace to another.
 */
export function useClearFiltersOnWorkspaceChange(
  store: StoreApi<IssueViewState> | { getState: () => IssueViewState },
  wsId: string | undefined,
) {
  const prevIdRef = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (prevIdRef.current && wsId && wsId !== prevIdRef.current) {
      store.getState().clearFilters();
    }
    prevIdRef.current = wsId;
  }, [wsId, store]);
}
