"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

const MAX_RECENT_ISSUES = 20;

export interface RecentIssueEntry {
  id: string;
  visitedAt: number;
}

interface RecentIssuesState {
  items: RecentIssueEntry[];
  recordVisit: (id: string) => void;
}

export const useRecentIssuesStore = create<RecentIssuesState>()(
  persist(
    (set) => ({
      items: [],
      recordVisit: (id) =>
        set((state) => {
          const filtered = state.items.filter((i) => i.id !== id);
          const updated: RecentIssueEntry = { id, visitedAt: Date.now() };
          return {
            items: [updated, ...filtered].slice(0, MAX_RECENT_ISSUES),
          };
        }),
    }),
    {
      name: "multica_recent_issues",
      storage: createJSONStorage(() =>
        createWorkspaceAwareStorage(defaultStorage),
      ),
      partialize: (state) => ({ items: state.items }),
    },
  ),
);

registerForWorkspaceRehydration(() =>
  useRecentIssuesStore.persist.rehydrate(),
);
