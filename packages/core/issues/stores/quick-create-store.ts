"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

// Per-workspace memory of the last agent and project the user picked in the
// Quick Create modal. Defaulted to those values on next open so frequent
// users skip the pickers entirely — without this, anyone targeting a single
// project ends up retyping "in project A" on every prompt. Persisted with
// the workspace-aware StateStorage so switching workspaces shows the right
// default automatically. Per-user scoping comes for free from localStorage
// being browser-profile-local — matches how draft-store /
// issues-scope-store / comment-collapse-store already namespace themselves.
interface QuickCreateState {
  lastAgentId: string | null;
  setLastAgentId: (id: string | null) => void;
  lastProjectId: string | null;
  setLastProjectId: (id: string | null) => void;
  prompt: string;
  setPrompt: (prompt: string) => void;
  clearPrompt: () => void;
  keepOpen: boolean;
  setKeepOpen: (v: boolean) => void;
}

export const useQuickCreateStore = create<QuickCreateState>()(
  persist(
    (set) => ({
      lastAgentId: null,
      setLastAgentId: (id) => set({ lastAgentId: id }),
      lastProjectId: null,
      setLastProjectId: (id) => set({ lastProjectId: id }),
      prompt: "",
      setPrompt: (prompt) => set({ prompt }),
      clearPrompt: () => set({ prompt: "" }),
      keepOpen: false,
      setKeepOpen: (v) => set({ keepOpen: v }),
    }),
    {
      name: "multica_quick_create",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useQuickCreateStore.persist.rehydrate());
