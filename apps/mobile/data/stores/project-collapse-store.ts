import { create } from "zustand";
import type { IssueStatus } from "@multica/core/types";

interface ProjectCollapseState {
  collapsedByProject: Record<string, IssueStatus[]>;
  toggle: (projectId: string, status: IssueStatus) => void;
  isCollapsed: (projectId: string, status: IssueStatus) => boolean;
}

export const useProjectCollapseStore = create<ProjectCollapseState>(
  (set, get) => ({
    collapsedByProject: {},
    toggle: (projectId, status) =>
      set((state) => {
        const current = state.collapsedByProject[projectId] ?? [];
        const next = current.includes(status)
          ? current.filter((s) => s !== status)
          : [...current, status];
        return {
          collapsedByProject: { ...state.collapsedByProject, [projectId]: next },
        };
      }),
    isCollapsed: (projectId, status) =>
      (get().collapsedByProject[projectId] ?? []).includes(status),
  }),
);
