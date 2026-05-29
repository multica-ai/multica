"use client";

import { useEffect, useRef } from "react";
import { create } from "zustand";
import { createStore, type StoreApi } from "zustand/vanilla";
import { createJSONStorage, persist } from "zustand/middleware";
import type { IssueStatus, IssuePriority, SavedView, ViewFilters } from "../../types";
import { ALL_STATUSES } from "../config";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

export type ViewMode = "board" | "list" | "gantt" | "swimlane";
export type GanttZoom = "day" | "week" | "month";
export type IssueGrouping = "status" | "assignee";
export type SwimlaneGrouping = "parent" | "project" | "assignee";
export type SortField = "position" | "priority" | "start_date" | "due_date" | "created_at" | "title";
export type SortDirection = "asc" | "desc";

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

export const SORT_OPTIONS: { value: SortField; label: string }[] = [
  { value: "position", label: "Manual" },
  { value: "priority", label: "Priority" },
  { value: "start_date", label: "Start date" },
  { value: "due_date", label: "Due date" },
  { value: "created_at", label: "Created date" },
  { value: "title", label: "Title" },
];

export const GROUPING_OPTIONS: { value: IssueGrouping; label: string }[] = [
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

/**
 * The subset of {@link IssueViewState} that a saved view controls. `setFilters`
 * patches these; `setActiveView` replaces them wholesale from a view's stored
 * `filters`. Excludes view-mode / sort / display preferences — those are
 * per-user surface settings, not part of the view's filter contract.
 */
export interface ViewFilterFields {
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
}

export interface IssueViewState {
  /** Active saved-view id, or null for an ad-hoc (unsaved) filter set. */
  currentViewId: string | null;
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
  // When true, the list only shows issues that currently have at least one
  // agent task in `running` status. Drives the workspace "agents working"
  // quick filter chip in the issues header. Not persisted across reloads —
  // running state changes second-to-second, a persisted toggle would let
  // users return to an empty list with no obvious cause.
  agentRunningFilter: boolean;
  sortBy: SortField;
  sortDirection: SortDirection;
  cardProperties: CardProperties;
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
  toggleAgentRunningFilter: () => void;
  hideStatus: (status: IssueStatus) => void;
  showStatus: (status: IssueStatus) => void;
  clearFilters: () => void;
  /** Patch any subset of the view-controlled filter fields in one set(). */
  setFilters: (partial: Partial<ViewFilterFields>) => void;
  /**
   * Activate a saved view: pin its id and REPLACE all filter fields from its
   * stored `filters` in a single set() (not a toggle loop). Passing null clears
   * the active view and resets all filter fields to empty.
   */
  setActiveView: (view: SavedView | null) => void;
  setSortBy: (field: SortField) => void;
  setSortDirection: (dir: SortDirection) => void;
  toggleCardProperty: (key: keyof CardProperties) => void;
  toggleListCollapsed: (status: IssueStatus) => void;
  setSwimlaneGrouping: (grouping: SwimlaneGrouping) => void;
  /** Update the lane order for the currently active swimlane grouping. */
  setSwimlaneOrder: (order: string[]) => void;
  /** Toggle a lane key in the currently active swimlane grouping. */
  toggleSwimlaneCollapsed: (key: string) => void;
}

/** Empty value for every view-controlled filter field. */
export const EMPTY_VIEW_FILTER_FIELDS: ViewFilterFields = {
  statusFilters: [],
  priorityFilters: [],
  assigneeFilters: [],
  includeNoAssignee: false,
  creatorFilters: [],
  projectFilters: [],
  includeNoProject: false,
  labelFilters: [],
};

/**
 * Split a saved view's `assignee_filters` / `creator_filters` tokens
 * ("type:id", id possibly a `{me}` / `{my_agents}` / `{my_squads}` placeholder)
 * back into the store's {@link ActorFilterValue} shape. Tokens without a `:`
 * (bare `{my_agents}` style) are dropped — they have no UI actor to render.
 */
function tokensToActors(tokens: string[] | undefined): ActorFilterValue[] {
  if (!tokens) return [];
  const out: ActorFilterValue[] = [];
  for (const token of tokens) {
    const sep = token.indexOf(":");
    if (sep === -1) continue;
    const type = token.slice(0, sep);
    if (type !== "member" && type !== "agent" && type !== "squad") continue;
    out.push({ type, id: token.slice(sep + 1) });
  }
  return out;
}

/**
 * Translate a saved view's stored {@link ViewFilters} into the store's filter
 * fields. `any_of` views (cross-dimension OR) can't be represented in the flat
 * filter-bar state, so their filter fields collapse to empty — the page still
 * drives its server fetch from the raw view `filters`, the store state only
 * backs the filter-bar UI and dirty-check.
 */
export function viewFiltersToState(filters: ViewFilters): ViewFilterFields {
  if (filters.any_of) return { ...EMPTY_VIEW_FILTER_FIELDS };
  return {
    statusFilters: filters.statuses ?? [],
    priorityFilters: filters.priorities ?? [],
    assigneeFilters: tokensToActors(filters.assignee_filters),
    includeNoAssignee: filters.include_no_assignee === true,
    creatorFilters: tokensToActors(filters.creator_filters),
    projectFilters: filters.project_ids ?? [],
    includeNoProject: filters.include_no_project === true,
    labelFilters: filters.label_ids ?? [],
  };
}

/**
 * Inverse of {@link viewFiltersToState}: project the store's flat filter fields
 * back into the API `ViewFilters` shape the server fetch consumes. Empty
 * fields are omitted so the request stays minimal. `any_of` is never produced
 * here — cross-dimension OR can't be expressed in the flat filter bar, so a
 * view that needs it (My Issues "All") must fetch from its raw stored filters,
 * not from reconstructed store state.
 */
export function stateToViewFilters(fields: ViewFilterFields): ViewFilters {
  const actorsToTokens = (actors: ActorFilterValue[]) =>
    actors.map((a) => `${a.type}:${a.id}`);
  const out: ViewFilters = {};
  if (fields.statusFilters.length) out.statuses = fields.statusFilters;
  if (fields.priorityFilters.length) out.priorities = fields.priorityFilters;
  if (fields.assigneeFilters.length) out.assignee_filters = actorsToTokens(fields.assigneeFilters);
  if (fields.includeNoAssignee) out.include_no_assignee = true;
  if (fields.creatorFilters.length) out.creator_filters = actorsToTokens(fields.creatorFilters);
  if (fields.projectFilters.length) out.project_ids = fields.projectFilters;
  if (fields.includeNoProject) out.include_no_project = true;
  if (fields.labelFilters.length) out.label_ids = fields.labelFilters;
  return out;
}

/**
 * Build the effective {@link ViewFilters} a page sends to the server: the live
 * filter-bar store fields (via {@link stateToViewFilters}) PLUS the active
 * view's view-only fields that have no filter-bar representation and so can't
 * survive the store round-trip — `assignee_types` (class-level Members /
 * Agents) and `any_of` (cross-dimension OR). Filter-bar edits layer on top as
 * outer AND fields.
 *
 * Without re-layering `assignee_types`, a flat Members/Agents view collapses to
 * `{}` after reconstruction and fetches every issue. `assignee_types` is only
 * applied when the filter bar hasn't pinned a specific assignee — the two are
 * mutually exclusive server-side (sending both is a 400), so a concrete
 * assignee selection wins over the class-level default.
 */
export function buildActiveViewFilters(
  fields: ViewFilterFields,
  activeView: SavedView | undefined,
): ViewFilters {
  const out = stateToViewFilters(fields);
  if (activeView?.filters.assignee_types?.length && !out.assignee_filters) {
    out.assignee_types = activeView.filters.assignee_types;
  }
  if (activeView?.filters.any_of) {
    out.any_of = activeView.filters.any_of;
  }
  return out;
}

export const viewStoreSlice = (set: StoreApi<IssueViewState>["setState"]): IssueViewState => ({
  currentViewId: null,
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
  listCollapsedStatuses: [],
  ganttZoom: "week",
  ganttShowCompleted: false,
  swimlaneGrouping: "assignee",
  swimlaneOrders: { parent: [], project: [], assignee: [] },
  collapsedSwimlanes: { parent: [], project: [], assignee: [] },

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
      currentViewId: null,
      statusFilters: [],
      priorityFilters: [],
      assigneeFilters: [],
      includeNoAssignee: false,
      creatorFilters: [],
      projectFilters: [],
      includeNoProject: false,
      labelFilters: [],
      agentRunningFilter: false,
    }),
  setFilters: (partial) => set(partial),
  setActiveView: (view) =>
    set({
      currentViewId: view?.id ?? null,
      ...(view ? viewFiltersToState(view.filters) : EMPTY_VIEW_FILTER_FIELDS),
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
});

export const viewStorePersistOptions = (name: string) => ({
  name,
  storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
  partialize: (state: IssueViewState) => ({
    // NOTE: `agentRunningFilter` is intentionally NOT persisted — running
    // state changes second-to-second, and a stored toggle would let users
    // return to an unexplained empty list. Keep it ephemeral. See the
    // field comment on IssueViewState.
    currentViewId: state.currentViewId,
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
    sortBy: state.sortBy,
    sortDirection: state.sortDirection,
    cardProperties: state.cardProperties,
    listCollapsedStatuses: state.listCollapsedStatuses,
    ganttZoom: state.ganttZoom,
    ganttShowCompleted: state.ganttShowCompleted,
    swimlaneGrouping: state.swimlaneGrouping,
    swimlaneOrders: state.swimlaneOrders,
    collapsedSwimlanes: state.collapsedSwimlanes,
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
