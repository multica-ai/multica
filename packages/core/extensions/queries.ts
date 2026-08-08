import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const extensionKeys = {
  all: (wsId: string) => ["workspaces", wsId, "extensions"] as const,
  list: (wsId: string) => [...extensionKeys.all(wsId), "list"] as const,
  detail: (wsId: string, releaseId: string) =>
    [...extensionKeys.all(wsId), "detail", releaseId] as const,
};

export function extensionListOptions(wsId: string) {
  return queryOptions({
    queryKey: extensionKeys.list(wsId),
    queryFn: () => api.listPlatformExtensions(),
    enabled: !!wsId,
  });
}

export function extensionDetailOptions(wsId: string, releaseId: string) {
  return queryOptions({
    queryKey: extensionKeys.detail(wsId, releaseId),
    queryFn: () => api.getPlatformExtension(releaseId),
    enabled: !!wsId && !!releaseId,
  });
}
