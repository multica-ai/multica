"use client";

import { create } from "zustand";
import { persist } from "zustand/middleware";

const EXCLUDED_PREFIXES = ["/login", "/pair/"];

function shouldPersistNavigationPath(path: string): boolean {
  if (EXCLUDED_PREFIXES.some((prefix) => path.startsWith(prefix))) {
    return false;
  }

  // Keep the last non-detail path so issue detail pages can return to their source surface.
  if (path.startsWith("/issues/") && path !== "/issues") {
    return false;
  }

  return true;
}

interface NavigationState {
  lastPath: string;
  onPathChange: (path: string) => void;
}

export const useNavigationStore = create<NavigationState>()(
  persist(
    (set) => ({
      lastPath: "/issues",

      onPathChange: (path: string) => {
        if (shouldPersistNavigationPath(path)) {
          set({ lastPath: path });
        }
      },
    }),
    {
      name: "multica_navigation",
      partialize: (state) => ({ lastPath: state.lastPath }),
    }
  )
);
