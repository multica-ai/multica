import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Workspace scoping note: `wsId` is part of the queryKey for cache isolation
// only. The actual workspace context is supplied by ApiClient's
// X-Workspace-Slug header set by the [workspaceSlug] layout.

export const channelKeys = {
  all: (wsId: string) => ["channels", wsId] as const,
  list: (wsId: string) => [...channelKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) => [...channelKeys.all(wsId), "detail", id] as const,
  members: (channelId: string) => ["channels", "members", channelId] as const,
  messages: (channelId: string) => ["channels", "messages", channelId] as const,
  // Phase 4 — per-parent thread payload (parent + replies, batch-hydrated
  // with reactions). Keyed by message id so the panel can be opened
  // independently of which channel the user is currently viewing.
  thread: (messageId: string) => ["channels", "thread", messageId] as const,
};

export function channelsListOptions(wsId: string, enabled: boolean) {
  return queryOptions({
    queryKey: channelKeys.list(wsId),
    queryFn: () => api.listChannels(),
    staleTime: Infinity,
    // Skip the request entirely when the workspace flag is off — the
    // backend would 404 anyway.
    enabled,
  });
}

export function channelDetailOptions(wsId: string, channelId: string, enabled: boolean) {
  return queryOptions({
    queryKey: channelKeys.detail(wsId, channelId),
    queryFn: () => api.getChannel(channelId),
    staleTime: Infinity,
    enabled: enabled && !!channelId,
  });
}

export function channelMembersOptions(channelId: string, enabled: boolean) {
  return queryOptions({
    queryKey: channelKeys.members(channelId),
    queryFn: () => api.listChannelMembers(channelId),
    staleTime: Infinity,
    enabled: enabled && !!channelId,
  });
}

export function channelMessageThreadOptions(channelId: string, messageId: string, enabled: boolean) {
  return queryOptions({
    queryKey: channelKeys.thread(messageId),
    queryFn: () => api.getChannelMessageThread(channelId, messageId),
    staleTime: Infinity,
    enabled: enabled && !!channelId && !!messageId,
  });
}

export function channelMessagesOptions(channelId: string, enabled: boolean) {
  return queryOptions({
    queryKey: channelKeys.messages(channelId),
    // Default page (newest 50). Older pages are an explicit follow-up using
    // useInfiniteQuery if/when the UI needs them.
    queryFn: () => api.listChannelMessages(channelId, { limit: 50 }),
    staleTime: Infinity,
    enabled: enabled && !!channelId,
  });
}
