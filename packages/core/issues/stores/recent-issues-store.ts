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
  isDeleted?: boolean;
}

interface RecentIssuesState {
  items: RecentIssueEntry[];
  recordVisit: (entry: Omit<RecentIssueEntry, "visitedAt" | "isDeleted">) => void;
  updateIssueStatus: (id: string, status: IssueStatus) => void;
  markAsDeleted: (id: string) => void;
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
      updateIssueStatus: (id, status) =>
        set((state) => ({
          items: state.items.map((item) =>
            item.id === id ? { ...item, status } : item,
          ),
        })),
      markAsDeleted: (id) =>
        set((state) => ({
          items: state.items.map((item) =>
            item.id === id ? { ...item, isDeleted: true } : item,
          ),
        })),
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
