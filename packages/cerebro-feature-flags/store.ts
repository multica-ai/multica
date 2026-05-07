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
  /** Set or clear a flag override. Pass `undefined` to revert to default. */
  setFlag: (key: CerebroFlagKey, enabled: boolean | undefined) => void;
  /** Replace all overrides at once (used after server hydration). */
  hydrateFromServer: (overrides: Partial<Record<CerebroFlagKey, boolean>>) => void;
}

export const useCerebroFeatureFlagsStore = create<CerebroFeatureFlagsState>()(
  persist(
    (set) => ({
      overrides: {},
      setFlag: (key, enabled) =>
        set((s) => {
          if (enabled === undefined) {
            const { [key]: _, ...rest } = s.overrides;
            return { overrides: rest };
          }
          return { overrides: { ...s.overrides, [key]: enabled } };
        }),
      hydrateFromServer: (overrides) => set({ overrides }),
    }),
    {
      name: "cerebro_feature_flags",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() =>
  useCerebroFeatureFlagsStore.persist.rehydrate(),
);

/**
 * Resolve the effective value of a flag: override if present, otherwise
 * the registry default.
 */
export function useFlagValue(key: CerebroFlagKey): boolean {
  return useCerebroFeatureFlagsStore(
    (s) => s.overrides[key] ?? CEREBRO_FLAG_DEFAULTS[key],
  );
}
