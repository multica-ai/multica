"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";

const MAX_RECENT = 8;

interface DevActionsStore {
  /** Commands pinned to the dev toolbar, in the order they were pinned. */
  pinned: string[];
  /** Commands most recently run, newest first. */
  recent: string[];
  /** The Actions panel section last open, so it reopens there. */
  section: string | null;
  togglePin: (command: string) => void;
  recordRun: (command: string) => void;
  setSection: (section: string) => void;
}

/**
 * What the dev toolbar's Actions panel remembers on this client. Commands are
 * opaque keys the toolbar builds from its action registry; a key whose action
 * is gone is ignored by the reader, not cleaned up here.
 */
export const useDevActionsStore = create<DevActionsStore>()(
  persist(
    (set) => ({
      pinned: [],
      recent: [],
      section: null,
      togglePin: (command) =>
        set((s) => ({
          pinned: s.pinned.includes(command) ? s.pinned.filter((k) => k !== command) : [...s.pinned, command],
        })),
      recordRun: (command) =>
        set((s) => ({ recent: [command, ...s.recent.filter((k) => k !== command)].slice(0, MAX_RECENT) })),
      setSection: (section) => set({ section }),
    }),
    {
      name: "multica_dev_actions",
      storage: createJSONStorage(() => defaultStorage),
      partialize: ({ pinned, recent, section }) => ({ pinned, recent, section }),
      version: 1,
    },
  ),
);
