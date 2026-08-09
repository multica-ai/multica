"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";
import { createSafeId } from "../../utils";
import type { IssueViewState } from "./view-store";

const EMPTY: SavedIssueView[] = [];

export interface SavedIssueView {
  id: string;
  name: string;
  createdAt: number;
  state: SavedIssueViewState;
}

/**
 * Serializable snapshot of the persisted issue-view fields. Mirrors the
 * `partialize` list in `viewStorePersistOptions` so a saved view restores
 * exactly what survives a reload. `dateFilter` and `agentRunningFilter` are
 * intentionally excluded — both are ephemeral on purpose (see the field
 * comments on `IssueViewState`).
 */
export type SavedIssueViewState = Pick<
  IssueViewState,
  | "viewMode"
  | "grouping"
  | "statusFilters"
  | "priorityFilters"
  | "assigneeFilters"
  | "includeNoAssignee"
  | "creatorFilters"
  | "projectFilters"
  | "includeNoProject"
  | "labelFilters"
  | "propertyFilters"
  | "sortBy"
  | "sortDirection"
  | "cardProperties"
  | "cardPropertyIds"
  | "showSubIssues"
  | "listCollapsedStatuses"
  | "ganttZoom"
  | "ganttShowCompleted"
  | "swimlaneGrouping"
  | "swimlaneOrders"
  | "collapsedSwimlanes"
  | "tableColumns"
  | "tableGrouping"
  | "tableCollapsedGroups"
  | "tableCollapsedParents"
  | "tableHierarchy"
  | "tableCalculation"
>;

interface SavedViewsState {
  /** Saved views grouped by workspace id (UUID). */
  byWorkspace: Record<string, SavedIssueView[]>;
  /** Snapshot the current view state and persist it as a named view. */
  saveView: (wsId: string, state: IssueViewState, name: string) => string;
  renameView: (wsId: string, id: string, name: string) => void;
  deleteView: (wsId: string, id: string) => void;
}

/** Extract the persisted subset of a live view state into a snapshot. */
export function snapshotIssueViewState(state: IssueViewState): SavedIssueViewState {
  return {
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
  };
}

/** A snapshot is directly usable as a `setState` partial on a view store. */
export function applySavedViewState(state: SavedIssueViewState): Partial<IssueViewState> {
  return { ...state };
}

/** Selector for the saved views of one workspace. */
export function selectSavedViews(wsId: string | null) {
  return (state: SavedViewsState) => (wsId ? (state.byWorkspace[wsId] ?? EMPTY) : EMPTY);
}

/**
 * Saved named issue-view presets, per workspace.
 *
 * Namespaced by workspace id in the data (not via `createWorkspaceAwareStorage`)
 * for the same reason as `useRecentIssuesStore`: child effects can write to a
 * store before WorkspaceRouteLayout has set the current slug, which would leak
 * the preset into a shared bare key. Keying on wsId keeps presets per-workspace
 * and survives workspace renames.
 */
export const useSavedViewsStore = create<SavedViewsState>()(
  persist(
    (set) => ({
      byWorkspace: {},
      saveView: (wsId, state, name) => {
        const id = createSafeId();
        const view: SavedIssueView = {
          id,
          name: name.trim(),
          createdAt: Date.now(),
          state: snapshotIssueViewState(state),
        };
        set((current) => ({
          byWorkspace: {
            ...current.byWorkspace,
            [wsId]: [...(current.byWorkspace[wsId] ?? EMPTY), view],
          },
        }));
        return id;
      },
      renameView: (wsId, id, name) =>
        set((current) => {
          const list = current.byWorkspace[wsId];
          if (!list) return current;
          let changed = false;
          const next = list.map((view) => {
            if (view.id !== id) return view;
            changed = true;
            return { ...view, name: name.trim() };
          });
          if (!changed) return current;
          return { byWorkspace: { ...current.byWorkspace, [wsId]: next } };
        }),
      deleteView: (wsId, id) =>
        set((current) => {
          const list = current.byWorkspace[wsId];
          if (!list) return current;
          const next = list.filter((view) => view.id !== id);
          if (next.length === list.length) return current;
          if (next.length === 0) {
            const { [wsId]: _removed, ...rest } = current.byWorkspace;
            return { byWorkspace: rest };
          }
          return { byWorkspace: { ...current.byWorkspace, [wsId]: next } };
        }),
    }),
    {
      name: "multica_saved_views",
      storage: createJSONStorage(() => defaultStorage),
      version: 1,
      partialize: (state) => ({ byWorkspace: state.byWorkspace }),
    },
  ),
);
