"use client";

import { create } from "zustand";
import { createJSONStorage, persist, type StateStorage } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";

// User-supplied pricing for models we don't ship a maintained rate for.
// We can't track every model OpenRouter / Codex / Hermes / Kimi etc. release,
// so the empty-state diagnostic lets users fill in their own rates. Stored
// globally (not workspace-scoped) because the rate of `gpt-5.5-mini` is the
// same regardless of which workspace you're viewing.
export interface CustomModelPricing {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

export interface CustomPricingState {
  pricings: Record<string, CustomModelPricing>;
  setCustomPricing: (model: string, pricing: CustomModelPricing) => void;
  removeCustomPricing: (model: string) => void;
}

// StorageAdapter (sync getItem returning string | null) is a structural subset
// of zustand's StateStorage, so it can be handed in directly via cast.
const stateStorage = defaultStorage as unknown as StateStorage;

export const useCustomPricingStore = create<CustomPricingState>()(
  persist(
    (set) => ({
      pricings: {},
      setCustomPricing: (model, pricing) =>
        set((state) => ({
          pricings: { ...state.pricings, [model]: pricing },
        })),
      removeCustomPricing: (model) =>
        set((state) => {
          if (!(model in state.pricings)) return state;
          const next = { ...state.pricings };
          delete next[model];
          return { pricings: next };
        }),
    }),
    {
      name: "multica_runtime_custom_pricing",
      storage: createJSONStorage(() => stateStorage),
    },
  ),
);

// Vanilla accessor for non-React callers (the `resolvePricing` helper in
// packages/views/runtimes/utils.ts reads from here during cost estimation).
export function getCustomPricing(model: string): CustomModelPricing | undefined {
  return useCustomPricingStore.getState().pricings[model];
}
