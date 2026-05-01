import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// TanStack keys + options for the notifications page (route='notifications').
// Single-item operations (read, archive) reuse the inbox mutation paths via
// useMarkInboxRead / useArchiveInbox — the route flag is server-side only.
export const notificationsKeys = {
  all: (wsId: string) => ["notifications", wsId] as const,
  list: (wsId: string) => [...notificationsKeys.all(wsId), "list"] as const,
  unreadCount: (wsId: string) =>
    [...notificationsKeys.all(wsId), "unread-count"] as const,
};

export function notificationsListOptions(wsId: string) {
  return queryOptions({
    queryKey: notificationsKeys.list(wsId),
    queryFn: () => api.listNotifications(),
    enabled: !!wsId,
  });
}

export function notificationsUnreadCountOptions(wsId: string) {
  return queryOptions({
    queryKey: notificationsKeys.unreadCount(wsId),
    queryFn: () => api.getUnreadNotificationsCount(),
    enabled: !!wsId,
  });
}
