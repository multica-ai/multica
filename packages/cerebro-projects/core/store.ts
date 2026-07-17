"use client";

import { create } from "zustand";
import { createJSONStorage, persist, type StateStorage } from "zustand/middleware";

interface ProjectsTreeState {
  expandedProjectsByWorkspace: Record<string, Record<string, boolean>>;
  showCompletedSprintsByWorkspace: Record<string, Record<string, boolean>>;
  toggleProject: (workspaceId: string, projectId: string) => void;
  toggleCompletedSprints: (workspaceId: string, projectId: string) => void;
}

const serverStorage: StateStorage = {
  getItem: () => null,
  setItem: () => undefined,
  removeItem: () => undefined,
};

export const useProjectsTreeStore = create<ProjectsTreeState>()(
  persist(
    (set) => ({
      expandedProjectsByWorkspace: {},
      showCompletedSprintsByWorkspace: {},
      toggleProject: (workspaceId, projectId) =>
        set((state) => ({
          expandedProjectsByWorkspace: {
            ...state.expandedProjectsByWorkspace,
            [workspaceId]: {
              ...state.expandedProjectsByWorkspace[workspaceId],
              [projectId]: !(
                state.expandedProjectsByWorkspace[workspaceId]?.[projectId] ?? true
              ),
            },
          },
        })),
      toggleCompletedSprints: (workspaceId, projectId) =>
        set((state) => ({
          showCompletedSprintsByWorkspace: {
            ...state.showCompletedSprintsByWorkspace,
            [workspaceId]: {
              ...state.showCompletedSprintsByWorkspace[workspaceId],
              [projectId]: !state.showCompletedSprintsByWorkspace[workspaceId]?.[projectId],
            },
          },
        })),
    }),
    {
      name: "multica_projects_tree",
      storage: createJSONStorage(() =>
        typeof window === "undefined" ? serverStorage : window.localStorage,
      ),
      partialize: ({ expandedProjectsByWorkspace, showCompletedSprintsByWorkspace }) => ({
        expandedProjectsByWorkspace,
        showCompletedSprintsByWorkspace,
      }),
    },
  ),
);
