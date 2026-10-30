import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/** Query key namespace for everything Telegram-installation-related. Realtime
 * sync invalidates `installations(wsId)` on `telegram_installation:*` events
 * so the Settings panel updates without a manual refetch. */
export const telegramKeys = {
  all: (wsId: string) => ["telegram", wsId] as const,
  installations: (wsId: string) => [...telegramKeys.all(wsId), "installations"] as const,
};

export const telegramInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: telegramKeys.installations(wsId),
    queryFn: () => api.listTelegramInstallations(wsId),
    enabled: !!wsId,
  });
