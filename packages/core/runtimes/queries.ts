import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const runtimeKeys = {
  all: (wsId: string) => ["runtimes", wsId] as const,
  list: (wsId: string) => [...runtimeKeys.all(wsId), "list"] as const,
  listMine: (wsId: string) => [...runtimeKeys.all(wsId), "list", "mine"] as const,
  usage: (rid: string, days: number) =>
    ["runtimes", "usage", rid, days] as const,
  usageByAgent: (rid: string, days: number) =>
    ["runtimes", "usage", "by-agent", rid, days] as const,
  usageByHour: (rid: string, days: number) =>
    ["runtimes", "usage", "by-hour", rid, days] as const,
  latestVersion: () => ["runtimes", "latestVersion"] as const,
};

// CEREBRO-PATCH(model-registry-pricing-query): FIR-2698 the single-source model registry — a deployment-wide
// singleton, so the key carries no workspace/entity id. Read by
// useSyncModelRegistryPricing to feed the synchronous pricing cache that
// packages/views/runtimes/utils.ts reads during cost estimation.
export const modelRegistryKeys = {
  all: () => ["cerebro", "model-registry"] as const,
};

export function modelRegistryOptions() {
  return queryOptions({
    queryKey: modelRegistryKeys.all(),
    queryFn: () => api.getModelRegistry(),
    staleTime: 5 * 60 * 1000,
  });
}

// Per-runtime usage. Used by the list view (each row pulls its own activity
// sparkline + 30d cost) and by the detail page. TanStack Query naturally
// deduplicates concurrent calls for the same runtime, so multiple components
// observing the same runtimeId share one network request.
export function runtimeUsageOptions(runtimeId: string, days: number) {
  return queryOptions({
    queryKey: runtimeKeys.usage(runtimeId, days),
    queryFn: () => api.getRuntimeUsage(runtimeId, { days }),
    staleTime: 60 * 1000,
  });
}

// Per-agent token totals for one runtime — drives the "Cost by agent" tab
// on the runtime detail page. Server-side aggregation keeps the response
// small (one row per agent) regardless of task volume.
export function runtimeUsageByAgentOptions(runtimeId: string, days: number) {
  return queryOptions({
    queryKey: runtimeKeys.usageByAgent(runtimeId, days),
    queryFn: () => api.getRuntimeUsageByAgent(runtimeId, { days }),
    staleTime: 60 * 1000,
  });
}

// Hourly (0..23) token totals for one runtime — drives the "By hour" tab.
export function runtimeUsageByHourOptions(runtimeId: string, days: number) {
  return queryOptions({
    queryKey: runtimeKeys.usageByHour(runtimeId, days),
    queryFn: () => api.getRuntimeUsageByHour(runtimeId, { days }),
    staleTime: 60 * 1000,
  });
}

export function runtimeListOptions(wsId: string, owner?: "me") {
  return queryOptions({
    queryKey: owner === "me" ? runtimeKeys.listMine(wsId) : runtimeKeys.list(wsId),
    queryFn: () => api.listRuntimes({ workspace_id: wsId, owner }),
  });
}

const GITHUB_RELEASES_URL =
  "https://api.github.com/repos/multica-ai/multica/releases/latest";

export function latestCliVersionOptions() {
  return queryOptions({
    queryKey: runtimeKeys.latestVersion(),
    queryFn: async (): Promise<string | null> => {
      try {
        const resp = await fetch(GITHUB_RELEASES_URL, {
          headers: { Accept: "application/vnd.github+json" },
        });
        if (!resp.ok) return null;
        const data = await resp.json();
        return (data.tag_name as string) ?? null;
      } catch {
        return null;
      }
    },
    staleTime: 10 * 60 * 1000, // 10 minutes
  });
}
