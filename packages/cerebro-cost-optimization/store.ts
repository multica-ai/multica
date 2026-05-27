"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "@multica/core/platform";
import { defaultStorage } from "@multica/core/platform";
import {
  COST_SAVING_DEFAULTS,
  type CostSavingKey,
  type CostSavingMode,
} from "./registry";

/**
 * Persists per-workspace cost-saving mode overrides.
 *
 * Only savings whose mode differs from the registry default are stored —
 * defaults are applied at read time. This keeps storage compact and means
 * adding a new saving (with a sensible default) requires no migration.
 *
 * Storage namespacing follows the workspace-aware pattern used by the other
 * fork stores (see `cerebro-feature-flags/store.ts`): the persist key is
 * suffixed with `:<slug>` and rehydrated on workspace switch.
 */
interface CostOptimizationState {
  overrides: Partial<Record<CostSavingKey, CostSavingMode>>;
  /** Set or clear a saving's mode. Pass `undefined` to revert to default. */
  setMode: (key: CostSavingKey, mode: CostSavingMode | undefined) => void;
  /** Replace all overrides at once (used after server hydration). */
  hydrateFromServer: (
    overrides: Partial<Record<CostSavingKey, CostSavingMode>>,
  ) => void;
}

export const useCostOptimizationStore = create<CostOptimizationState>()(
  persist(
    (set) => ({
      overrides: {},
      setMode: (key, mode) =>
        set((s) => {
          if (mode === undefined) {
            const { [key]: _, ...rest } = s.overrides;
            return { overrides: rest };
          }
          return { overrides: { ...s.overrides, [key]: mode } };
        }),
      hydrateFromServer: (overrides) => set({ overrides }),
    }),
    {
      name: "cerebro_cost_optimization",
      storage: createJSONStorage(() =>
        createWorkspaceAwareStorage(defaultStorage),
      ),
    },
  ),
);

registerForWorkspaceRehydration(() =>
  useCostOptimizationStore.persist.rehydrate(),
);

/**
 * Resolve the effective mode of a saving: override if present, otherwise
 * the registry default.
 */
export function useSavingMode(key: CostSavingKey): CostSavingMode {
  return useCostOptimizationStore(
    (s) => s.overrides[key] ?? COST_SAVING_DEFAULTS[key],
  );
}
