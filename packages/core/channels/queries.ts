import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { Channel } from "../types/channel";

// ── Query Keys ───────────────────────────────────────────

export const channelKeys = {
  all: (wsId: string) => ["channels", wsId] as const,
  list: (wsId: string) => [...channelKeys.all(wsId), "list"] as const,
  detail: (wsId: string, channelId: string) =>
    [...channelKeys.all(wsId), "detail", channelId] as const,
  members: (wsId: string, channelId: string) =>
    [...channelKeys.all(wsId), "members", channelId] as const,
  messages: (wsId: string, channelId: string) =>
    [...channelKeys.all(wsId), "messages", channelId] as const,
  thread: (wsId: string, channelId: string, threadId: string) =>
    [...channelKeys.all(wsId), "thread", channelId, threadId] as const,
};

// ── Query Options ────────────────────────────────────────

export function channelListOptions(wsId: string) {
  return queryOptions({
    queryKey: channelKeys.list(wsId),
    queryFn: async () => {
      const res = await api.listChannels();
      return res as Channel[];
    },
    enabled: !!wsId,
  });
}

export function channelDetailOptions(wsId: string, channelId: string) {
  return queryOptions({
    queryKey: channelKeys.detail(wsId, channelId),
    queryFn: async () => {
      const res = await api.getChannel(channelId); return res;
    },
    enabled: !!wsId && !!channelId,
  });
}

export function channelMembersOptions(wsId: string, channelId: string) {
  return queryOptions({
    queryKey: channelKeys.members(wsId, channelId),
    queryFn: async () => {
      const res = await api.listChannelMembers(channelId); return res;
    },
    enabled: !!wsId && !!channelId,
  });
}

export function channelMessagesOptions(
  wsId: string,
  channelId: string,
) {
  return queryOptions({
    queryKey: channelKeys.messages(wsId, channelId),
    queryFn: async () => {
      const res = await api.listChannelMessages(channelId); return res;
    },
    enabled: !!wsId && !!channelId,
  });
}
