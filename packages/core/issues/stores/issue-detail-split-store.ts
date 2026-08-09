import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

/**
 * Left list rail state for the `/{ws}/issues/{id}` detail route's two-column
 * layout. Persisted per workspace so each workspace remembers whether the
 * user collapsed the issue list rail.
 */
export interface IssueDetailSplitState {
  /** True when the left list rail is collapsed to a narrow rail. */
  collapsed: boolean;
  setCollapsed: (collapsed: boolean) => void;
  toggleCollapsed: () => void;
}

export const useIssueDetailSplitStore = create<IssueDetailSplitState>()(
  persist(
    (set) => ({
      collapsed: false,
      setCollapsed: (collapsed) => set({ collapsed }),
      toggleCollapsed: () => set((s) => ({ collapsed: !s.collapsed })),
    }),
    {
      name: "multica_issue_detail_split",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useIssueDetailSplitStore.persist.rehydrate());
