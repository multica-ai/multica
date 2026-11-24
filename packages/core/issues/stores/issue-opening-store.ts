import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

export type IssueOpenMode = "page" | "peek";

interface IssueOpeningState {
  openMode: IssueOpenMode;
  setOpenMode: (openMode: IssueOpenMode) => void;
}

// Like theme and comment-composer preferences, this personal navigation habit
// is saved on this device across workspaces. Existing users keep full pages.
export const useIssueOpeningStore = create<IssueOpeningState>()(
  persist(
    (set) => ({
      openMode: "page",
      setOpenMode: (openMode) => set({ openMode }),
    }),
    {
      name: "multica_issue_opening",
      storage: createJSONStorage(() => defaultStorage),
      partialize: ({ openMode }) => ({ openMode }),
      merge: (persisted, current) => ({
        ...current,
        openMode:
          persisted && typeof persisted === "object" &&
          "openMode" in persisted && persisted.openMode === "peek"
            ? "peek"
            : "page",
      }),
    },
  ),
);
