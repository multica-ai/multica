import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const runtimeKeys = {
  all: (wsId: string) => ["runtimes", wsId] as const,
  list: (wsId: string) => [...runtimeKeys.all(wsId), "list"] as const,
  listMine: (wsId: string) => [...runtimeKeys.all(wsId), "list", "mine"] as const,
  // Setup uses POST to mint a secret, so these keys must not sit below
  // `all(wsId)`: daemon events routinely invalidate that prefix and would
  // otherwise create a fresh token behind the user's back.
  setupCreate: (wsId: string) => ["runtime-setup", wsId, "create"] as const,
  setupStatus: (wsId: string, sessionId: string) =>
    ["runtime-setup", wsId, sessionId] as const,
  usage: (rid: string, days: number, tz: string) =>
    ["runtimes", "usage", rid, days, tz] as const,
  usageByAgent: (rid: string, days: number, tz: string) =>
    ["runtimes", "usage", "by-agent", rid, days, tz] as const,
  // by-hour now follows the viewer's tz, like the other reports.
  usageByHour: (rid: string, days: number, tz: string) =>
    ["runtimes", "usage", "by-hour", rid, days, tz] as const,
};

export function runtimeSetupCreateOptions(wsId: string) {
  return queryOptions({
    queryKey: runtimeKeys.setupCreate(wsId),
    queryFn: () => api.createRuntimeSetupSession(wsId),
    staleTime: Infinity,
    gcTime: 0,
    retry: false,
    refetchOnMount: false,
    refetchOnReconnect: false,
    refetchOnWindowFocus: false,
  });
}

export function runtimeSetupStatusOptions(
  wsId: string,
  sessionId: string,
) {
  return queryOptions({
    queryKey: runtimeKeys.setupStatus(wsId, sessionId),
    queryFn: () => api.getRuntimeSetupSession(wsId, sessionId),
    enabled: sessionId !== "",
    // Keep the polling fallback alive for the connected-with-zero-runtimes
    // state: installing an agent CLI later should advance this same guide.
    refetchInterval: (query) =>
      (query.state.data?.runtime_count ?? 0) > 0 ? false : 2_000,
  });
}

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

export function runtimeUsageByHourOptions(runtimeId: string, days: number, tz: string) {
  return queryOptions({
    queryKey: runtimeKeys.usageByHour(runtimeId, days, tz),
    queryFn: () => api.getRuntimeUsageByHour(runtimeId, { days, tz }),
    staleTime: 60 * 1000,
  });
}

export function runtimeListOptions(wsId: string, owner?: "me") {
  return queryOptions({
    queryKey: owner === "me" ? runtimeKeys.listMine(wsId) : runtimeKeys.list(wsId),
    queryFn: () => api.listRuntimes({ workspace_id: wsId, owner }),
  });
}
