import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const weixinKeys = {
  all: (workspaceId: string) => ["weixin", workspaceId] as const,
  installations: (workspaceId: string) =>
    [...weixinKeys.all(workspaceId), "installations"] as const,
  installStatus: (workspaceId: string, sessionId: string) =>
    [...weixinKeys.all(workspaceId), "install", sessionId] as const,
};

export const weixinInstallationsOptions = (workspaceId: string) =>
  queryOptions({
    queryKey: weixinKeys.installations(workspaceId),
    queryFn: () => api.listWeixinInstallations(workspaceId),
  });
