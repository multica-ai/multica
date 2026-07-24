import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/** Query key namespace for everything WeChat-installation-related. Realtime
 * sync invalidates `installations(wsId)` on `wechat_installation:*` events so
 * the Settings panel updates without a manual refetch (e.g. after the QR-scan
 * install lands the install in another tab). */
export const wechatKeys = {
  all: (wsId: string) => ["wechat", wsId] as const,
  installations: (wsId: string) => [...wechatKeys.all(wsId), "installations"] as const,
};

export const wechatInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: wechatKeys.installations(wsId),
    queryFn: () => api.listWechatInstallations(wsId),
    enabled: !!wsId,
  });
