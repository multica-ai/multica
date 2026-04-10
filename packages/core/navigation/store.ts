"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createPersistStorage } from "../platform/persist-storage";
import { defaultStorage } from "../platform/storage";

const EXCLUDED_PREFIXES = ["/login", "/pair/"];

interface NavigationState {
  lastPath: string;
  onPathChange: (path: string) => void;
}

export const useNavigationStore = create<NavigationState>()(
  persist(
    (set) => ({
      lastPath: "/issues",

      onPathChange: (path: string) => {
        if (!EXCLUDED_PREFIXES.some((prefix) => path.startsWith(prefix))) {
          set({ lastPath: path });
        }
      },
    }),
    {
      name: "multica_navigation",
      storage: createJSONStorage(() => createPersistStorage(defaultStorage)),
      partialize: (state) => ({ lastPath: state.lastPath }),
    }
  )
);
