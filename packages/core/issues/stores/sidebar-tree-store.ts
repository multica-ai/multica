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
  /** The row after the projects that lists issues outside every project. */
  showNoProject: boolean;
  /** Every project row shows the default folder icon instead of the project's own. */
  minimalIcons: boolean;
}

export const DEFAULT_SIDEBAR_PROJECT_SETTINGS: SidebarProjectSettings = {
  ordering: "manual",
  statuses: ["planned", "in_progress", "paused"],
  onlyMine: false,
  showCount: true,
  showNoProject: true,
  minimalIcons: false,
};

/** How many rows a capped sidebar section shows before "Show more". */
export type SidebarSectionLimit = 5 | 10 | 20;

/** The Work panel's Inbox section. Which notifications exist is the account's notification preferences. */
export interface SidebarInboxSettings {
  limit: SidebarSectionLimit;
  /** The unread count beside the section label. */
  showCount: boolean;
  /** The unread count on the Work rail item. */
  railBadge: boolean;
  /** One line per notification, without the actor and what happened. */
  compact: boolean;
}

export const DEFAULT_SIDEBAR_INBOX_SETTINGS: SidebarInboxSettings = {
  limit: 5,
  showCount: true,
  railBadge: true,
  compact: false,
};

export interface SidebarLocateRequest {
  issueId: string;
  /** The project row's sidebar source, `project:{id}`. */
  source: string;
}

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
  inboxSettings: SidebarInboxSettings;
  pinnedLimit: SidebarSectionLimit;
  /** Counts on the Views section's rows (Unread, All views). */
  viewsShowCount: boolean;
  inboxCollapsed: boolean;
  projectsCollapsed: boolean;
  viewsCollapsed: boolean;
  pinnedCollapsed: boolean;
  workingCollapsed: boolean;
  needsAttentionCollapsed: boolean;
  /**
   * An issue the user asked to show in its project's row, until the Work
   * panel has opened that row and scrolled to it. Not persisted.
   */
  locate: SidebarLocateRequest | null;
  setExpanded: (key: string, expanded: boolean) => void;
  requestLocate: (request: SidebarLocateRequest | null) => void;
  setInboxSettings: (patch: Partial<SidebarInboxSettings>) => void;
  setPinnedLimit: (limit: SidebarSectionLimit) => void;
  setViewsShowCount: (show: boolean) => void;
  setInboxCollapsed: (collapsed: boolean) => void;
  setProjectsCollapsed: (collapsed: boolean) => void;
  setViewsCollapsed: (collapsed: boolean) => void;
  setPinnedCollapsed: (collapsed: boolean) => void;
  setWorkingCollapsed: (collapsed: boolean) => void;
  setNeedsAttentionCollapsed: (collapsed: boolean) => void;
}

type PersistedSidebarTree = Pick<
  SidebarTreeState,
  | "expanded"
  | "issueSettings"
  | "projectSettings"
  | "projectOrder"
  | "inboxSettings"
  | "pinnedLimit"
  | "viewsShowCount"
  | "inboxCollapsed"
  | "projectsCollapsed"
  | "viewsCollapsed"
  | "pinnedCollapsed"
  | "workingCollapsed"
  | "needsAttentionCollapsed"
>;

const INITIAL: PersistedSidebarTree = {
  expanded: [],
  issueSettings: DEFAULT_SIDEBAR_ISSUE_SETTINGS,
  projectSettings: DEFAULT_SIDEBAR_PROJECT_SETTINGS,
  projectOrder: [],
  inboxSettings: DEFAULT_SIDEBAR_INBOX_SETTINGS,
  pinnedLimit: 5,
  viewsShowCount: true,
  inboxCollapsed: false,
  projectsCollapsed: false,
  viewsCollapsed: false,
  pinnedCollapsed: false,
  workingCollapsed: false,
  needsAttentionCollapsed: false,
};

export const useSidebarTreeStore = create<SidebarTreeState>()(
  persist(
    (set) => ({
      ...INITIAL,
      locate: null,
      requestLocate: (locate) => set({ locate }),
      setExpanded: (key, expanded) =>
        set((s) => {
          if (s.expanded.includes(key) === expanded) return s;
          return { expanded: expanded ? [...s.expanded, key] : s.expanded.filter((k) => k !== key) };
        }),
      setInboxSettings: (patch) => set((s) => ({ inboxSettings: { ...s.inboxSettings, ...patch } })),
      setPinnedLimit: (pinnedLimit) => set({ pinnedLimit }),
      setViewsShowCount: (viewsShowCount) => set({ viewsShowCount }),
      setInboxCollapsed: (collapsed) => set({ inboxCollapsed: collapsed }),
      setProjectsCollapsed: (collapsed) => set({ projectsCollapsed: collapsed }),
      setViewsCollapsed: (collapsed) => set({ viewsCollapsed: collapsed }),
      setPinnedCollapsed: (collapsed) => set({ pinnedCollapsed: collapsed }),
      setWorkingCollapsed: (collapsed) => set({ workingCollapsed: collapsed }),
      setNeedsAttentionCollapsed: (collapsed) => set({ needsAttentionCollapsed: collapsed }),
    }),
    {
      name: "multica_sidebar_tree",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state): PersistedSidebarTree => ({
        expanded: state.expanded,
        issueSettings: state.issueSettings,
        projectSettings: state.projectSettings,
        projectOrder: state.projectOrder,
        inboxSettings: state.inboxSettings,
        pinnedLimit: state.pinnedLimit,
        viewsShowCount: state.viewsShowCount,
        inboxCollapsed: state.inboxCollapsed,
        projectsCollapsed: state.projectsCollapsed,
        viewsCollapsed: state.viewsCollapsed,
        pinnedCollapsed: state.pinnedCollapsed,
        workingCollapsed: state.workingCollapsed,
        needsAttentionCollapsed: state.needsAttentionCollapsed,
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
          inboxSettings: { ...DEFAULT_SIDEBAR_INBOX_SETTINGS, ...p.inboxSettings },
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useSidebarTreeStore.persist.rehydrate());
