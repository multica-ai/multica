import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const channelKeys = {
  all: (wsId: string) => ["channels", wsId] as const,
  list: (wsId: string) => [...channelKeys.all(wsId), "list"] as const,
  messages: (channelId: string) => ["channel-messages", channelId] as const,
  members: (channelId: string) => ["channel-members", channelId] as const,
};

export function channelsOptions(wsId: string) {
  return queryOptions({
    queryKey: channelKeys.list(wsId),
    queryFn: () => api.listChannels(),
    enabled: !!wsId,
  });
}

export function channelMessagesOptions(channelId: string) {
  return queryOptions({
    queryKey: channelKeys.messages(channelId),
    queryFn: () => api.listChannelMessages(channelId),
    enabled: !!channelId,
  });
}

export function channelMembersOptions(channelId: string) {
  return queryOptions({
    queryKey: channelKeys.members(channelId),
    queryFn: () => api.listChannelMembers(channelId),
    enabled: !!channelId,
  });
}
