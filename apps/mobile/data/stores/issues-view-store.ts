/**
 * View store for the workspace-wide Issues page (`more/issues.tsx`).
 *
 * Shape mirrors `useMyIssuesViewStore` plus a `scope` field — workspace
 * Issues has `all / members / agents` scope tabs (see web
 * `packages/views/issues/components/issues-page.tsx:32-94`), while
 * My Issues has its own `assigned / created / agents` scopes.
 *
 * The `scope` filter is **client-side** on `assignee_type` — see
 * `more/issues.tsx`'s `scopedIssues` derivation. Server param stays unset
 * so the cache key (`issueKeys.list(wsId)`) and WS realtime invalidation
 * (`useIssuesRealtime`) don't have to know about scope.
 *
 * `IssuesScope` is defined locally rather than imported from
 * `@multica/core/issues/stores/issues-scope-store` — mobile only
 * `import type` from `@multica/core/types/*` per Sharing Principles, and
 * the union is small enough that a duplicated literal is preferable to a
 * cross-package type import hop.
 *
 * Empty filter array = "show all" (matches web's predicate semantics in
 * packages/views/issues/utils/filter.ts). Completed issues are hidden until
 * `showCompleted` is turned on — see `hideCompletedIssues`.
 *
 * Persisted per user and workspace, scope included (web persists it in
 * `issues-scope-store`); see `./view-prefs-storage.ts`. `clearFilters`
 * deliberately does NOT reset `scope` or `showCompleted`: they are view
 * choices, not filters.
 */
import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { IssuePriority, IssueStatus } from "@multica/core/types";
import { issueViewPersistOptions, type IssueViewPrefs } from "./issue-view-prefs";
import { registerViewPrefsStore } from "./view-prefs-storage";

export type IssuesScope = "all" | "members" | "agents";

const SCOPES: readonly IssuesScope[] = ["all", "members", "agents"];

interface IssuesViewState extends IssueViewPrefs<IssuesScope> {
  setScope: (scope: IssuesScope) => void;
  toggleStatusFilter: (status: IssueStatus) => void;
  togglePriorityFilter: (priority: IssuePriority) => void;
  setShowCompleted: (showCompleted: boolean) => void;
  clearFilters: () => void;
}

export const useIssuesViewStore = create<IssuesViewState>()(
  persist(
    (set) => ({
      scope: "all",
      statusFilters: [],
      priorityFilters: [],
      showCompleted: false,
      setScope: (scope) => set({ scope }),
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
      setShowCompleted: (showCompleted) => set({ showCompleted }),
      clearFilters: () => set({ statusFilters: [], priorityFilters: [] }),
    }),
    issueViewPersistOptions<IssuesScope, IssuesViewState>(
      "multica_issues_view",
      SCOPES,
    ),
  ),
);

registerViewPrefsStore(useIssuesViewStore);
