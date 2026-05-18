"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

export type SquadsScope = "mine" | "all";

export interface SquadsViewState {
  scope: SquadsScope;
  setScope: (scope: SquadsScope) => void;
}

export const useSquadsViewStore = create<SquadsViewState>()(
  persist(
    (set) => ({
      scope: "mine",
      setScope: (scope) => set({ scope }),
    }),
    {
      name: "multica_squads_view",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({ scope: state.scope }),
      // On rehydrate, if the new workspace has no persisted value, reset to
      // the default "mine" instead of leaving the previous workspace's in-
      // memory scope in place. Default merge keeps current state when
      // persisted is undefined, which would leak "all" across workspaces.
      merge: (persisted, current) => {
        if (!persisted) return { ...current, scope: "mine" };
        return { ...current, ...(persisted as Partial<SquadsViewState>) };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useSquadsViewStore.persist.rehydrate());
