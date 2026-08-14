import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const qianwenKeys = {
  all: (wsId: string) => ["qianwen", wsId] as const,
  installationsRoot: (wsId: string) => [
    ...qianwenKeys.all(wsId),
    "installations",
  ] as const,
  installations: (wsId: string, currentUserId: string) => [
    ...qianwenKeys.installationsRoot(wsId),
    "user",
    currentUserId,
  ] as const,
};

export const qianwenInstallationsOptions = (
  wsId: string,
  currentUserId: string,
) => queryOptions({
  queryKey: qianwenKeys.installations(wsId, currentUserId),
  queryFn: () => api.listQianwenInstallations(wsId),
  enabled: !!wsId && !!currentUserId,
});
