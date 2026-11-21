"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";
import type { TrialReminderMilestone } from "./trial";

/**
 * The trial's one-time notices: the welcome card, each reminder, and the
 * "trial ended" message. Each shows once per device.
 */
export type TrialNotice =
  | "welcome"
  | `reminder:${TrialReminderMilestone}`
  | "ended";

/** Keeps the persisted record small; old trials age out first. */
const MAX_SEEN = 200;

/**
 * Keyed by workspace and the trial's end, so a later trial of the same
 * workspace starts with nothing seen.
 */
export function trialNoticeKey(wsId: string, endsAt: number, notice: TrialNotice): string {
  return `${wsId}:${endsAt}:${notice}`;
}

interface TrialNoticeState {
  /** Seen notice keys, oldest first. */
  seen: string[];
  markSeen: (key: string) => void;
}

export const useTrialNoticeStore = create<TrialNoticeState>()(
  persist(
    (set) => ({
      seen: [],
      markSeen: (key) =>
        set((state) =>
          state.seen.includes(key)
            ? state
            : { seen: [...state.seen, key].slice(-MAX_SEEN) },
        ),
    }),
    {
      name: "multica_trial_notices",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ seen: state.seen }),
      merge: (persisted, current) => {
        const saved = (persisted as { seen?: unknown } | undefined)?.seen;
        return {
          ...current,
          seen: Array.isArray(saved)
            ? saved.filter((key): key is string => typeof key === "string").slice(-MAX_SEEN)
            : [],
        };
      },
    },
  ),
);
