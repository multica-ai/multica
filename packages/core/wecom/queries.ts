import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const wecomKeys = {
  all: (wsId: string) => ["wecom", wsId] as const,
  installations: (wsId: string) => [...wecomKeys.all(wsId), "installations"] as const,
};

export const wecomInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: wecomKeys.installations(wsId),
    queryFn: () => api.listWecomInstallations(wsId),
    enabled: !!wsId,
  });
