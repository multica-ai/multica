"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

export type IssueCommentOrder = "oldest_first" | "newest_first";

interface IssueCommentOrderStore {
  order: IssueCommentOrder;
  setOrder: (order: IssueCommentOrder) => void;
}

export const useIssueCommentOrderStore = create<IssueCommentOrderStore>()(
  persist(
    (set) => ({
      order: "oldest_first",
      setOrder: (order) => set({ order }),
    }),
    {
      name: "multica_issue_comment_order",
      storage: createJSONStorage(() =>
        createWorkspaceAwareStorage(defaultStorage),
      ),
      partialize: (state) => ({ order: state.order }),
    },
  ),
);

registerForWorkspaceRehydration(() =>
  useIssueCommentOrderStore.persist.rehydrate(),
);
