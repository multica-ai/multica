import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { deduplicateInboxItems, inboxKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import type { InboxItem, InboxWorkspaceUnread } from "../types";

/**
 * Refresh the cross-workspace unread summary.
 *
 * The unread badge reads that summary (`useInboxUnreadCount`), and it lives
 * under its own account-level key which `inboxKeys.all(wsId)` does not reach.
 * Every mutation here can change the number it holds, so each one refreshes it
 * on settle rather than waiting for the WebSocket echo of its own action.
 */
function invalidateUnreadSummary(qc: QueryClient) {
  qc.invalidateQueries({ queryKey: inboxKeys.unreadSummary() });
}

/**
 * Re-derive this workspace's unread count from the inbox list cache and write
 * it into the summary cache, so the badge tracks an optimistic patch instead
 * of waiting for a round-trip.
 *
 * The summary is server-computed and cannot be recalculated from nothing — but
 * every caller below has just patched the list cache, and the list holds
 * exactly the rows the count is defined over. Re-running the same
 * `deduplicateInboxItems` the inbox view uses keeps the badge in lockstep with
 * the rows on screen; `onSettled` still re-pulls the authoritative value.
 *
 * Both caches are read defensively. No list cache means no inbox surface is
 * mounted, and no summary cache means the badge has not loaded — in either
 * case there is nothing to be optimistic about, and the server value stands.
 */
function syncUnreadSummaryFromList(qc: QueryClient, wsId: string) {
  const items = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
  if (!items) return;
  const count = deduplicateInboxItems(items).filter((i) => !i.read).length;
  qc.setQueryData<InboxWorkspaceUnread[]>(inboxKeys.unreadSummary(), (old) => {
    if (!old) return old;
    // Order carries no meaning here — every consumer scans by workspace id.
    // A zero-count workspace is dropped, mirroring the server response, which
    // omits workspaces with nothing unread.
    const others = old.filter((entry) => entry.workspace_id !== wsId);
    return count > 0 ? [...others, { workspace_id: wsId, count }] : others;
  });
}

export function useMarkInboxRead() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.markInboxRead(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: inboxKeys.all(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      const prevArchived = qc.getQueryData<InboxItem[]>(inboxKeys.archived(wsId));
      const markRead = (old: InboxItem[] | undefined) =>
        old?.map((item) => (item.id === id ? { ...item, read: true } : item));
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), markRead);
      // Opening a notification from the archived sub-view marks it read too —
      // patch that cache as well, or its unread dot would sit there until the
      // next refetch.
      qc.setQueryData<InboxItem[]>(inboxKeys.archived(wsId), markRead);
      syncUnreadSummaryFromList(qc, wsId);
      return { prev, prevArchived };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
      if (ctx?.prevArchived) qc.setQueryData(inboxKeys.archived(wsId), ctx.prevArchived);
      syncUnreadSummaryFromList(qc, wsId);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
      invalidateUnreadSummary(qc);
    },
  });
}

export function useRetrySourceContextQuickCreate() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (taskId: string) => api.retrySourceContextQuickCreate(taskId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
      invalidateUnreadSummary(qc);
    },
  });
}

/**
 * Flip a notification back to unread — the inverse of {@link useMarkInboxRead}.
 *
 * Same optimistic shape as marking read (predictable outcome, no navigation,
 * trivial rollback), and it patches BOTH caches for the same reason: an item
 * can be actioned from either list, and leaving the other one stale would show
 * two different read states for one notification after a view switch.
 *
 * The unread badge reads the server-computed cross-workspace summary, so the
 * patch alone would not raise it — `syncUnreadSummaryFromList` re-derives that
 * workspace's entry from the freshly patched list so the badge moves without
 * waiting for the round-trip. `onSettled` still re-pulls the real value.
 */
export function useMarkInboxUnread() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.markInboxUnread(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: inboxKeys.all(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      const prevArchived = qc.getQueryData<InboxItem[]>(inboxKeys.archived(wsId));
      const markUnread = (old: InboxItem[] | undefined) =>
        old?.map((item) => (item.id === id ? { ...item, read: false } : item));
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), markUnread);
      qc.setQueryData<InboxItem[]>(inboxKeys.archived(wsId), markUnread);
      syncUnreadSummaryFromList(qc, wsId);
      return { prev, prevArchived };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
      if (ctx?.prevArchived) qc.setQueryData(inboxKeys.archived(wsId), ctx.prevArchived);
      syncUnreadSummaryFromList(qc, wsId);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
      // The switcher dot must light again when the workspace goes back to
      // having unread items — that count lives on the server.
      invalidateUnreadSummary(qc);
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
      // Archiving an unread issue group drops it out of the badge immediately.
      syncUnreadSummaryFromList(qc, wsId);
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
      syncUnreadSummaryFromList(qc, wsId);
    },
    onSettled: () => {
      // Both lists: the item just moved from the main inbox into the archive.
      qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
      invalidateUnreadSummary(qc);
    },
  });
}

/**
 * Restore an archived notification to the main inbox.
 *
 * Optimistic on the ARCHIVED cache only: flipping `archived` there makes the
 * row leave the archived list at once (the dedup helper filters on it), the
 * user stays put, and rollback is a single snapshot restore. The main list is
 * left to `onSettled` — its contents after a restore are the server's call
 * (which sibling rows come back, their read state, their order), so it is
 * invalidated rather than reconstructed client-side.
 */
export function useUnarchiveInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.unarchiveInbox(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: inboxKeys.archived(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.archived(wsId));
      // Restore every sibling for the same issue — the server unarchives the
      // whole issue group, so the optimistic patch must too or the rest of the
      // group would linger in the archived list until the refetch lands.
      const target = prev?.find((i) => i.id === id);
      const issueId = target?.issue_id;
      qc.setQueryData<InboxItem[]>(inboxKeys.archived(wsId), (old) =>
        old?.map((item) =>
          item.id === id || (issueId && item.issue_id === issueId)
            ? { ...item, archived: false }
            : item,
        ),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.archived(wsId), ctx.prev);
    },
    onSettled: () => {
      // Both lists: the item moves from one to the other, and the unread badge
      // rises again when it was archived unread.
      qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
      invalidateUnreadSummary(qc);
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
      syncUnreadSummaryFromList(qc, wsId);
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
      syncUnreadSummaryFromList(qc, wsId);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
      invalidateUnreadSummary(qc);
    },
  });
}

// The three batch-archive mutations below all move items into the archive, so
// each invalidates BOTH lists on settle — plus the unread summary, since an
// archived unread group leaves the badge.
export function useArchiveAllInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: () => api.archiveAllInbox(),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
      invalidateUnreadSummary(qc);
    },
  });
}

export function useArchiveAllReadInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: () => api.archiveAllReadInbox(),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
      invalidateUnreadSummary(qc);
    },
  });
}

export function useArchiveCompletedInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: () => api.archiveCompletedInbox(),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
      invalidateUnreadSummary(qc);
    },
  });
}
