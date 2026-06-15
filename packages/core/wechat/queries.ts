import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

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
