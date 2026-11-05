import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/** Query key namespace for everything Lark-installation-related. Realtime
 * sync invalidates `installations(wsId)` on `lark_installation:*` events
 * so the Settings panel updates without a refetch. */
export const larkKeys = {
  all: (wsId: string) => ["lark", wsId] as const,
  installations: (wsId: string) => [...larkKeys.all(wsId), "installations"] as const,
};

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
