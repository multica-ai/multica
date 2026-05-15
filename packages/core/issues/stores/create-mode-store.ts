"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";
import { useModalStore } from "../../modals";

/**
 * Last create-issue mode the user landed on. Drives the global `c` shortcut
 * and the in-modal mode switch — pressing `c` opens whichever modal the user
 * used last, and the switch button in either modal updates this so the
 * preference sticks.
 *
 * Workspace-agnostic on purpose: the user's mental preference for "how do I
 * file an issue" doesn't change per workspace, so this lives in plain
 * localStorage rather than the workspace-aware StateStorage that scopes
 * per-workspace stores like quick-create-store / draft-store.
 */
export type CreateMode = "agent" | "manual";

interface CreateModeState {
  lastMode: CreateMode;
  setLastMode: (mode: CreateMode) => void;
}

export const useCreateModeStore = create<CreateModeState>()(
  persist(
    (set) => ({
      lastMode: "agent",
      setLastMode: (mode) => set({ lastMode: mode }),
    }),
    {
      name: "multica_create_mode",
      storage: createJSONStorage(() => defaultStorage),
    },
  ),
);

/**
 * Open the create-issue flow in whichever mode the user landed on last.
 * Generic entry points (sidebar button, command palette, `c` shortcut) call
 * this so the persisted preference actually takes effect; entry points that
 * pre-seed manual-only fields (status, parent_issue_id) keep opening
 * "create-issue" directly because agent mode can't honour those seeds.
 */
export function openCreateIssueWithPreference(
  data?: Record<string, unknown> | null,
) {
  const lastMode = useCreateModeStore.getState().lastMode;
  const modal = lastMode === "manual" ? "create-issue" : "quick-create-issue";
  useModalStore.getState().open(modal, data ?? null);
}
