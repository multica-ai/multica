"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
  defaultStorage,
} from "@multica/core/platform";

// FIR-2669: view preferences for the Runtimes list page. Only column
// visibility so far. Persisted per workspace, per user/device — the same
// workspace-aware storage the agents view store (upstream) uses, so switching
// workspace loads that workspace's own choices. Mirrors the agents pattern so
// the two list pages behave identically.

// User-hideable columns. The runtime name (with its provider logo), the Health
// column (the primary at-a-glance signal, kept on every breakpoint), the owner
// column (auto-shown only when the page has more than one owner), and the row
// actions are structural and stay outside the picker.
export type RuntimeColumnKey = "agents" | "cost" | "cli" | "account";

// Every column visible by default — the picker only ever hides. Jesper asked
// to ADD the Account column, not to trim the existing set, so nothing starts
// hidden (FIR-2669).
export const RUNTIME_DEFAULT_HIDDEN_COLUMNS: RuntimeColumnKey[] = [];

// Order shown in the column picker. Account sits next to CLI so the identity
// columns read together.
export const RUNTIME_COLUMN_KEYS: RuntimeColumnKey[] = [
  "agents",
  "cost",
  "account",
  "cli",
];

export interface RuntimesViewState {
  hiddenColumns: RuntimeColumnKey[];
  toggleColumn: (key: RuntimeColumnKey) => void;
}

const DEFAULTS = {
  hiddenColumns: RUNTIME_DEFAULT_HIDDEN_COLUMNS,
};

export const useRuntimesViewStore = create<RuntimesViewState>()(
  persist(
    (set) => ({
      ...DEFAULTS,
      toggleColumn: (key) =>
        set((state) => ({
          hiddenColumns: state.hiddenColumns.includes(key)
            ? state.hiddenColumns.filter((k) => k !== key)
            : [...state.hiddenColumns, key],
        })),
    }),
    {
      name: "multica_runtimes_view",
      storage: createJSONStorage(() =>
        createWorkspaceAwareStorage(defaultStorage),
      ),
      partialize: (state) => ({ hiddenColumns: state.hiddenColumns }),
      // On rehydrate into a workspace with no persisted value, fall back to
      // defaults instead of leaking the previous workspace's in-memory state.
      merge: (persisted, current) => {
        if (!persisted) return { ...current, ...DEFAULTS };
        const p = persisted as Partial<RuntimesViewState>;
        return {
          ...current,
          ...p,
          hiddenColumns: (p.hiddenColumns ?? DEFAULTS.hiddenColumns).filter(
            (k): k is RuntimeColumnKey => RUNTIME_COLUMN_KEYS.includes(k),
          ),
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useRuntimesViewStore.persist.rehydrate());
