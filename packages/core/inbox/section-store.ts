"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";

/** The Inbox sections that fold. "Needs you" and "Working" are always open. */
export type InboxFoldingSection = "updates" | "snoozed";

interface InboxSectionState {
  open: Record<InboxFoldingSection, boolean>;
  toggle: (section: InboxFoldingSection) => void;
}

const CLOSED: Record<InboxFoldingSection, boolean> = { updates: false, snoozed: false };

/**
 * Which folding sections the user has opened. Both start closed: they are
 * what the user does not have to look at. A view preference, so it is kept
 * across workspaces and restarts.
 */
export const useInboxSectionStore = create<InboxSectionState>()(
  persist(
    (set) => ({
      open: CLOSED,
      toggle: (section) =>
        set((state) => ({ open: { ...state.open, [section]: !state.open[section] } })),
    }),
    {
      name: "multica_inbox_sections",
      version: 1,
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ open: state.open }),
      merge: (persisted, current) => {
        const stored = (persisted as { open?: Record<string, unknown> } | undefined)?.open;
        return {
          ...current,
          open: {
            updates: stored?.updates === true,
            snoozed: stored?.snoozed === true,
          },
        };
      },
    },
  ),
);
