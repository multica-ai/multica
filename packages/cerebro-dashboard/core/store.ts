"use client";

import { create } from "zustand";
import { DEFAULT_FILTER, type ActorScope, type TimeRange } from "./types";

// Client-state for the dashboard. Per state-management rules: ephemeral UI
// state (current tab, selected actor, time-range) lives in Zustand, not in
// the URL or query cache. Server data is fetched via TanStack Query keyed
// on these values.
export type DashboardTab = "issues" | "messages";

interface DashboardState {
  scope: ActorScope;
  actorId: string | null;
  actorName: string | null;
  range: TimeRange;
  tab: DashboardTab;
  setScope: (scope: ActorScope) => void;
  setActor: (id: string | null, name?: string | null) => void;
  setRange: (range: TimeRange) => void;
  setTab: (tab: DashboardTab) => void;
  reset: () => void;
}

export const useDashboardStore = create<DashboardState>((set) => ({
  ...DEFAULT_FILTER,
  tab: "issues",
  setScope: (scope) =>
    set((s) => (s.scope === scope ? s : { scope, actorId: null, actorName: null })),
  setActor: (actorId, actorName = null) => set({ actorId, actorName }),
  setRange: (range) => set({ range }),
  setTab: (tab) => set({ tab }),
  reset: () => set({ ...DEFAULT_FILTER, tab: "issues" }),
}));
