"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";
import type { IssueAssigneeType } from "../../types";

const MAX_PICKS = 100;
const MAX_SCOPES = 20;
const EMPTY: AssigneePick[] = [];

/** One manual choice in the assignee picker. Counts are derived from the queue. */
export interface AssigneePick {
  type: IssueAssigneeType;
  id: string;
}

interface AssigneePicksState {
  /** FIFO queue (oldest first) per `${userId}:${wsId}`, capped at MAX_PICKS. */
  byScope: Record<string, AssigneePick[]>;
  recordPick: (userId: string, wsId: string, pick: AssigneePick) => void;
}

const scopeKey = (userId: string, wsId: string) => `${userId}:${wsId}`;

// Scoped by user + workspace id so another account or workspace on this device
// never borrows the ranking. Not cleared on logout, like the other recents.
export const useAssigneePicksStore = create<AssigneePicksState>()(
  persist(
    (set) => ({
      byScope: {},
      recordPick: (userId, wsId, pick) =>
        set((state) => {
          const key = scopeKey(userId, wsId);
          const queue = [...(state.byScope[key] ?? EMPTY), { type: pick.type, id: pick.id }].slice(-MAX_PICKS);
          // Re-inserting the key moves it to the end, so key order is
          // least-recently-used first and the oldest scope is evicted.
          const { [key]: _, ...rest } = state.byScope;
          const byScope: Record<string, AssigneePick[]> = { ...rest, [key]: queue };
          const keys = Object.keys(byScope);
          for (const stale of keys.slice(0, Math.max(0, keys.length - MAX_SCOPES))) {
            delete byScope[stale];
          }
          return { byScope };
        }),
    }),
    {
      name: "multica_assignee_picks",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ byScope: state.byScope }),
      version: 1,
    },
  ),
);

export function selectAssigneePicks(userId: string | null | undefined, wsId: string | null | undefined) {
  return (state: AssigneePicksState) =>
    userId && wsId ? (state.byScope[scopeKey(userId, wsId)] ?? EMPTY) : EMPTY;
}

export interface RankedAssignee extends AssigneePick {
  count: number;
}

/**
 * Pick counts, highest first. Ties go to the more recently picked actor —
 * the queue already records the order, so nothing extra is stored.
 */
export function rankAssigneePicks(picks: readonly AssigneePick[]): RankedAssignee[] {
  const byKey = new Map<string, RankedAssignee & { last: number }>();
  picks.forEach((pick, index) => {
    const key = `${pick.type}:${pick.id}`;
    const entry = byKey.get(key);
    if (entry) {
      entry.count += 1;
      entry.last = index;
    } else {
      byKey.set(key, { type: pick.type, id: pick.id, count: 1, last: index });
    }
  });
  return [...byKey.values()]
    .sort((a, b) => b.count - a.count || b.last - a.last)
    .map(({ type, id, count }) => ({ type, id, count }));
}
