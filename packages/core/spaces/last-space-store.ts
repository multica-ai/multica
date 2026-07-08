"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../platform/workspace-storage";
import { defaultStorage } from "../platform/storage";

// Per-workspace memory of the last space the user created something in.
// Shared by every creation surface (issue, quick-create, project, autopilot)
// — users don't distinguish "last space for issues" from "last space for
// projects", so one memory backs all of them. Only consulted as a fallback
// when no stronger signal applies (view context, parent issue, single-space
// project); see resolveCreationSpaceId in ./default-space.
interface LastSpaceState {
  lastSpaceId: string | null;
  setLastSpaceId: (id: string | null) => void;
}

export const useLastSpaceStore = create<LastSpaceState>()(
  persist(
    (set) => ({
      lastSpaceId: null,
      setLastSpaceId: (id) => set({ lastSpaceId: id }),
    }),
    {
      name: "multica_last_space",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useLastSpaceStore.persist.rehydrate());
