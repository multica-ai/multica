"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";
import type { IssueStatus, ProjectStatus } from "../types";

const MAX_RECENT_CONTEXTS = 20;
const MAX_WORKSPACES = 50;
const EMPTY: RecentContextEntry[] = [];

export type RecentContextType = "issue" | "project";

export interface RecentContextEntry {
  type: RecentContextType;
  id: string;
  label?: string;
  subtitle?: string;
  status?: IssueStatus;
  projectStatus?: ProjectStatus;
  icon?: string | null;
  visitedAt: number;
}

interface RecentContextState {
  byWorkspace: Record<string, RecentContextEntry[]>;
  recordVisit: (wsId: string, entry: Pick<RecentContextEntry, "type" | "id"> & Partial<Pick<RecentContextEntry, "label" | "subtitle" | "status" | "projectStatus" | "icon">>) => void;
  forgetContext: (wsId: string, entry: Pick<RecentContextEntry, "type" | "id">) => void;
  pruneWorkspaces: (activeWsIds: string[]) => void;
}

function entryKey(entry: Pick<RecentContextEntry, "type" | "id">): string {
  return `${entry.type}:${entry.id}`;
}

export const useRecentContextStore = create<RecentContextState>()(
  persist(
    (set) => ({
      byWorkspace: {},
      recordVisit: (wsId, entry) =>
        set((state) => {
          const bucket = state.byWorkspace[wsId] ?? EMPTY;
          const key = entryKey(entry);
          const filtered = bucket.filter((item) => entryKey(item) !== key);
          const updated: RecentContextEntry = {
            type: entry.type,
            id: entry.id,
            label: entry.label,
            subtitle: entry.subtitle,
            status: entry.status,
            projectStatus: entry.projectStatus,
            icon: entry.icon,
            visitedAt: Date.now(),
          };
          const nextBucket = [updated, ...filtered].slice(0, MAX_RECENT_CONTEXTS);

          let nextByWorkspace = {
            ...state.byWorkspace,
            [wsId]: nextBucket,
          };

          const ids = Object.keys(nextByWorkspace);
          if (ids.length > MAX_WORKSPACES) {
            const oldest = ids.reduce((oldestId, candidateId) => {
              const a = nextByWorkspace[oldestId]?.[0]?.visitedAt ?? 0;
              const b = nextByWorkspace[candidateId]?.[0]?.visitedAt ?? 0;
              return b < a ? candidateId : oldestId;
            });
            const { [oldest]: _, ...rest } = nextByWorkspace;
            nextByWorkspace = rest;
          }

          return { byWorkspace: nextByWorkspace };
        }),
      forgetContext: (wsId, entry) =>
        set((state) => {
          const bucket = state.byWorkspace[wsId];
          if (!bucket) return state;
          const key = entryKey(entry);
          const nextBucket = bucket.filter((item) => entryKey(item) !== key);
          if (nextBucket.length === bucket.length) return state;
          if (nextBucket.length === 0) {
            const { [wsId]: _, ...rest } = state.byWorkspace;
            return { byWorkspace: rest };
          }
          return {
            byWorkspace: { ...state.byWorkspace, [wsId]: nextBucket },
          };
        }),
      pruneWorkspaces: (activeWsIds) =>
        set((state) => {
          const allow = new Set(activeWsIds);
          let changed = false;
          const next: Record<string, RecentContextEntry[]> = {};
          for (const [wsId, items] of Object.entries(state.byWorkspace)) {
            if (allow.has(wsId)) next[wsId] = items;
            else changed = true;
          }
          return changed ? { byWorkspace: next } : state;
        }),
    }),
    {
      name: "multica_recent_contexts",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ byWorkspace: state.byWorkspace }),
      version: 1,
      migrate: () => ({ byWorkspace: {} }),
    },
  ),
);

export function selectRecentContexts(wsId: string | null) {
  return (state: RecentContextState) =>
    wsId ? (state.byWorkspace[wsId] ?? EMPTY) : EMPTY;
}