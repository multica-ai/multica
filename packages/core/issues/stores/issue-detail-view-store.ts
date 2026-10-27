import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

export type IssueActivityOrder = "oldest" | "newest";

interface IssueDetailViewStore {
  activityOrder: IssueActivityOrder;
  setActivityOrder: (order: IssueActivityOrder) => void;
}

/**
 * Personal issue-detail reading preferences shared across workspaces.
 *
 * Activity defaults to chronological order for existing users. Comments are
 * not governed by this setting and retain their chronological order.
 */
export const useIssueDetailViewStore = create<IssueDetailViewStore>()(
  persist(
    (set) => ({
      activityOrder: "oldest",
      setActivityOrder: (activityOrder) => set({ activityOrder }),
    }),
    {
      name: "multica_issue_detail_view",
      storage: createJSONStorage(() => defaultStorage),
      partialize: ({ activityOrder }) => ({ activityOrder }),
    },
  ),
);
