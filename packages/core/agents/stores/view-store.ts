"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

export type AgentsScope = "mine" | "all";

export interface AgentsViewState {
  scope: AgentsScope;
  setScope: (scope: AgentsScope) => void;
}

export const useAgentsViewStore = create<AgentsViewState>()(
  persist(
    (set) => ({
      scope: "all",
      setScope: (scope) => set({ scope }),
    }),
    {
      name: "multica_agents_view",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({ scope: state.scope }),
      // On rehydrate, if the new workspace has no persisted value, reset to
      // the default "all" instead of leaving the previous workspace's in-
      // memory scope in place. Default merge keeps current state when
      // persisted is undefined, which would leak the previous workspace's
      // scope across workspaces. Fork #88 keeps the default "all".
      merge: (persisted, current) => {
        if (!persisted) return { ...current, scope: "all" };
        return { ...current, ...(persisted as Partial<AgentsViewState>) };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useAgentsViewStore.persist.rehydrate());
