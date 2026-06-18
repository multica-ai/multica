import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const channelKeys = {
  all: (wsId: string) => ["channels", wsId] as const,
  list: (wsId: string) => [...channelKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...channelKeys.all(wsId), "detail", id] as const,
  agentSettings: (wsId: string, id: string) =>
    [...channelKeys.all(wsId), "agent-settings", id] as const,
  // CEREBRO-PATCH(core-channels-perms-key): TECH-3698 — per-channel permission settings.
  permissions: (wsId: string, id: string) =>
    [...channelKeys.all(wsId), "permissions", id] as const,
};

export function channelListOptions(wsId: string) {
  return queryOptions({
    queryKey: channelKeys.list(wsId),
    queryFn: () => api.listChannels(),
    staleTime: Infinity,
  });
}

// CEREBRO-PATCH(channel-roster-list-q): TECH-3758 — the Chat block roster
// includes inbox-archived channels (archive only hides them from the inbox
// feed, not from the persistent chat interface). Separate cache key from the
// inbox list above so archiving still hides the row from the inbox boxes.
export function channelRosterListOptions(wsId: string) {
  return queryOptions({
    queryKey: [...channelKeys.list(wsId), "roster"] as const,
    queryFn: () => api.listChannels({ include_archived: true }),
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

// CEREBRO-PATCH(core-channels-listen-q): JEH-699 — listen-mode overrides
// for agents in a channel. The server only stores rows when the user has
// flipped an agent away from the default ('always'), so callers should
// default missing rows to 'always' on the client.
export function channelAgentSettingsOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: channelKeys.agentSettings(wsId, id),
    queryFn: () => api.listChannelAgentSettings(id),
    enabled: !!id,
    staleTime: Infinity,
  });
}

// CEREBRO-PATCH(core-channels-perms-q): TECH-3698 — per-channel permission
// settings (rename / add-remove members / leave). Server returns effective
// settings (defaults when no row exists), so the client never needs to fill.
export function channelPermissionsOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: channelKeys.permissions(wsId, id),
    queryFn: () => api.getChannelPermissions(id),
    enabled: !!id,
    staleTime: Infinity,
  });
}
