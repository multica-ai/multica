/**
 * Mobile-only zustand store for the My Issues view (scope + status/priority
 * filters). Mirrors the field shape of web's
 * `packages/core/issues/stores/my-issues-view-store.ts` so the same filter
 * input produces the same visible issue set on both clients (the "same N
 * rule" in apps/mobile/CLAUDE.md). Mobile cannot import core's runtime, so
 * this is re-implemented locally.
 *
 * Empty filter array = "show all" (matches web's predicate semantics in
 * packages/views/issues/utils/filter.ts). Completed issues are hidden until
 * `showCompleted` is turned on — see `hideCompletedIssues`.
 *
 * Persisted per user and workspace like web's store; see
 * `./view-prefs-storage.ts` for the storage and hydration lifecycle.
 */
import { create } from "zustand";
import { persist } from "zustand/middleware";
import type { IssuePriority, IssueStatus } from "@multica/core/types";
import type { MyIssuesScope } from "@/data/queries/issue-keys";
import { issueViewPersistOptions, type IssueViewPrefs } from "./issue-view-prefs";
import { registerViewPrefsStore } from "./view-prefs-storage";

const SCOPES: readonly MyIssuesScope[] = ["assigned", "created", "agents"];

interface MyIssuesViewState extends IssueViewPrefs<MyIssuesScope> {
  setScope: (scope: MyIssuesScope) => void;
  toggleStatusFilter: (status: IssueStatus) => void;
  togglePriorityFilter: (priority: IssuePriority) => void;
  setShowCompleted: (showCompleted: boolean) => void;
  clearFilters: () => void;
}

export const useMyIssuesViewStore = create<MyIssuesViewState>()(
  persist(
    (set) => ({
      scope: "assigned",
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
    issueViewPersistOptions<MyIssuesScope, MyIssuesViewState>(
      "multica_my_issues_view",
      SCOPES,
    ),
  ),
);

registerViewPrefsStore(useMyIssuesViewStore);
