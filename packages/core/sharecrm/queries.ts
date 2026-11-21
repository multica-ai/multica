import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const sharecrmKeys = {
  all: (wsId: string) => ["sharecrm", wsId] as const,
  installations: (wsId: string) => [...sharecrmKeys.all(wsId), "installations"] as const,
};

export const sharecrmInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: sharecrmKeys.installations(wsId),
    queryFn: () => api.listShareCRMInstallations(wsId),
    enabled: !!wsId,
  });
