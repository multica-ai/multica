"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

/** State for a single (issue, agent) worktree view. */
export interface WorktreeViewEntry {
  selectedPath: string | null;
  scrollTop: number;
}

interface WorktreeViewState {
  /** Keyed by `${issueId}:${agentId}`. */
  entries: Record<string, WorktreeViewEntry>;
  setSelectedPath: (
    issueId: string,
    agentId: string,
    path: string | null,
  ) => void;
  setScrollTop: (
    issueId: string,
    agentId: string,
    scrollTop: number,
  ) => void;
}

const key = (issueId: string, agentId: string) => `${issueId}:${agentId}`;

export const useWorktreeViewStore = create<WorktreeViewState>()(
  persist(
    (set) => ({
      entries: {},
      setSelectedPath: (issueId, agentId, path) =>
        set((state) => {
          const k = key(issueId, agentId);
          const prev = state.entries[k];
          return {
            entries: {
              ...state.entries,
              [k]: {
                selectedPath: path,
                scrollTop: prev?.scrollTop ?? 0,
              },
            },
          };
        }),
      setScrollTop: (issueId, agentId, scrollTop) =>
        set((state) => {
          const k = key(issueId, agentId);
          const prev = state.entries[k];
          // Avoid an update if unchanged — store is hit on every scroll event.
          if (prev && prev.scrollTop === scrollTop) return state;
          return {
            entries: {
              ...state.entries,
              [k]: {
                selectedPath: prev?.selectedPath ?? null,
                scrollTop,
              },
            },
          };
        }),
    }),
    {
      name: "multica_worktree_view",
      storage: createJSONStorage(() =>
        createWorkspaceAwareStorage(defaultStorage),
      ),
      partialize: (state) => ({ entries: state.entries }),
    },
  ),
);

registerForWorkspaceRehydration(() =>
  useWorktreeViewStore.persist.rehydrate(),
);
