import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";

// Settings stays visible so every hidden item can be restored.
export const SIDEBAR_ITEMS = [
  "inbox",
  "my_issues",
  "chat",
  "issues",
  "projects",
  "autopilots",
  "agents",
  "squads",
  "skills",
  "runtimes",
  "usage",
] as const;

export type SidebarItem = (typeof SIDEBAR_ITEMS)[number];

interface SidebarPreferencesState {
  hiddenItems: SidebarItem[];
  setItemVisible: (item: SidebarItem, visible: boolean) => void;
}

/** Sidebar visibility is a device preference shared across workspaces. */
export const useSidebarPreferencesStore = create<SidebarPreferencesState>()(
  persist(
    (set) => ({
      hiddenItems: [],
      setItemVisible: (item, visible) => set((state) => ({
        hiddenItems: visible
          ? state.hiddenItems.filter((hidden) => hidden !== item)
          : [...new Set([...state.hiddenItems, item])],
      })),
    }),
    {
      name: "multica_sidebar_preferences",
      storage: createJSONStorage(() => defaultStorage),
      partialize: ({ hiddenItems }) => ({ hiddenItems }),
      merge: (persisted, current) => {
        const stored = persisted && typeof persisted === "object" &&
          "hiddenItems" in persisted && Array.isArray(persisted.hiddenItems)
          ? persisted.hiddenItems
          : [];
        return {
          ...current,
          hiddenItems: [...new Set(stored.filter((item): item is SidebarItem =>
            SIDEBAR_ITEMS.some((key) => key === item),
          ))],
        };
      },
    },
  ),
);
