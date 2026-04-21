"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { isGlobalPath } from "../paths";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../platform/workspace-storage";
import { defaultStorage } from "../platform/storage";

// Paths that should not be persisted as "last visited":
//  - Global / pre-workspace routes (see paths.isGlobalPath)
//  - Pair flow (/pair/)
const EXCLUDED_PREFIXES = ["/pair/"];

interface NavigationState {
  lastPath: string | null;
  onPathChange: (path: string) => void;
}

export const useNavigationStore = create<NavigationState>()(
  persist(
    (set) => ({
      lastPath: null,
      onPathChange: (path: string) => {
        if (
          !isGlobalPath(path) &&
          !EXCLUDED_PREFIXES.some((prefix) => path.startsWith(prefix))
        ) {
          set({ lastPath: path });
        }
      },
    }),
    {
      name: "multica_navigation",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({ lastPath: state.lastPath }),
    },
  ),
);

// Workspace-aware: re-read lastPath when current workspace changes.
registerForWorkspaceRehydration(() => useNavigationStore.persist.rehydrate());
