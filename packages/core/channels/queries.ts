import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const channelKeys = {
  all: (wsId: string) => ["channels", wsId] as const,
  list: (wsId: string) => [...channelKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...channelKeys.all(wsId), "detail", id] as const,
};

export function channelListOptions(wsId: string) {
  return queryOptions({
    queryKey: channelKeys.list(wsId),
    queryFn: () => api.listChannels(),
    staleTime: Infinity,
  });
}

export function channelDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: channelKeys.detail(wsId, id),
    queryFn: () => api.getChannel(id),
    enabled: !!id,
    staleTime: Infinity,
  });
}
