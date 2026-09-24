"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";

/**
 * Away this long and the next Home visit starts a new "since" boundary. Shorter
 * trips — into the queue, an issue, and back — keep the boundary, so what
 * changed does not empty itself while the viewer works through it.
 */
export const HOME_SESSION_GAP_MS = 30 * 60 * 1000;

const DAY_MS = 24 * 60 * 60 * 1000;

export interface HomeVisitRecord {
  /** The "since" boundary of the current session. */
  since: string;
  /** When Home was last left. */
  lastSeen: string;
}

/**
 * The boundary for a visit starting at `now`: the last 24 hours on a device
 * that has never recorded one, the previous departure after a long absence,
 * and the current session's boundary otherwise.
 */
export function resolveHomeSince(record: HomeVisitRecord | undefined, now: number): string {
  if (!record) return new Date(now - DAY_MS).toISOString();
  const away = now - new Date(record.lastSeen).getTime();
  return away > HOME_SESSION_GAP_MS ? record.lastSeen : record.since;
}

/**
 * Per-workspace Home visit bookkeeping behind "what changed since". Client-only
 * on purpose: the MVP adds no backend state, so each device keeps its own.
 */
interface HomeLastVisitState {
  visits: Record<string, HomeVisitRecord>;
  /** Resolves and records the boundary for a visit starting now. */
  beginVisit: (wsId: string, now?: number) => string;
  endVisit: (wsId: string, now?: number) => void;
}

export const useHomeLastVisitStore = create<HomeLastVisitState>()(
  persist(
    (set, get) => ({
      visits: {},
      beginVisit: (wsId, now = Date.now()) => {
        const since = resolveHomeSince(get().visits[wsId], now);
        const lastSeen = get().visits[wsId]?.lastSeen ?? new Date(now).toISOString();
        set((state) => ({ visits: { ...state.visits, [wsId]: { since, lastSeen } } }));
        return since;
      },
      endVisit: (wsId, now = Date.now()) =>
        set((state) => {
          const record = state.visits[wsId];
          if (!record) return state;
          return {
            visits: { ...state.visits, [wsId]: { ...record, lastSeen: new Date(now).toISOString() } },
          };
        }),
    }),
    {
      name: "multica_home_visits",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ visits: state.visits }),
    },
  ),
);
