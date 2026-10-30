"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";

const MAX_RECENT_VISITS = 20;
const MAX_WORKSPACES = 50;
const EMPTY: RecentVisitEntry[] = [];

/**
 * Non-issue objects for the command bar's mixed Recent group. Issues keep
 * their own store (`useRecentIssuesStore`), which already has many readers;
 * the bar merges the two by `visitedAt`.
 */
export type RecentVisitType = "project" | "agent" | "autopilot";

export interface RecentVisitEntry {
  type: RecentVisitType;
  id: string;
  visitedAt: number;
}

interface RecentVisitsState {
  byWorkspace: Record<string, RecentVisitEntry[]>;
  recordVisit: (wsId: string, entry: Pick<RecentVisitEntry, "type" | "id">) => void;
  pruneWorkspaces: (activeWsIds: string[]) => void;
}

const sameEntry = (a: Pick<RecentVisitEntry, "type" | "id">, b: Pick<RecentVisitEntry, "type" | "id">) =>
  a.type === b.type && a.id === b.id;

// Keyed by workspace id rather than a slug-namespaced storage key for the same
// reason as recent-issues-store: child mount effects fire before the workspace
// layout sets the current slug.
export const useRecentVisitsStore = create<RecentVisitsState>()(
  persist(
    (set) => ({
      byWorkspace: {},
      recordVisit: (wsId, entry) =>
        set((state) => {
          const bucket = state.byWorkspace[wsId] ?? EMPTY;
          const next: RecentVisitEntry = { type: entry.type, id: entry.id, visitedAt: Date.now() };
          const nextBucket = [next, ...bucket.filter((item) => !sameEntry(item, entry))].slice(
            0,
            MAX_RECENT_VISITS,
          );
          let byWorkspace = { ...state.byWorkspace, [wsId]: nextBucket };
          const ids = Object.keys(byWorkspace);
          if (ids.length > MAX_WORKSPACES) {
            const oldest = ids.reduce((oldestId, candidateId) => {
              const a = byWorkspace[oldestId]?.[0]?.visitedAt ?? 0;
              const b = byWorkspace[candidateId]?.[0]?.visitedAt ?? 0;
              return b < a ? candidateId : oldestId;
            });
            const { [oldest]: _, ...rest } = byWorkspace;
            byWorkspace = rest;
          }
          return { byWorkspace };
        }),
      pruneWorkspaces: (activeWsIds) =>
        set((state) => {
          const allow = new Set(activeWsIds);
          let changed = false;
          const next: Record<string, RecentVisitEntry[]> = {};
          for (const [wsId, items] of Object.entries(state.byWorkspace)) {
            if (allow.has(wsId)) next[wsId] = items;
            else changed = true;
          }
          return changed ? { byWorkspace: next } : state;
        }),
    }),
    {
      name: "multica_recent_visits",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ byWorkspace: state.byWorkspace }),
      version: 1,
    },
  ),
);

export function selectRecentVisits(wsId: string | null) {
  return (state: RecentVisitsState) => (wsId ? (state.byWorkspace[wsId] ?? EMPTY) : EMPTY);
}
