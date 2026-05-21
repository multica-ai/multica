"use client";

import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

export type TimelineSortDirection = "asc" | "desc";

interface TimelineViewState {
  sortDirection: TimelineSortDirection;
  setSortDirection: (dir: TimelineSortDirection) => void;
  toggleSortDirection: () => void;
}

export const useTimelineViewStore = create<TimelineViewState>()(
  persist(
    (set) => ({
      sortDirection: "asc",
      setSortDirection: (dir) => set({ sortDirection: dir }),
      toggleSortDirection: () =>
        set((s) => ({ sortDirection: s.sortDirection === "asc" ? "desc" : "asc" })),
    }),
    {
      name: "multica_timeline_view",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({ sortDirection: state.sortDirection }),
    },
  ),
);

registerForWorkspaceRehydration(() => useTimelineViewStore.persist.rehydrate());
