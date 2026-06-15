import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const channelKeys = {
  all: (wsId: string) => ["channels", wsId] as const,
  list: (wsId: string) => [...channelKeys.all(wsId), "list"] as const,
  groups: (wsId: string) => [...channelKeys.all(wsId), "groups"] as const,
  detail: (wsId: string, channelId: string) =>
    [...channelKeys.all(wsId), "detail", channelId] as const,
  members: (wsId: string, channelId: string) =>
    [...channelKeys.all(wsId), "members", channelId] as const,
  threads: (wsId: string, channelId: string) =>
    [...channelKeys.all(wsId), "threads", channelId] as const,
  messages: (wsId: string, channelId: string, threadId: string) =>
    [...channelKeys.all(wsId), "messages", channelId, threadId] as const,
  channelMessages: (wsId: string, channelId: string) =>
    [...channelKeys.all(wsId), "channelMessages", channelId] as const,
  messageThread: (wsId: string, channelId: string, messageId: string) =>
    [...channelKeys.all(wsId), "messageThread", channelId, messageId] as const,
  context: (wsId: string, channelId: string) =>
    [...channelKeys.all(wsId), "context", channelId] as const,
};

export function channelListOptions(wsId: string) {
  return queryOptions({
    queryKey: channelKeys.list(wsId),
    queryFn: () => api.listChannels(),
    select: (data) => data.channels,
  });
}

export function channelGroupsOptions(wsId: string) {
  return queryOptions({
    queryKey: channelKeys.groups(wsId),
    queryFn: () => api.listChannelGroups(),
  });
}

const CHANNEL_PAGE_SIZE = 20;

export function channelMessagesOptions(wsId: string, channelId: string | null) {
  return infiniteQueryOptions({
    queryKey: channelKeys.channelMessages(wsId, channelId ?? ""),
    queryFn: ({ pageParam }) =>
      api.listChannelMessages(channelId ?? "", {
        limit: CHANNEL_PAGE_SIZE,
        before: pageParam ?? undefined,
      }),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) =>
      lastPage.has_more && lastPage.messages.length > 0
        ? lastPage.messages[0]!.created_at
        : undefined,
    enabled: !!channelId,
    select: (data) => data.pages.flatMap((p) => p.messages),
  });
}

export function messageThreadOptions(wsId: string, channelId: string | null, messageId: string | null) {
  return queryOptions({
    queryKey: channelKeys.messageThread(wsId, channelId ?? "", messageId ?? ""),
    queryFn: () => api.getMessageThread(channelId ?? "", messageId ?? ""),
    enabled: !!channelId && !!messageId,
  });
}

export function channelContextOptions(wsId: string, channelId: string | null) {
  return queryOptions({
    queryKey: channelKeys.context(wsId, channelId ?? ""),
    queryFn: () => api.getChannelContext(channelId ?? ""),
    enabled: !!channelId,
  });
}

// Legacy V1 queries (backward compat)
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
