"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";
import { useModalStore } from "../../modals";

const CREATE_MODE_STORAGE_KEY = "multica_create_mode";

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

function isCreateMode(value: unknown): value is CreateMode {
  return value === "agent" || value === "manual";
}

function serializeCreateMode(mode: CreateMode) {
  return JSON.stringify({ state: { lastMode: mode }, version: 0 });
}

export function getPersistedCreateMode(): CreateMode {
  const stored = defaultStorage.getItem(CREATE_MODE_STORAGE_KEY);
  if (!stored) {
    useCreateModeStore.setState({ lastMode: "manual" });
    defaultStorage.setItem(CREATE_MODE_STORAGE_KEY, serializeCreateMode("manual"));
    return "manual";
  }

  try {
    const parsed = JSON.parse(stored) as {
      state?: { lastMode?: unknown };
    };
    const lastMode = parsed.state?.lastMode;
    if (isCreateMode(lastMode)) return lastMode;
  } catch {
    // Fall through to the manual default for corrupt persisted state.
  }

  useCreateModeStore.setState({ lastMode: "manual" });
  defaultStorage.setItem(CREATE_MODE_STORAGE_KEY, serializeCreateMode("manual"));
  return "manual";
}

export const useCreateModeStore = create<CreateModeState>()(
  persist(
    (set) => ({
      lastMode: "manual",
      setLastMode: (mode) => set({ lastMode: mode }),
    }),
    {
      name: CREATE_MODE_STORAGE_KEY,
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
