import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

export type IssueActivityOrder = "oldest_first" | "newest_first";

interface IssueActivityState {
  order: IssueActivityOrder;
  setOrder: (order: IssueActivityOrder) => void;
}

export const useIssueActivityStore = create<IssueActivityState>()(
  persist(
    (set) => ({
      order: "oldest_first",
      setOrder: (order) => set({ order }),
    }),
    {
      name: "multica_issue_activity",
      storage: createJSONStorage(() => defaultStorage),
      partialize: ({ order }) => ({ order }),
      merge: (persisted, current) => ({
        ...current,
        order:
          persisted && typeof persisted === "object" &&
          "order" in persisted && persisted.order === "newest_first"
            ? "newest_first"
            : "oldest_first",
      }),
    },
  ),
);
