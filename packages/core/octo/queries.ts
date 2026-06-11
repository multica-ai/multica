import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/** Query key namespace for Octo-installation state. Realtime sync invalidates
 * `installations(wsId)` on `octo_installation:*` events so the Settings panel
 * updates without a refetch. */
export const octoKeys = {
  all: (wsId: string) => ["octo", wsId] as const,
  installations: (wsId: string) => [...octoKeys.all(wsId), "installations"] as const,
};

export const octoInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: octoKeys.installations(wsId),
    queryFn: () => api.listOctoInstallations(wsId),
    enabled: !!wsId,
  });
