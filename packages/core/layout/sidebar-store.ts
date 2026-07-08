"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../platform/workspace-storage";
import { defaultStorage } from "../platform/storage";

// Sidebar group collapse state, persisted per workspace (same
// workspace-aware storage the quick-create prefs use). Keys are group ids:
// "pinned", "workspace", "more", "spaces", and `space:{spaceId}` for each space
// group. Absent key = expanded — groups default open so a fresh workspace
// shows its whole structure.
interface SidebarState {
  collapsed: Record<string, boolean>;
  setGroupCollapsed: (key: string, collapsed: boolean) => void;
}

export const useSidebarStore = create<SidebarState>()(
  persist(
    (set) => ({
      collapsed: {},
      setGroupCollapsed: (key, collapsed) =>
        set((s) => ({ collapsed: { ...s.collapsed, [key]: collapsed } })),
    }),
    {
      name: "multica_sidebar",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useSidebarStore.persist.rehydrate());
