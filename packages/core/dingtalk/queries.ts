import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/** Query key namespace for everything DingTalk-installation-related. Realtime
 * sync invalidates `installations(wsId)` on `dingtalk_installation:*` events so
 * the Settings panel updates without a manual refetch (e.g. after a binding
 * lands the install in another tab). */
export const dingtalkKeys = {
  all: (wsId: string) => ["dingtalk", wsId] as const,
  installations: (wsId: string) => [...dingtalkKeys.all(wsId), "installations"] as const,
};

export const dingtalkInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: dingtalkKeys.installations(wsId),
    queryFn: () => api.listDingTalkInstallations(wsId),
    enabled: !!wsId,
  });
