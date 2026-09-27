import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const weixinKeys = {
  all: (wsId: string) => ["weixin", wsId] as const,
  installations: (wsId: string) => [...weixinKeys.all(wsId), "installations"] as const,
  login: (wsId: string, loginId: string) => [...weixinKeys.all(wsId), "login", loginId] as const,
};

export const weixinInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: weixinKeys.installations(wsId),
    queryFn: () => api.listWeixinInstallations(wsId),
    enabled: !!wsId,
  });

/** Polls a QR login until it connects or fails. */
export const weixinLoginOptions = (wsId: string, loginId: string) =>
  queryOptions({
    queryKey: weixinKeys.login(wsId, loginId),
    queryFn: () => api.getWeixinLogin(wsId, loginId),
    enabled: !!wsId && !!loginId,
    refetchInterval: (query) => {
      const state = query.state.data?.state;
      return state === "connected" || state === "failed" ? false : 2000;
    },
  });
