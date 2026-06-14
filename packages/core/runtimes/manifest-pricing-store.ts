"use client";

import { create } from "zustand";

// In-memory pricing populated by the React Query hook that fetches
// runtimes from the daemon. External runtime extensions ship a `pricing`
// map in their `runtime.json`; the manifest is the source of truth, so
// this store mirrors whatever the daemon last reported instead of
// persisting to localStorage. (A persisted layer would risk showing
// stale rates if the user updates their manifest.)
//
// Layered on top of the curated `MODEL_PRICING` table and below the
// user-supplied `CustomPricingStore`:
//
//   1. Built-in MODEL_PRICING       (curated, ships with the app)
//   2. CustomPricingStore           (user override via empty-state UI)
//   3. ManifestPricingStore         (this file — runtime.json `pricing`)
//
// The lookup helper in `packages/views/runtimes/utils.ts` walks them in
// that order. A user-set custom rate beats a manifest rate so an
// outdated manifest can be corrected without uninstalling the runtime.

import type { RuntimeModelPricing } from "../types/agent";

export interface ManifestPricingState {
  // Keyed by model id (the key the daemon reports — usually the same id
  // used in the model picker). When two manifests share a model id (rare
  // but possible if two runtimes both expose, say, `claude-sonnet-4`),
  // last-write-wins; the daemon already enforces uniqueness on the
  // provider key, so collisions across manifests are unusual.
  pricings: Record<string, RuntimeModelPricing>;
  setManifestPricings: (
    next: Record<string, RuntimeModelPricing>,
  ) => void;
  mergeManifestPricings: (
    next: Record<string, RuntimeModelPricing>,
  ) => void;
}

export const useManifestPricingStore = create<ManifestPricingState>()((
  set,
) => ({
  pricings: {},
  setManifestPricings: (next) => set({ pricings: { ...next } }),
  mergeManifestPricings: (next) =>
    set((state) => ({ pricings: { ...state.pricings, ...next } })),
}));

// Vanilla accessor for non-React callers (the `resolvePricing` helper in
// packages/views/runtimes/utils.ts reads from here during cost estimation).
// Returns undefined when the manifest layer has nothing for this model id;
// callers fall through to the curated table or the custom override.
export function getManifestPricing(
  model: string,
): RuntimeModelPricing | undefined {
  return useManifestPricingStore.getState().pricings[model];
}
