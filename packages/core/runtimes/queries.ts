import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const runtimeKeys = {
  all: (wsId: string) => ["runtimes", wsId] as const,
  list: (wsId: string) => [...runtimeKeys.all(wsId), "list"] as const,
  listMine: (wsId: string) => [...runtimeKeys.all(wsId), "list", "mine"] as const,
  usage: (rid: string, days: number, tz: string) =>
    ["runtimes", "usage", rid, days, tz] as const,
  usageByAgent: (rid: string, days: number, tz: string) =>
    ["runtimes", "usage", "by-agent", rid, days, tz] as const,
  // by-hour now follows the viewer's tz, like the other reports.
  usageByHour: (rid: string, days: number, tz: string) =>
    ["runtimes", "usage", "by-hour", rid, days, tz] as const,
  providerUsage: (wsId: string, runtimeId: string) =>
    [...runtimeKeys.all(wsId), "provider-usage", runtimeId] as const,
  providerUsageList: (wsId: string, runtimeIds: readonly string[]) =>
    [...runtimeKeys.all(wsId), "provider-usage-list", runtimeIds] as const,
};

// `tz` is the viewer's IANA name — all reports follow the viewer's tz.
export function runtimeUsageOptions(
  runtimeId: string,
  days: number,
  tz: string,
) {
  return queryOptions({
    queryKey: runtimeKeys.usage(runtimeId, days, tz),
    queryFn: () => api.getRuntimeUsage(runtimeId, { days, tz }),
    staleTime: 60 * 1000,
  });
}

export function runtimeUsageByAgentOptions(
  runtimeId: string,
  days: number,
  tz: string,
) {
  return queryOptions({
    queryKey: runtimeKeys.usageByAgent(runtimeId, days, tz),
    queryFn: () => api.getRuntimeUsageByAgent(runtimeId, { days, tz }),
    staleTime: 60 * 1000,
  });
}

export function runtimeProviderUsageOptions(wsId: string, runtimeId: string) {
  return queryOptions({
    queryKey: runtimeKeys.providerUsage(wsId, runtimeId),
    queryFn: () => api.getRuntimeProviderUsage(runtimeId),
    enabled: wsId.length > 0 && runtimeId.length > 0,
    staleTime: 60 * 1000,
  });
}

export function runtimeProviderUsageListOptions(
  wsId: string,
  runtimeIds: readonly string[],
) {
  const ids = [...runtimeIds].sort();
  return queryOptions({
    queryKey: runtimeKeys.providerUsageList(wsId, ids),
    queryFn: () => api.listRuntimeProviderUsage(ids),
    enabled: wsId.length > 0 && ids.length > 0,
    staleTime: 60 * 1000,
  });
}

export function runtimeUsageByHourOptions(runtimeId: string, days: number, tz: string) {
  return queryOptions({
    queryKey: runtimeKeys.usageByHour(runtimeId, days, tz),
    queryFn: () => api.getRuntimeUsageByHour(runtimeId, { days, tz }),
    staleTime: 60 * 1000,
  });
}

/**
 * `wsSlug` targets a workspace other than the active one. The server resolves
 * the workspace from the slug header before the `workspace_id` param, so the
 * param alone cannot reach a workspace the app has not navigated to — which is
 * exactly the create-workspace flow's situation.
 */
export function runtimeListOptions(wsId: string, owner?: "me", wsSlug?: string) {
  return queryOptions({
    queryKey: owner === "me" ? runtimeKeys.listMine(wsId) : runtimeKeys.list(wsId),
    queryFn: () => api.listRuntimes({ workspace_id: wsId, owner }, wsSlug),
  });
}
