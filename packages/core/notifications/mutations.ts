import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { notificationsKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import type { InboxItem } from "../types";

// Single-item read/archive on a notifications-routed item. Backend reuses the
// shared inbox endpoints (`/api/inbox/{id}/...`) — these hooks only differ
// from useMarkInboxRead/useArchiveInbox in which TanStack cache they update.
export function useMarkNotificationRead() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.markInboxRead(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: notificationsKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(notificationsKeys.list(wsId));
      qc.setQueryData<InboxItem[]>(notificationsKeys.list(wsId), (old) =>
        old?.map((item) => (item.id === id ? { ...item, read: true } : item)),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(notificationsKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: notificationsKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: notificationsKeys.unreadCount(wsId) });
    },
  });
}

export function useArchiveNotification() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.archiveInbox(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: notificationsKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(notificationsKeys.list(wsId));
      qc.setQueryData<InboxItem[]>(notificationsKeys.list(wsId), (old) =>
        old?.map((item) =>
          item.id === id ? { ...item, archived: true } : item,
        ),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(notificationsKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: notificationsKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: notificationsKeys.unreadCount(wsId) });
    },
  });
}

export function useMarkAllNotificationsRead() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: () => api.markAllNotificationsRead(),
    onMutate: async () => {
      await qc.cancelQueries({ queryKey: notificationsKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(notificationsKeys.list(wsId));
      qc.setQueryData<InboxItem[]>(notificationsKeys.list(wsId), (old) =>
        old?.map((item) => (!item.archived ? { ...item, read: true } : item)),
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(notificationsKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: notificationsKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: notificationsKeys.unreadCount(wsId) });
    },
  });
}

export function useArchiveAllNotifications() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: () => api.archiveAllNotifications(),
    onMutate: async () => {
      await qc.cancelQueries({ queryKey: notificationsKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(notificationsKeys.list(wsId));
      qc.setQueryData<InboxItem[]>(notificationsKeys.list(wsId), (old) =>
        old?.map((item) => ({ ...item, archived: true })),
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(notificationsKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: notificationsKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: notificationsKeys.unreadCount(wsId) });
    },
  });
}
