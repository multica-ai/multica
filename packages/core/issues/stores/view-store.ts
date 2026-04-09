"use client";

import { create } from "zustand";
import { createStore, type StoreApi } from "zustand/vanilla";
import { persist } from "zustand/middleware";
import type { IssueStatus, IssuePriority } from "../../types";
import { ALL_STATUSES } from "../config";

export type ViewMode = "board" | "list";
export type SortField = "position" | "priority" | "due_date" | "created_at" | "title";
export type SortDirection = "asc" | "desc";

export interface CardProperties {
  priority: boolean;
  description: boolean;
  assignee: boolean;
  dueDate: boolean;
}

export interface ActorFilterValue {
  type: "member" | "agent";
  id: string;
}

export const SORT_OPTIONS: { value: SortField; label: string }[] = [
  { value: "position", label: "Manual" },
  { value: "priority", label: "Priority" },
  { value: "due_date", label: "Due date" },
  { value: "created_at", label: "Created date" },
  { value: "title", label: "Title" },
];

export const CARD_PROPERTY_OPTIONS: { key: keyof CardProperties; label: string }[] = [
  { key: "priority", label: "Priority" },
  { key: "description", label: "Description" },
  { key: "assignee", label: "Assignee" },
  { key: "dueDate", label: "Due date" },
];

export interface IssueViewState {
  viewMode: ViewMode;
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  creatorFilters: ActorFilterValue[];
  sortBy: SortField;
  sortDirection: SortDirection;
  cardProperties: CardProperties;
  listCollapsedStatuses: IssueStatus[];
  setViewMode: (mode: ViewMode) => void;
  toggleStatusFilter: (status: IssueStatus) => void;
  togglePriorityFilter: (priority: IssuePriority) => void;
  toggleAssigneeFilter: (value: ActorFilterValue) => void;
  toggleNoAssignee: () => void;
  toggleCreatorFilter: (value: ActorFilterValue) => void;
  hideStatus: (status: IssueStatus) => void;
  showStatus: (status: IssueStatus) => void;
  clearFilters: () => void;
  setSortBy: (field: SortField) => void;
  setSortDirection: (dir: SortDirection) => void;
  toggleCardProperty: (key: keyof CardProperties) => void;
  toggleListCollapsed: (status: IssueStatus) => void;
}

export const viewStoreSlice = (set: StoreApi<IssueViewState>["setState"]): IssueViewState => ({
  viewMode: "board",
  statusFilters: [],
  priorityFilters: [],
  assigneeFilters: [],
  includeNoAssignee: false,
  creatorFilters: [],
  sortBy: "position",
  sortDirection: "asc",
  cardProperties: {
    priority: true,
    description: true,
    assignee: true,
    dueDate: true,
  },
  listCollapsedStatuses: [],

  setViewMode: (mode) => set({ viewMode: mode }),
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
});

export const viewStorePersistOptions = (name: string) => ({
  name,
  partialize: (state: IssueViewState) => ({
    viewMode: state.viewMode,
    statusFilters: state.statusFilters,
    priorityFilters: state.priorityFilters,
    assigneeFilters: state.assigneeFilters,
    includeNoAssignee: state.includeNoAssignee,
    creatorFilters: state.creatorFilters,
    sortBy: state.sortBy,
    sortDirection: state.sortDirection,
    cardProperties: state.cardProperties,
    listCollapsedStatuses: state.listCollapsedStatuses,
  }),
});

/** Factory: creates a vanilla StoreApi for use with React Context. */
export function createIssueViewStore(persistKey: string): StoreApi<IssueViewState> {
  return createStore<IssueViewState>()(
    persist(viewStoreSlice, viewStorePersistOptions(persistKey))
  );
}

/** Global singleton for the /issues page. */
export const useIssueViewStore = create<IssueViewState>()(
  persist(viewStoreSlice, viewStorePersistOptions("multica_issues_view"))
);

// Clear filters on all registered view stores when workspace switches.
const _syncedStores = new Set<StoreApi<IssueViewState>>();
let _workspaceSyncInitialized = false;

/**
 * Register a view store to clear filters on workspace switch.
 *
 * @param store - The view store to register.
 * @param subscribeToWorkspace - Optional: a function that subscribes to workspace
 *   changes and calls the callback with the new workspace ID. The app layer should
 *   provide this to avoid a circular dependency on the workspace store.
 *   Example: `(cb) => useWorkspaceStore.subscribe(s => cb(s.workspace?.id))`
 */
export function registerViewStoreForWorkspaceSync(
  store: StoreApi<IssueViewState>,
  subscribeToWorkspace?: (callback: (workspaceId: string | undefined) => void) => void,
) {
  _syncedStores.add(store);
  if (_workspaceSyncInitialized) return;
  _workspaceSyncInitialized = true;

  if (subscribeToWorkspace) {
    let prevId: string | undefined;
    subscribeToWorkspace((id) => {
      if (prevId && id !== prevId) {
        for (const s of _syncedStores) s.getState().clearFilters();
      }
      prevId = id;
    });
  }
  // TODO: If no subscribeToWorkspace is provided, the workspace sync is a no-op.
  // The app layer (apps/web) should call this with the workspace store subscription
  // to wire up filter clearing on workspace switch.
}

/** Backward-compatible alias — registers the global singleton for workspace sync. */
export const initFilterWorkspaceSync = (
  subscribeToWorkspace?: (callback: (workspaceId: string | undefined) => void) => void,
) =>
  registerViewStoreForWorkspaceSync(useIssueViewStore, subscribeToWorkspace);
