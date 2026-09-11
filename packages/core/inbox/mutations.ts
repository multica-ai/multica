import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { inboxKeys } from "./queries";
import { onInboxInvalidate, onInboxSummaryInvalidate } from "./ws-updaters";
import { useWorkspaceId } from "../hooks";
import type { InboxItem } from "../types";

/**
 * Re-read every inbox cache a write can change: both workspace lists and the
 * cross-workspace unread summary.
 *
 * The unread badge reads that summary (`useInboxUnreadCount`), and it lives
 * under its own account-level key which `inboxKeys.all(wsId)` does not reach.
 * Every mutation here can change the number it holds, so each one refreshes it
 * once the server has confirmed, rather than waiting for the WebSocket echo of
 * its own action.
 *
 * Deliberately the same entry points realtime uses, not local copies: both
 * refreshes have to cancel any in-flight request first, and a mutation racing
 * a first load hits exactly the same hole a WS event does. A plain
 * `invalidateQueries` on the list here would let the list fall behind the
 * badge — the two are rendered side by side (see `refreshInboxQuery`).
 *
 * Not awaited by `onSettled`: the mutation is finished once the server has
 * answered, and a background refresh should not hold its lifecycle open.
 *
 * The rows are patched optimistically but the badge is NOT: it follows the
 * server's confirmation, which buys a single writer at the cost of the badge
 * trailing the row. Deriving it locally instead — recomputing the count from
 * the list cache and writing that back — reads as instant but is unsound: a
 * list cache proves only that the list was loaded ONCE, never that it is
 * complete or concurrent with the summary, and the account-level summary
 * request is not cancelled by the workspace-scoped `cancelQueries` below, so a
 * response already in flight lands on top of the local value anyway. Under
 * pagination it would be wrong by construction — one loaded page cannot
 * produce a global count. If instant feedback is wanted later, it has to be a
 * per-group delta that handles the race, not a recomputed total.
 */
function refreshInboxAfterWrite(qc: QueryClient, wsId: string) {
  void onInboxInvalidate(qc, wsId);
  void onInboxSummaryInvalidate(qc);
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
      return { prev, prevArchived };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
      if (ctx?.prevArchived) qc.setQueryData(inboxKeys.archived(wsId), ctx.prevArchived);
    },
    onSettled: () => {
      refreshInboxAfterWrite(qc, wsId);
    },
  });
}

export function useRetrySourceContextQuickCreate() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (taskId: string) => api.retrySourceContextQuickCreate(taskId),
    onSettled: () => {
      refreshInboxAfterWrite(qc, wsId);
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
 * The rows flip at once; the badge follows on settle — see
 * {@link refreshInboxAfterWrite} for why it is not patched locally.
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
      return { prev, prevArchived };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
      if (ctx?.prevArchived) qc.setQueryData(inboxKeys.archived(wsId), ctx.prevArchived);
    },
    onSettled: () => {
      // The switcher dot must light again when the workspace goes back to
      // having unread items — that count lives on the server.
      refreshInboxAfterWrite(qc, wsId);
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
      // Both lists: the item just moved from the main inbox into the archive.
      refreshInboxAfterWrite(qc, wsId);
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
      refreshInboxAfterWrite(qc, wsId);
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
      refreshInboxAfterWrite(qc, wsId);
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
      refreshInboxAfterWrite(qc, wsId);
    },
  });
}

export function useArchiveAllReadInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: () => api.archiveAllReadInbox(),
    onSettled: () => {
      refreshInboxAfterWrite(qc, wsId);
    },
  });
}

export function useArchiveCompletedInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: () => api.archiveCompletedInbox(),
    onSettled: () => {
      refreshInboxAfterWrite(qc, wsId);
    },
  });
}
