import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/** Query key namespace for everything DingTalk-installation-related. Realtime
 * sync invalidates `installations(wsId)` on `dingtalk_installation:*` events so
 * the Settings panel updates without a manual refetch (e.g. after a binding
 * lands the install in another tab). */
export const dingtalkKeys = {
  all: (wsId: string) => ["dingtalk", wsId] as const,
  installations: (wsId: string) => [...dingtalkKeys.all(wsId), "installations"] as const,
  groupRoutes: (wsId: string) => [...dingtalkKeys.all(wsId), "group-routes"] as const,
};

export const dingtalkInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: dingtalkKeys.installations(wsId),
    queryFn: () => api.listDingTalkInstallations(wsId),
    enabled: !!wsId,
  });

export const dingtalkGroupRoutesOptions = (wsId: string) =>
  queryOptions({
    queryKey: dingtalkKeys.groupRoutes(wsId),
    queryFn: () => api.listDingTalkGroupRoutes(wsId),
    enabled: !!wsId,
    // Group discovery is driven by DingTalk Stream callbacks rather than an
    // HTTP mutation, so poll lightly while the settings panel is open. Stop
    // after a failed retry cycle: a persistent server error must require an
    // explicit user retry instead of producing another request every 5s.
    refetchInterval: (query) => (query.state.status === "error" ? false : 5_000),
  });
