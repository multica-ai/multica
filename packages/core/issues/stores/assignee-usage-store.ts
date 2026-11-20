"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";
import type { IssueAssigneeType } from "../../types";

// Per workspace, keep the most-used assignees only. The picker shows the top
// three, so a bucket this size leaves room for the ranking to shift without
// growing without bound.
const MAX_ENTRIES_PER_WORKSPACE = 30;
const MAX_WORKSPACES = 50;
const EMPTY: AssigneeUsageEntry[] = [];

export interface AssigneeUsageEntry {
  type: IssueAssigneeType;
  id: string;
  count: number;
  lastUsedAt: number;
}

interface AssigneeUsageState {
  byWorkspace: Record<string, AssigneeUsageEntry[]>;
  recordAssignment: (wsId: string, assignee: { type: IssueAssigneeType; id: string }) => void;
  pruneWorkspaces: (activeWsIds: string[]) => void;
}

/** Most-used first; the more recent pick wins a tie. */
export function compareAssigneeUsage(a: AssigneeUsageEntry, b: AssigneeUsageEntry): number {
  return b.count - a.count || b.lastUsedAt - a.lastUsedAt;
}

const latestUse = (bucket: AssigneeUsageEntry[] | undefined) =>
  bucket?.reduce((max, entry) => Math.max(max, entry.lastUsedAt), 0) ?? 0;

// How often this user picks each assignee, on this device only. Keyed by
// workspace id for the same reason as recent-issues-store: the picker can
// write from a child mount before the workspace layout sets the current slug.
export const useAssigneeUsageStore = create<AssigneeUsageState>()(
  persist(
    (set) => ({
      byWorkspace: {},
      recordAssignment: (wsId, { type, id }) =>
        set((state) => {
          const bucket = state.byWorkspace[wsId] ?? EMPTY;
          const existing = bucket.find((e) => e.type === type && e.id === id);
          const updated: AssigneeUsageEntry = {
            type,
            id,
            count: (existing?.count ?? 0) + 1,
            lastUsedAt: Date.now(),
          };
          const nextBucket = [updated, ...bucket.filter((e) => e !== existing)]
            .sort(compareAssigneeUsage)
            .slice(0, MAX_ENTRIES_PER_WORKSPACE);

          let nextByWorkspace = { ...state.byWorkspace, [wsId]: nextBucket };

          // Backstop for when pruneWorkspaces never runs: evict the workspace
          // whose last assignment is the oldest.
          const ids = Object.keys(nextByWorkspace);
          if (ids.length > MAX_WORKSPACES) {
            const oldest = ids.reduce((oldestId, candidateId) =>
              latestUse(nextByWorkspace[candidateId]) < latestUse(nextByWorkspace[oldestId])
                ? candidateId
                : oldestId,
            );
            const { [oldest]: _, ...rest } = nextByWorkspace;
            nextByWorkspace = rest;
          }

          return { byWorkspace: nextByWorkspace };
        }),
      pruneWorkspaces: (activeWsIds) =>
        set((state) => {
          const allow = new Set(activeWsIds);
          let changed = false;
          const next: Record<string, AssigneeUsageEntry[]> = {};
          for (const [wsId, entries] of Object.entries(state.byWorkspace)) {
            if (allow.has(wsId)) next[wsId] = entries;
            else changed = true;
          }
          return changed ? { byWorkspace: next } : state;
        }),
    }),
    {
      name: "multica_assignee_usage",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ byWorkspace: state.byWorkspace }),
    },
  ),
);

export function selectAssigneeUsage(wsId: string | null) {
  return (state: AssigneeUsageState) =>
    wsId ? (state.byWorkspace[wsId] ?? EMPTY) : EMPTY;
}
