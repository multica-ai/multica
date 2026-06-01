"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "@multica/core/platform";
import { defaultStorage } from "@multica/core/platform";
import { CEREBRO_FLAG_DEFAULTS, type CerebroFlagKey } from "./registry";

/**
 * Persists per-workspace, per-user feature-flag overrides.
 *
 * Only flags toggled AWAY from their default are stored — defaults are
 * applied at read time. This keeps storage compact and means adding a new
 * flag (with a sensible default) requires no migration.
 *
 * Storage namespacing follows the workspace-aware pattern used by other
 * fork stores (see `comment-collapse-store.ts`): the persist key is
 * suffixed with `:<slug>` and rehydrated on workspace switch.
 */
interface CerebroFeatureFlagsState {
  overrides: Partial<Record<CerebroFlagKey, boolean>>;
  /**
   * Owner-set workspace-level overrides. They apply to every member and win
   * over personal overrides when the key is also `locked`. Always hydrated
   * from the server (authoritative), never persisted personally.
   */
  workspaceOverrides: Partial<Record<CerebroFlagKey, boolean>>;
  /** Flag keys whose workspace value members may NOT override personally. */
  locked: Partial<Record<CerebroFlagKey, boolean>>;
  /** Set or clear a personal flag override. Pass `undefined` to revert to default. */
  setFlag: (key: CerebroFlagKey, enabled: boolean | undefined) => void;
  /** Replace all override state at once (used after server hydration). */
  hydrateFromServer: (data: {
    overrides: Partial<Record<CerebroFlagKey, boolean>>;
    workspaceOverrides: Partial<Record<CerebroFlagKey, boolean>>;
    locked: Partial<Record<CerebroFlagKey, boolean>>;
  }) => void;
}

export const useCerebroFeatureFlagsStore = create<CerebroFeatureFlagsState>()(
  persist(
    (set) => ({
      overrides: {},
      workspaceOverrides: {},
      locked: {},
      setFlag: (key, enabled) =>
        set((s) => {
          if (enabled === undefined) {
            const { [key]: _, ...rest } = s.overrides;
            return { overrides: rest };
          }
          return { overrides: { ...s.overrides, [key]: enabled } };
        }),
      hydrateFromServer: ({ overrides, workspaceOverrides, locked }) =>
        set({ overrides, workspaceOverrides, locked }),
    }),
    {
      name: "cerebro_feature_flags",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      // Persist only the personal overrides locally; the authoritative
      // workspace overrides + lock state are always re-fetched from the server.
      partialize: (s) => ({ overrides: s.overrides }),
    },
  ),
);

registerForWorkspaceRehydration(() =>
  useCerebroFeatureFlagsStore.persist.rehydrate(),
);

/**
 * Pure flag resolution. Precedence:
 *   1. A LOCKED workspace override wins outright — members cannot override it.
 *   2. Otherwise a personal override wins.
 *   3. Otherwise an unlocked workspace override (a soft workspace default).
 *   4. Otherwise the registry default.
 * Exported so the precedence can be unit-tested without rendering a hook.
 */
export function resolveFlag(
  key: CerebroFlagKey,
  overrides: Partial<Record<CerebroFlagKey, boolean>>,
  workspaceOverrides: Partial<Record<CerebroFlagKey, boolean>>,
  locked: Partial<Record<CerebroFlagKey, boolean>>,
): boolean {
  const ws = workspaceOverrides[key];
  if (locked[key] && ws !== undefined) return ws;
  return overrides[key] ?? ws ?? CEREBRO_FLAG_DEFAULTS[key];
}

/** Resolve the effective value of a flag (see {@link resolveFlag}). */
export function useFlagValue(key: CerebroFlagKey): boolean {
  return useCerebroFeatureFlagsStore((s) =>
    resolveFlag(key, s.overrides, s.workspaceOverrides, s.locked),
  );
}

/**
 * Whether the workspace owner has locked this flag — members see the personal
 * toggle disabled and cannot override the workspace value.
 */
export function useFlagLocked(key: CerebroFlagKey): boolean {
  return useCerebroFeatureFlagsStore((s) => !!s.locked[key]);
}

/** The workspace-level value an owner has set, or undefined if none. */
export function useWorkspaceFlagValue(key: CerebroFlagKey): boolean | undefined {
  return useCerebroFeatureFlagsStore((s) => s.workspaceOverrides[key]);
}
