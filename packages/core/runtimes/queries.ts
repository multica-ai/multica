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
  latestManifest: () => ["runtimes", "latestManifest"] as const,
};

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

export function latestCliManifestOptions() {
  return queryOptions({
    queryKey: runtimeKeys.latestManifest(),
    queryFn: () => api.getCLIUpdateManifest(),
    staleTime: 10 * 60 * 1000, // 10 minutes
  });
}

export function latestCliVersionOptions() {
  return queryOptions({
    queryKey: [...runtimeKeys.latestManifest(), "version"] as const,
    queryFn: async (): Promise<string | null> =>
      (await api.getCLIUpdateManifest()).version ?? null,
    staleTime: 10 * 60 * 1000, // 10 minutes
  });
}
