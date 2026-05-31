import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const channelKeys = {
  all: (wsId: string) => ["channels", wsId] as const,
  list: (wsId: string) => [...channelKeys.all(wsId), "list"] as const,
  detail: (wsId: string, channelId: string) =>
    [...channelKeys.all(wsId), "detail", channelId] as const,
  members: (wsId: string, channelId: string) =>
    [...channelKeys.all(wsId), "members", channelId] as const,
  threads: (wsId: string, channelId: string) =>
    [...channelKeys.all(wsId), "threads", channelId] as const,
  messages: (wsId: string, channelId: string, threadId: string) =>
    [...channelKeys.all(wsId), "messages", channelId, threadId] as const,
};

export function channelListOptions(wsId: string) {
  return queryOptions({
    queryKey: channelKeys.list(wsId),
    queryFn: () => api.listChannels(),
    select: (data) => data.channels,
  });
}

export function channelThreadsOptions(wsId: string, channelId: string | null) {
  return queryOptions({
    queryKey: channelKeys.threads(wsId, channelId ?? ""),
    queryFn: () => api.listChannelThreads(channelId ?? ""),
    enabled: !!channelId,
    select: (data) => data.threads,
  });
}

export function threadMessagesOptions(
  wsId: string,
  channelId: string | null,
  threadId: string | null,
) {
  return queryOptions({
    queryKey: channelKeys.messages(wsId, channelId ?? "", threadId ?? ""),
    queryFn: () => api.listThreadMessages(channelId ?? "", threadId ?? ""),
    enabled: !!channelId && !!threadId,
  });
}

export function channelMembersOptions(wsId: string, channelId: string | null) {
  return queryOptions({
    queryKey: channelKeys.members(wsId, channelId ?? ""),
    queryFn: () => api.listChannelMembers(channelId ?? ""),
    enabled: !!channelId,
    select: (data) => data.members,
  });
}
