"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";

type GettingStartedState = "active" | "dismissed";

interface GettingStartedStore {
  /** Keyed by workspace id. Absent means the checklist never applies there:
   *  only a workspace this device just onboarded into gets one, so existing
   *  workspaces never sprout a checklist. */
  workspaces: Record<string, GettingStartedState>;
  activate: (wsId: string) => void;
  dismiss: (wsId: string) => void;
}

/** This device's getting-started checklist state per workspace. */
export const useGettingStartedStore = create<GettingStartedStore>()(
  persist(
    (set) => ({
      workspaces: {},
      activate: (wsId) =>
        set((s) =>
          s.workspaces[wsId]
            ? s
            : { workspaces: { ...s.workspaces, [wsId]: "active" } },
        ),
      dismiss: (wsId) =>
        set((s) => ({ workspaces: { ...s.workspaces, [wsId]: "dismissed" } })),
    }),
    {
      name: "multica_getting_started",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ workspaces: state.workspaces }),
      merge: (persisted, current) => {
        const saved = (persisted as { workspaces?: unknown } | undefined)
          ?.workspaces;
        if (!saved || typeof saved !== "object" || Array.isArray(saved)) {
          return current;
        }
        const workspaces: Record<string, GettingStartedState> = {};
        for (const [id, value] of Object.entries(saved)) {
          if (value === "active" || value === "dismissed") workspaces[id] = value;
        }
        return { ...current, workspaces };
      },
    },
  ),
);
