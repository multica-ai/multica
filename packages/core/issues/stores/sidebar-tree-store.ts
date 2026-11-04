"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";
import type { ProjectStatus } from "../../types";

export type SidebarIssueSort = "updated_at" | "created_at" | "priority" | "status" | "title" | "due_date";
export type SidebarIssueAssignee = "anyone" | "me" | "my_agents" | "me_and_my_agents";

/** How expanded projects list their issues in the sidebar. */
export interface SidebarIssueSettings {
  sortBy: SidebarIssueSort;
  sortDirection: "asc" | "desc";
  /** Status keys left out; `null` leaves out every finished (done / closed) status. */
  hiddenStatuses: string[] | null;
  assignee: SidebarIssueAssignee;
  showSubIssues: boolean;
}

export const DEFAULT_SIDEBAR_ISSUE_SETTINGS: SidebarIssueSettings = {
  sortBy: "updated_at",
  sortDirection: "desc",
  hiddenStatuses: null,
  assignee: "anyone",
  showSubIssues: true,
};

export type SidebarProjectOrdering = "manual" | "name" | "updated" | "created";

/** Which projects the sidebar lists, and in what order. */
export interface SidebarProjectSettings {
  ordering: SidebarProjectOrdering;
  statuses: ProjectStatus[];
  onlyMine: boolean;
  showCount: boolean;
}

export const DEFAULT_SIDEBAR_PROJECT_SETTINGS: SidebarProjectSettings = {
  ordering: "manual",
  statuses: ["planned", "in_progress", "paused"],
  onlyMine: false,
  showCount: true,
};


/**
 * This device's sidebar layout. The Projects section's settings and order are
 * per user on the server (see `sidebar-project-prefs`); what is left here is
 * what stays per device: which rows and sections are open.
 */
interface SidebarTreeState {
  /** Expanded rows as `project:{id}` / `view:{id}`. */
  expanded: string[];
  /**
   * The Projects section's settings from before they followed the user. Read
   * only, and only until the user's server copy is first saved.
   */
  issueSettings: SidebarIssueSettings;
  projectSettings: SidebarProjectSettings;
  projectOrder: string[];
  projectsCollapsed: boolean;
  viewsCollapsed: boolean;
  pinnedCollapsed: boolean;
  setExpanded: (key: string, expanded: boolean) => void;
  setProjectsCollapsed: (collapsed: boolean) => void;
  setViewsCollapsed: (collapsed: boolean) => void;
  setPinnedCollapsed: (collapsed: boolean) => void;
}

type PersistedSidebarTree = Pick<
  SidebarTreeState,
  | "expanded"
  | "issueSettings"
  | "projectSettings"
  | "projectOrder"
  | "projectsCollapsed"
  | "viewsCollapsed"
  | "pinnedCollapsed"
>;

const INITIAL: PersistedSidebarTree = {
  expanded: [],
  issueSettings: DEFAULT_SIDEBAR_ISSUE_SETTINGS,
  projectSettings: DEFAULT_SIDEBAR_PROJECT_SETTINGS,
  projectOrder: [],
  projectsCollapsed: false,
  viewsCollapsed: false,
  pinnedCollapsed: false,
};

export const useSidebarTreeStore = create<SidebarTreeState>()(
  persist(
    (set) => ({
      ...INITIAL,
      setExpanded: (key, expanded) =>
        set((s) => {
          if (s.expanded.includes(key) === expanded) return s;
          return { expanded: expanded ? [...s.expanded, key] : s.expanded.filter((k) => k !== key) };
        }),
      setProjectsCollapsed: (collapsed) => set({ projectsCollapsed: collapsed }),
      setViewsCollapsed: (collapsed) => set({ viewsCollapsed: collapsed }),
      setPinnedCollapsed: (collapsed) => set({ pinnedCollapsed: collapsed }),
    }),
    {
      name: "multica_sidebar_tree",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state): PersistedSidebarTree => ({
        expanded: state.expanded,
        issueSettings: state.issueSettings,
        projectSettings: state.projectSettings,
        projectOrder: state.projectOrder,
        projectsCollapsed: state.projectsCollapsed,
        viewsCollapsed: state.viewsCollapsed,
        pinnedCollapsed: state.pinnedCollapsed,
      }),
      // A workspace switch rehydrates into the previous workspace's state:
      // start from the initial values, never from `current`'s data.
      merge: (persisted, current) => {
        const p = (persisted ?? {}) as Partial<PersistedSidebarTree>;
        return {
          ...current,
          ...INITIAL,
          ...p,
          issueSettings: { ...DEFAULT_SIDEBAR_ISSUE_SETTINGS, ...p.issueSettings },
          projectSettings: { ...DEFAULT_SIDEBAR_PROJECT_SETTINGS, ...p.projectSettings },
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useSidebarTreeStore.persist.rehydrate());
