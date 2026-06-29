"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../platform/workspace-storage";

// The inbox is a single deduplicated list (one row per issue). Sorting is the
// only view axis: Date (newest first, the historical default), Priority (by
// the linked issue's priority), and Unread first (unread items bubble up).
// The choice is a durable per-workspace preference, so it persists like the
// project view store.
export type InboxSortField = "date" | "priority" | "unread";

export type InboxSortDirection = "asc" | "desc";

export const INBOX_SORT_DEFAULT_DIRECTION: Record<
  InboxSortField,
  InboxSortDirection
> = {
  date: "desc", // newest first
  priority: "desc", // urgent first
  unread: "desc", // unread first
};

export interface InboxSortState {
  sortField: InboxSortField;
  sortDirection: InboxSortDirection;
  /** Flip direction if the same field is re-selected, otherwise switch field
   *  and reset to that field's default direction. */
  toggleSort: (field: InboxSortField) => void;
  setSortField: (field: InboxSortField) => void;
  setSortDirection: (direction: InboxSortDirection) => void;
}

const DEFAULTS = {
  sortField: "date" as InboxSortField,
  sortDirection: INBOX_SORT_DEFAULT_DIRECTION.date,
};

export const useInboxSortStore = create<InboxSortState>()(
  persist(
    (set) => ({
      ...DEFAULTS,
      toggleSort: (field) =>
        set((state) =>
          state.sortField === field
            ? { sortDirection: state.sortDirection === "asc" ? "desc" : "asc" }
            : {
                sortField: field,
                sortDirection: INBOX_SORT_DEFAULT_DIRECTION[field],
              },
        ),
      setSortField: (field) =>
        set((state) =>
          state.sortField === field
            ? {}
            : {
                sortField: field,
                sortDirection: INBOX_SORT_DEFAULT_DIRECTION[field],
              },
        ),
      setSortDirection: (direction) => set({ sortDirection: direction }),
    }),
    {
      name: "multica_inbox_sort",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({
        sortField: state.sortField,
        sortDirection: state.sortDirection,
      }),
      // Fall back to defaults for a payload persisted before this store
      // existed, so a missing field can't leave the list unsorted.
      merge: (persisted, current) => {
        if (!persisted) return { ...current, ...DEFAULTS };
        const p = persisted as Partial<InboxSortState>;
        return { ...current, ...DEFAULTS, ...p };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useInboxSortStore.persist.rehydrate());
