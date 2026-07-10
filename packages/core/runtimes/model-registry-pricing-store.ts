"use client";

import { create } from "zustand";

// CEREBRO-PATCH(model-registry-pricing-store): FIR-2698 in-memory read-through
// cache of the single-source model registry's prices, fed by
// useSyncModelRegistryPricing (see
// use-sync-model-registry-pricing.ts). This replaced five hand-maintained
// price tables that drifted apart (the FIR-2689 root cause) — the frontend's
// own copy was one of them (packages/views/runtimes/utils.ts MODEL_PRICING).
//
// Deliberately NOT persisted: the registry is a deployment-wide server
// document, not user preference, so there is nothing worth keeping across
// reloads — a fresh fetch is always the current source of truth. Mirrors the
// shape (and the vanilla accessor pattern) of custom-pricing-store.ts, which
// stays as the fallback for models the registry hasn't curated yet.
export interface RegistryModelPricing {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

interface ModelRegistryPricingState {
  pricings: Record<string, RegistryModelPricing>;
  setPricings: (pricings: Record<string, RegistryModelPricing>) => void;
}

export const useModelRegistryPricingStore = create<ModelRegistryPricingState>()(
  (set) => ({
    pricings: {},
    setPricings: (pricings) => set({ pricings }),
  }),
);

// Vanilla accessor for non-React callers (the `resolvePricing` helper in
// packages/views/runtimes/utils.ts reads from here during cost estimation).
export function getRegistryPricing(model: string): RegistryModelPricing | undefined {
  return useModelRegistryPricingStore.getState().pricings[model];
}
