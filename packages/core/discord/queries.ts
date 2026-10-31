import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/** Query key namespace for everything Discord-installation-related. Realtime
 * sync invalidates `installations(wsId)` on `discord_installation:*` events
 * so the Settings panel updates without a manual refetch. */
export const discordKeys = {
  all: (wsId: string) => ["discord", wsId] as const,
  installations: (wsId: string) => [...discordKeys.all(wsId), "installations"] as const,
};

export const discordInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: discordKeys.installations(wsId),
    queryFn: () => api.listDiscordInstallations(wsId),
    enabled: !!wsId,
  });
