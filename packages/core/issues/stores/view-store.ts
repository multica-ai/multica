"use client";

import { useEffect, useRef } from "react";
import { create } from "zustand";
import { createStore, type StoreApi } from "zustand/vanilla";
import { createJSONStorage, persist } from "zustand/middleware";
import type { IssueStatus, IssuePriority } from "../../types";
import { ALL_STATUSES } from "../config";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

export type ViewMode = "board" | "list";
export type SortField = "position" | "priority" | "due_date" | "created_at" | "title";
export type SortDirection = "asc" | "desc";

export interface CardProperties {
  priority: boolean;
  description: boolean;
  assignee: boolean;
  dueDate: boolean;
  project: boolean;
  childProgress: boolean;
  labels: boolean;
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
  { key: "project", label: "Project" },
  { key: "labels", label: "Labels" },
  { key: "childProgress", label: "Sub-issue progress" },
];

export interface IssueViewState {
  viewMode: ViewMode;
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
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
  toggleProjectFilter: (projectId: string) => void;
  toggleNoProject: () => void;
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
  projectFilters: [],
  includeNoProject: false,
  sortBy: "position",
  sortDirection: "asc",
  cardProperties: {
    priority: true,
    description: true,
    assignee: true,
    dueDate: true,
    project: true,
    childProgress: true,
    labels: true,
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
  toggleProjectFilter: (projectId) =>
    set((state) => ({
      projectFilters: state.projectFilters.includes(projectId)
        ? state.projectFilters.filter((id) => id !== projectId)
        : [...state.projectFilters, projectId],
    })),
  toggleNoProject: () =>
    set((state) => ({ includeNoProject: !state.includeNoProject })),
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
  storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
  partialize: (state: IssueViewState) => ({
    viewMode: state.viewMode,
    statusFilters: state.statusFilters,
    priorityFilters: state.priorityFilters,
    assigneeFilters: state.assigneeFilters,
    includeNoAssignee: state.includeNoAssignee,
    creatorFilters: state.creatorFilters,
    projectFilters: state.projectFilters,
    includeNoProject: state.includeNoProject,
    sortBy: state.sortBy,
    sortDirection: state.sortDirection,
    cardProperties: state.cardProperties,
    listCollapsedStatuses: state.listCollapsedStatuses,
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
  return {
    ...current,
    ...p,
    cardProperties: {
      ...current.cardProperties,
      ...(p.cardProperties ?? {}),
    },
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
