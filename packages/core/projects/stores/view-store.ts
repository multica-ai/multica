"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

export type ProjectSortField =
  | "priority"
  | "status"
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
  priority: "asc",
  status: "asc",
  created_at: "desc",
  updated_at: "desc",
  title: "asc",
};

export interface ProjectViewState {
  sortBy: ProjectSortField;
  sortDirection: ProjectSortDirection;
  setSortBy: (field: ProjectSortField) => void;
  setSortDirection: (direction: ProjectSortDirection) => void;
}

export const useProjectViewStore = create<ProjectViewState>()(
  persist(
    (set) => ({
      sortBy: "created_at",
      sortDirection: "desc",
      setSortBy: (sortBy) => set({ sortBy }),
      setSortDirection: (sortDirection) => set({ sortDirection }),
    }),
    {
      name: "multica_projects_view",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({
        sortBy: state.sortBy,
        sortDirection: state.sortDirection,
      }),
    },
  ),
);

registerForWorkspaceRehydration(() => useProjectViewStore.persist.rehydrate());
