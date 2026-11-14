import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { inboxKeys, mapArchivedInboxCache, patchArchivedInboxCaches, type ArchivedInboxCache } from "./queries";
import { onInboxInvalidate, onInboxSummaryInvalidate } from "./ws-updaters";
import { useWorkspaceId } from "../hooks";
import type { InboxItem } from "../types";

/**
 * Refresh workspace inbox lists and the account-level unread summary after a
 * confirmed write. Use realtime's refresh helpers so in-flight reads are cancelled
 * before refetching; a workspace query prefix does not reach the summary.
 *
 * Rows may patch optimistically, but the unread badge follows server confirmation.
 * A partial or independently fetched list cannot produce a reliable global total.
 * Do not hold the completed mutation open while the refresh runs.
 */
function refreshInboxAfterWrite(qc: QueryClient, wsId: string) {
  void onInboxInvalidate(qc, wsId);
  void onInboxSummaryInvalidate(qc);
}

/** The issue an inbox item belongs to, looked up in the active and archived caches. */
function cachedInboxIssueId(qc: QueryClient, wsId: string, id: string): string | null {
  const active = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))?.find((item) => item.id === id);
  if (active) return active.issue_id;
  for (const [, data] of qc.getQueriesData<ArchivedInboxCache>({ queryKey: inboxKeys.archived(wsId) })) {
    if (!data) continue;
    const items = Array.isArray(data) ? data : "pages" in data ? data.pages.flatMap((page) => page.items) : data.items;
    const archived = items.find((item) => item.id === id);
    if (archived) return archived.issue_id;
  }
  return null;
}

/**
 * Read an inbox item. The server also reads every other unread active item on
 * its issue (MUL-8618): selecting a row opens the whole issue, so an
 * assignment under a later comment is read with it instead of staying unread
 * out of sight. The optimistic patch mirrors that set.
 */
export function useMarkInboxRead() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.markInboxRead(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: inboxKeys.all(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      const issueId = cachedInboxIssueId(qc, wsId, id);
      const markRead = (old: InboxItem[] | undefined) =>
        old?.map((item) =>
          item.id === id || (issueId != null && item.issue_id === issueId && !item.archived)
            ? { ...item, read: true }
            : item,
        );
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), markRead);
      // Opening a notification from the archived sub-view marks it read too —
      // patch that cache as well, or its unread dot would sit there until the
      // next refetch.
      const prevArchived = patchArchivedInboxCaches(qc, wsId, (items) => markRead(items) ?? items);
      return { prev, prevArchived };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
      for (const [key, data] of ctx?.prevArchived ?? []) qc.setQueryData(key, data);
    },
    onSettled: () => {
      refreshInboxAfterWrite(qc, wsId);
    },
  });
}

/**
 * Read every unread notification on one issue: what viewing the issue outside
 * the inbox does (MUL-8588). The caller passes the workspace id it already
 * holds.
 *
 * Not optimistic. Outside the inbox the list is usually not loaded, and only
 * the server knows which of the issue's rows are unread. The caches refresh
 * unless the server reports that no row changed, so opening an issue with
 * nothing unread costs no inbox refetch. A response without a count is treated
 * as a change: skipping the refresh then could leave the badge stale.
 */
export function useMarkIssueInboxRead(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (issueId: string) => api.markIssueInboxRead(issueId),
    onSuccess: (result) => {
      if (result.count !== 0) refreshInboxAfterWrite(qc, wsId);
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
      const markUnread = (old: InboxItem[] | undefined) =>
        old?.map((item) => (item.id === id ? { ...item, read: false } : item));
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), markUnread);
      const prevArchived = patchArchivedInboxCaches(qc, wsId, (items) => markUnread(items) ?? items);
      return { prev, prevArchived };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
      for (const [key, data] of ctx?.prevArchived ?? []) qc.setQueryData(key, data);
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
 * Snooze an issue until a time. Issue-level, like archive: every active
 * notification on the issue is put away with it, and the one snoozed from is
 * unread again so it reads as new when it comes back.
 */
export function useSnoozeInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, until }: { id: string; until: string }) => api.snoozeInbox(id, until),
    onMutate: async ({ id, until }) => {
      await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      const issueId = prev?.find((item) => item.id === id)?.issue_id;
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
        old?.map((item) => {
          if (item.archived) return item;
          if (item.id === id) return { ...item, snoozed_until: until, read: false };
          return issueId && item.issue_id === issueId ? { ...item, snoozed_until: until } : item;
        }),
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      // A snoozed issue leaves the badge until it comes back.
      refreshInboxAfterWrite(qc, wsId);
    },
  });
}

/** Bring a snoozed issue back now. */
export function useUnsnoozeInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.unsnoozeInbox(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      const issueId = prev?.find((item) => item.id === id)?.issue_id;
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
        old?.map((item) =>
          !item.archived && (item.id === id || (issueId && item.issue_id === issueId))
            ? { ...item, snoozed_until: null }
            : item,
        ),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      refreshInboxAfterWrite(qc, wsId);
    },
  });
}

export function useDeleteInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteInbox(id),
    // Deletion is not optimistic: keep the row and selection on failure.
    onSuccess: () => refreshInboxAfterWrite(qc, wsId),
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
      // Resolve the group once across every page and deep-link cache.
      let issueId: string | null | undefined;
      for (const [, data] of qc.getQueriesData<ArchivedInboxCache>({ queryKey: inboxKeys.archived(wsId) })) {
        if (data) mapArchivedInboxCache(data, (items) => {
          issueId ??= items.find((item) => item.id === id)?.issue_id;
          return items;
        });
      }
      const prev = patchArchivedInboxCaches(qc, wsId, (items) => items.map((item) =>
        item.id === id || (issueId && item.issue_id === issueId) ? { ...item, archived: false } : item));
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      for (const [key, data] of ctx?.prev ?? []) qc.setQueryData(key, data);
    },
    onSettled: () => {
      // Both lists: the item moves from one to the other, and the unread badge
      // rises again when it was archived unread.
      refreshInboxAfterWrite(qc, wsId);
    },
  });
}

/**
 * Move issues that only carry updates to the archive: what reading an update
 * comes to (`readOnly`), and what clearing the section does to the rest.
 * `groups` are list keys (`issue_id ?? id`).
 *
 * The rows leave at once. Which of them the server really archived is its
 * call — an issue that gained something addressed to the user in the meantime
 * stays — so the list is refreshed on settle rather than trusted.
 */
export function useArchiveInboxUpdates() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ groups, readOnly }: { groups: readonly string[]; readOnly: boolean }) =>
      api.archiveInboxUpdates(groups, readOnly),
    onMutate: async ({ groups }) => {
      await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      const keys = new Set(groups);
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
        old?.map((item) =>
          !item.archived && keys.has(item.issue_id ?? item.id) ? { ...item, archived: true } : item,
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
