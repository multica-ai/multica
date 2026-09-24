"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";
import type { Issue } from "../types";

/**
 * Replies the viewer posted to a Blocked request. The issue stays blocked until
 * the agent picks the reply up, so without this the entry would keep asking
 * for the same reply. Keyed by queue entry, valued by the reply's server
 * timestamp.
 *
 * A mark only holds while nothing happened on the issue after the reply: any
 * later activity (the agent answering, still blocked) makes the entry
 * actionable again. Persisted so a reload does not forget it; entries that
 * left the queue are dropped.
 */
interface NeedsMeRepliesState {
  replies: Record<string, string>;
  markReplied: (key: string, at: string) => void;
  /** Drops marks for entries no longer in the queue. */
  retainReplies: (liveKeys: ReadonlySet<string>) => void;
}

export const useNeedsMeRepliesStore = create<NeedsMeRepliesState>()(
  persist(
    (set) => ({
      replies: {},
      markReplied: (key, at) => set((state) => ({ replies: { ...state.replies, [key]: at } })),
      retainReplies: (liveKeys) =>
        set((state) => {
          const stale = Object.keys(state.replies).filter((key) => !liveKeys.has(key));
          if (stale.length === 0) return state;
          const next = { ...state.replies };
          for (const key of stale) delete next[key];
          return { replies: next };
        }),
    }),
    {
      name: "multica_home_replies",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ replies: state.replies }),
    },
  ),
);

// The reply itself bumps the issue's activity a moment after the comment row
// is written; anything later than this is someone else's move.
const REPLY_ACTIVITY_TOLERANCE_MS = 10_000;

/** True while the viewer's reply is still the latest thing on the issue. */
export function isReplyPending(repliedAt: string | undefined, issue: Pick<Issue, "last_activity_at"> | null): boolean {
  if (!repliedAt) return false;
  const activity = issue?.last_activity_at;
  if (!activity) return true;
  return new Date(activity).getTime() <= new Date(repliedAt).getTime() + REPLY_ACTIVITY_TOLERANCE_MS;
}
