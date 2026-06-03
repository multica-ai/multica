"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";
import { useModalStore } from "../../modals";
import { configStore } from "../../config";

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

// In generic mode the persisted "agent" preference must be ignored so new
// users landing on the tracker always see the manual form, not the AI agent
// flow. We achieve this by re-hydrating the store with "manual" whenever
// generic mode is active — the `merge` override on the persist middleware
// wins over whatever was previously written to localStorage.
const _isGenericMode = () => configStore.getState().genericMode;

export const useCreateModeStore = create<CreateModeState>()(
  persist(
    (set) => ({
      lastMode: _isGenericMode() ? "manual" : "agent",
      setLastMode: (mode) => set({ lastMode: mode }),
    }),
    {
      name: "multica_create_mode",
      storage: createJSONStorage(() => defaultStorage),
      merge: (persisted, current) => {
        // In generic mode override any persisted "agent" value so the manual
        // form is always the default even for returning users.
        if (_isGenericMode()) {
          return { ...current, ...(persisted as Partial<CreateModeState>), lastMode: "manual" };
        }
        return { ...current, ...(persisted as Partial<CreateModeState>) };
      },
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
  // In generic mode always open the manual form regardless of the persisted
  // preference — the agent quick-create flow is hidden from non-IT users.
  const isGeneric = configStore.getState().genericMode;
  const lastMode = isGeneric ? "manual" : useCreateModeStore.getState().lastMode;
  const modal = lastMode === "manual" ? "create-issue" : "quick-create-issue";
  useModalStore.getState().open(modal, data ?? null);
}
