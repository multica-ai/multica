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
 * packages/views/issues/utils/filter.ts).
 *
 * No persist middleware — filters are session-scoped. `clearFilters`
 * deliberately does NOT reset `scope` so a workspace switch keeps the
 * user on the same scope tab (web's URL-driven scope reset is incidental
 * to its routing model, not an invariant mobile should mirror).
 */
import { create } from "zustand";
import type { IssuePriority, IssueStatus } from "@multica/core/types";

export type IssuesScope = "all" | "members" | "agents";

interface IssuesViewState {
  scope: IssuesScope;
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  sortByLastEdited: boolean;
  setScope: (scope: IssuesScope) => void;
  toggleStatusFilter: (status: IssueStatus) => void;
  togglePriorityFilter: (priority: IssuePriority) => void;
  toggleSortByLastEdited: () => void;
  clearFilters: () => void;
}

export const useIssuesViewStore = create<IssuesViewState>((set) => ({
  scope: "all",
  statusFilters: [],
  priorityFilters: [],
  sortByLastEdited: false,
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
  toggleSortByLastEdited: () =>
    set((state) => ({ sortByLastEdited: !state.sortByLastEdited })),
  clearFilters: () => set({ statusFilters: [], priorityFilters: [] }),
}));
