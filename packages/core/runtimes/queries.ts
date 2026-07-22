import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const setupTokenKeys = {
  mint: (wsId: string) => ["setup-token", wsId] as const,
};

// Mints a setup token for the "Connect from the terminal" dialog (MUL-5112).
// Only fires while `enabled` (the dialog is open) so we don't create tokens on
// mount. staleTime sits under the server TTL (SetupTokenTTL, 15m) so a
// long-open dialog re-mints before the rendered command can lapse; gcTime 0
// drops the token the moment the dialog closes. One retry only — the dialog
// falls back to the manual command if minting fails.
export function setupTokenOptions(wsId: string, enabled: boolean) {
  return queryOptions({
    queryKey: setupTokenKeys.mint(wsId),
    queryFn: () => api.mintSetupToken(wsId),
    enabled: enabled && wsId.length > 0,
    staleTime: 10 * 60 * 1000,
    gcTime: 0,
    retry: 1,
  });
}

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
