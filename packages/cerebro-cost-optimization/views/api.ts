"use client";

import { useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  COST_SAVING_DEFAULTS,
  isDefaultMode,
  type CostSavingKey,
  type CostSavingMode,
} from "../registry";
import { parseDashboardResponse } from "../dashboard";
import { useCostOptimizationStore } from "../store";

const costOptimizationKeys = {
  all: (wsId: string) => ["cerebro-cost-optimization", wsId] as const,
  dashboard: (wsId: string) =>
    ["cerebro-cost-optimization", wsId, "dashboard"] as const,
};

/**
 * Fetch the per-saving estimated-vs-measured dashboard for the workspace. The
 * client returns the body as `unknown`; the contract is validated here with
 * {@link parseDashboardResponse} so a drifted shape degrades to an empty
 * dashboard instead of throwing into the settings page.
 */
export function useCostOptimizationDashboardQuery() {
  const wsId = useWorkspaceId();
  return useQuery({
    queryKey: costOptimizationKeys.dashboard(wsId),
    queryFn: async () =>
      parseDashboardResponse(await api.getCostOptimizationDashboard(wsId)),
  });
}

/**
 * Fetch the current workspace's saving-mode overrides.
 *
 * On success the Zustand store is hydrated so synchronous selectors
 * (`useSavingMode`) see the same data without going through the query cache.
 * The query cache stays the canonical "loaded / loading / errored" source, per
 * the project's state-management rules. Mirrors `useFeatureFlagsQuery`.
 */
export function useCostOptimizationQuery() {
  const wsId = useWorkspaceId();
  const query = useQuery({
    queryKey: costOptimizationKeys.all(wsId),
    queryFn: () => api.listCostOptimization(wsId),
  });

  const hydrate = useCostOptimizationStore((s) => s.hydrateFromServer);
  const data = query.data;
  useEffect(() => {
    if (!data) return;
    // Narrow the server response (string values) down to known saving keys and
    // valid modes; ignore anything the server returns that this build predates.
    const filtered: Partial<Record<CostSavingKey, CostSavingMode>> = {};
    for (const key of Object.keys(COST_SAVING_DEFAULTS) as CostSavingKey[]) {
      const value = data.overrides[key];
      if (value === "off" || value === "shadow" || value === "on") {
        filtered[key] = value;
      }
    }
    hydrate(filtered);
  }, [data, hydrate]);

  return query;
}

/**
 * Set a saving's mode. Optimistic: the Zustand store updates immediately so all
 * `useSavingMode` consumers re-render; on failure the previous override (or its
 * absence) is restored.
 *
 * Persistence mirrors the store's "only store deviations" model: selecting the
 * registry default clears the server override (DELETE), any other mode writes
 * it (PUT). See {@link isDefaultMode}.
 */
export function useSetSavingModeMutation() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

  return useMutation({
    mutationFn: ({ key, mode }: { key: CostSavingKey; mode: CostSavingMode }) =>
      isDefaultMode(key, mode)
        ? api.clearCostOptimization(wsId, key)
        : api.setCostOptimization(wsId, key, mode),
    onMutate: ({ key, mode }) => {
      const store = useCostOptimizationStore.getState();
      const previous = store.overrides[key];
      store.setMode(key, isDefaultMode(key, mode) ? undefined : mode);
      return { previous };
    },
    onError: (_err, { key }, context) => {
      useCostOptimizationStore.getState().setMode(key, context?.previous);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: costOptimizationKeys.all(wsId) });
    },
  });
}
