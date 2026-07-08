import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { inboxKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import type { InboxItem } from "../types";

function invalidateInboxCaches(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
) {
  qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
  qc.invalidateQueries({ queryKey: inboxKeys.unreadSummary() });
}

export function useMarkInboxRead() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.markInboxRead(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
        old?.map((item) => (item.id === id ? { ...item, read: true } : item)),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      invalidateInboxCaches(qc, wsId);
    },
  });
}

export function useArchiveInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.archiveInbox(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      // Archive all items for the same issue (same behavior as store)
      const target = prev?.find((i) => i.id === id);
      const issueId = target?.issue_id;
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
        old?.map((item) =>
          item.id === id || (issueId && item.issue_id === issueId)
            ? { ...item, archived: true }
            : item,
        ),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      invalidateInboxCaches(qc, wsId);
    },
  });
}

export function useMarkAllInboxRead() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: () => api.markAllInboxRead(),
    onMutate: async () => {
      await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
        old?.map((item) =>
          !item.archived ? { ...item, read: true } : item,
        ),
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      invalidateInboxCaches(qc, wsId);
    },
  });
}

export function useArchiveAllInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: () => api.archiveAllInbox(),
    onSettled: () => {
      invalidateInboxCaches(qc, wsId);
    },
  });
}

export function useArchiveAllReadInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: () => api.archiveAllReadInbox(),
    onSettled: () => {
      invalidateInboxCaches(qc, wsId);
    },
  });
}

export function useArchiveCompletedInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: () => api.archiveCompletedInbox(),
    onSettled: () => {
      invalidateInboxCaches(qc, wsId);
    },
  });
}
