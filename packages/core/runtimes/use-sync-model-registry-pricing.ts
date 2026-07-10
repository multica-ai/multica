"use client";

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { modelRegistryOptions } from "./queries";
import { useModelRegistryPricingStore } from "./model-registry-pricing-store";

// CEREBRO-PATCH(model-registry-pricing-sync): FIR-2698 loads the
// single-source model registry once per session and republishes its prices
// into the synchronous pricing cache
// (model-registry-pricing-store.ts) that packages/views/runtimes/utils.ts
// reads during cost estimation. Mount once near the app root (DashboardGuard)
// so pricing is available everywhere token usage renders, without every
// call site needing its own query.
//
// `enabled` gates the request on auth being resolved — DashboardGuard calls
// this before its own auth check (hooks can't be called conditionally), so
// without the gate this would fire an unauthenticated request on every
// logged-out page load.
export function useSyncModelRegistryPricing(enabled: boolean = true) {
  const { data } = useQuery({ ...modelRegistryOptions(), enabled });
  const setPricings = useModelRegistryPricingStore((s) => s.setPricings);

  useEffect(() => {
    if (!data) return;
    const pricings: Record<
      string,
      { input: number; output: number; cacheRead: number; cacheWrite: number }
    > = {};
    for (const [id, entry] of Object.entries(data.snapshot.models)) {
      pricings[id] = {
        input: entry.input_usd_per_mtok,
        output: entry.output_usd_per_mtok,
        cacheRead: entry.cache_read_usd_per_mtok,
        cacheWrite: entry.cache_write_usd_per_mtok,
      };
    }
    setPricings(pricings);
  }, [data, setPricings]);
}
