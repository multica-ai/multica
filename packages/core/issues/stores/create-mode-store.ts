"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

/**
 * Remembers the last create-issue mode the user explicitly switched to inside
 * the dialog. Generic "New Issue" entrypoints open manual mode by default;
 * this store only preserves the user's most recent explicit toggle so callers
 * can opt into that state if they ever need it.
 */
export type CreateMode = "agent" | "manual";

interface CreateModeState {
  lastMode: CreateMode;
  setLastMode: (mode: CreateMode) => void;
}

export const useCreateModeStore = create<CreateModeState>()(
  persist(
    (set) => ({
      lastMode: "manual",
      setLastMode: (mode) => set({ lastMode: mode }),
    }),
    {
      name: "multica_create_mode",
      storage: createJSONStorage(() => defaultStorage),
    },
  ),
);
