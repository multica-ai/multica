"use client";

import { create } from "zustand";
import { DEFAULT_FILTER, type ActorScope, type TimeRange } from "./types";

// Client-state for the dashboard. Per state-management rules: ephemeral UI
// state (current tab, selected actor, time-range) lives in Zustand, not in
// the URL or query cache. Server data is fetched via TanStack Query keyed
// on these values.
interface DashboardState {
  scope: ActorScope;
  actorId: string | null;
  range: TimeRange;
  setScope: (scope: ActorScope) => void;
  setActorId: (id: string | null) => void;
  setRange: (range: TimeRange) => void;
  reset: () => void;
}

export const useDashboardStore = create<DashboardState>((set) => ({
  ...DEFAULT_FILTER,
  setScope: (scope) =>
    set((s) => (s.scope === scope ? s : { scope, actorId: null })),
  setActorId: (actorId) => set({ actorId }),
  setRange: (range) => set({ range }),
  reset: () => set(DEFAULT_FILTER),
}));
