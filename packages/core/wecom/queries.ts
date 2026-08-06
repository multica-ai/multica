import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/** Query key namespace for WeCom installation data. Realtime sync
 * invalidates `installations(wsId)` on `wecom_installation:*` events. */
export const wecomKeys = {
  all: (wsId: string) => ["wecom", wsId] as const,
  installations: (wsId: string) => [...wecomKeys.all(wsId), "installations"] as const,
};

export const wecomInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: wecomKeys.installations(wsId),
    queryFn: () => api.listWecomInstallations(wsId),
    enabled: !!wsId,
  });
