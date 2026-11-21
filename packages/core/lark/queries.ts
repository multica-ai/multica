import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/** Query key namespace for everything Lark-installation-related. Realtime
 * sync invalidates `installations(wsId)` on `lark_installation:*` events
 * so the Settings panel updates without a refetch. */
export const larkKeys = {
  all: (wsId: string) => ["lark", wsId] as const,
  installations: (wsId: string) => [...larkKeys.all(wsId), "installations"] as const,
  sendObservation: (wsId: string, id: string, revision: string) => [...larkKeys.all(wsId), "send-observation", id, revision] as const,
};

export const larkSendObservationOptions = (wsId: string, id: string, revision = "") =>
  queryOptions({
    // Installation updates include rebinds. Do not reuse the previous
    // binding's cached evidence while the new request is pending.
    queryKey: larkKeys.sendObservation(wsId, id, revision),
    queryFn: () => api.getLarkSendObservation(wsId, id),
    retry: false,
    staleTime: 0,
  });

export const larkInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: larkKeys.installations(wsId),
    queryFn: () => api.listLarkInstallations(wsId),
    enabled: !!wsId,
    // Poll only while mounted and awaiting proof; silence never becomes failure.
    refetchInterval: (query) =>
      query.state.data?.installations.some(
        (inst) => inst.status === "active" && !inst.first_message_received_at,
      ) ? 5000 : false,
  });
