import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/** Query key namespace for everything Mattermost-installation-related.
 * Realtime sync invalidates `installations(wsId)` on `mattermost_installation:*`
 * events so the Settings panel updates without a manual refetch. */
export const mattermostKeys = {
  all: (wsId: string) => ["mattermost", wsId] as const,
  installations: (wsId: string) => [...mattermostKeys.all(wsId), "installations"] as const,
};

export const mattermostInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: mattermostKeys.installations(wsId),
    queryFn: () => api.listMattermostInstallations(wsId),
    enabled: !!wsId,
  });
