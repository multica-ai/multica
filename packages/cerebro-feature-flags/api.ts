"use client";

import { useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { CEREBRO_FLAG_DEFAULTS, type CerebroFlagKey } from "./registry";
import { useCerebroFeatureFlagsStore, useFlagValue } from "./store";

const featureFlagKeys = {
  all: (wsId: string) => ["cerebro-feature-flags", wsId] as const,
};

/**
 * Fetch the current workspace's feature-flag overrides for the signed-in user.
 *
 * On success, the Zustand store is hydrated so synchronous selectors
 * (`useFeatureFlag`) see the same data without going through the query
 * cache. The query cache remains the canonical source for "is this loaded /
 * loading / errored", per the project's state-management rules.
 */
export function useFeatureFlagsQuery() {
  const wsId = useWorkspaceId();
  const query = useQuery({
    queryKey: featureFlagKeys.all(wsId),
    queryFn: () => api.listFeatureFlags(wsId),
  });

  const hydrate = useCerebroFeatureFlagsStore((s) => s.hydrateFromServer);
  const data = query.data;
  useEffect(() => {
    if (!data) return;
    // Narrow the server response (string keys) down to the registry keys.
    const filtered: Partial<Record<CerebroFlagKey, boolean>> = {};
    for (const key of Object.keys(CEREBRO_FLAG_DEFAULTS) as CerebroFlagKey[]) {
      const value = data.overrides[key];
      if (typeof value === "boolean") filtered[key] = value;
    }
    hydrate(filtered);
  }, [data, hydrate]);

  return query;
}

/**
 * Toggle a single flag. Optimistic: the Zustand store is updated immediately
 * so all `useFeatureFlag` consumers re-render. On failure, the previous
 * override (or absence) is restored.
 */
export function useSetFeatureFlagMutation() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

  return useMutation({
    mutationFn: ({ key, enabled }: { key: CerebroFlagKey; enabled: boolean }) =>
      api.setFeatureFlag(wsId, key, enabled),
    onMutate: ({ key, enabled }) => {
      const store = useCerebroFeatureFlagsStore.getState();
      const previous = store.overrides[key];
      store.setFlag(key, enabled);
      return { previous };
    },
    onError: (_err, { key }, context) => {
      const store = useCerebroFeatureFlagsStore.getState();
      store.setFlag(key, context?.previous);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: featureFlagKeys.all(wsId) });
    },
  });
}

/**
 * Primary consumer hook. Reads from the Zustand store (synchronous, no
 * Suspense). Triggers `useFeatureFlagsQuery` so the store is hydrated from
 * the server on first use; subsequent calls in the same workspace share
 * the cached query.
 */
export function useFeatureFlag(key: CerebroFlagKey): boolean {
  useFeatureFlagsQuery();
  return useFlagValue(key);
}
