"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import type { IssueStatus } from "../../types";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

const MAX_RECENT_ISSUES = 20;

export interface RecentIssueEntry {
  id: string;
  identifier: string;
  title: string;
  status: IssueStatus;
  visitedAt: number;
}

interface RecentIssuesState {
  items: RecentIssueEntry[];
  recordVisit: (entry: Omit<RecentIssueEntry, "visitedAt">) => void;
}

export const useRecentIssuesStore = create<RecentIssuesState>()(
  persist(
    (set) => ({
      items: [],
      recordVisit: (entry) =>
        set((state) => {
          const filtered = state.items.filter((i) => i.id !== entry.id);
          const updated: RecentIssueEntry = { ...entry, visitedAt: Date.now() };
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
