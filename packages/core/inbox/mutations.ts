import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { inboxKeys, mapArchivedInboxCache, patchArchivedInboxCaches, type ArchivedInboxCache } from "./queries";
import { onInboxInvalidate, onInboxSummaryInvalidate } from "./ws-updaters";
import { useWorkspaceId } from "../hooks";
import { issueKeys } from "../issues/queries";
import { refreshSubscribedIssues } from "../issues/subscribed";
import type { InboxItem, SnoozedInboxIssue } from "../types";

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

export type InboxUnsubscribeScope = "issue" | "subtree";

/** Subscription state changed for the viewer: re-read everything derived from it. */
function refreshAfterSubscriptionWrite(qc: QueryClient, wsId: string) {
  refreshInboxAfterWrite(qc, wsId);
  qc.invalidateQueries({ queryKey: issueKeys.subscribersAll() });
  void refreshSubscribedIssues(qc, wsId);
}

/**
 * Leave the issue an inbox item is about, then archive the item: the one-step
 * "stop telling me about this" for issues the viewer is only watching.
 *
 * Unsubscribes first so a failed unsubscribe never archives — an archived row
 * whose issue keeps notifying would read as the action having worked. The row
 * leaves the list optimistically, like a plain archive.
 */
export interface UnsubscribeArchiveVariables {
  id: string;
  issueId: string;
  scope: InboxUnsubscribeScope;
}

/**
 * The callbacks are the mutation's own, not `mutate`'s: the surfaces that fire
 * this move on to the next notification at once, and per-call callbacks die
 * with the component that made the call, taking the confirmation and its Undo
 * (or the error) with them.
 */
export interface UnsubscribeArchiveCallbacks {
  onSuccess?: (variables: UnsubscribeArchiveVariables) => void;
  onError?: (error: Error, variables: UnsubscribeArchiveVariables) => void;
}

export function useUnsubscribeArchiveInbox(callbacks: UnsubscribeArchiveCallbacks = {}) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: async ({ id, issueId, scope }: UnsubscribeArchiveVariables) => {
      if (scope === "subtree") await api.unsubscribeFromIssueSubtree(issueId);
      else await api.unsubscribeFromIssue(issueId);
      await api.archiveInbox(id);
    },
    onMutate: async ({ issueId }) => {
      await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
        old?.map((item) => (item.issue_id === issueId ? { ...item, archived: true } : item)),
      );
      return { prev };
    },
    onSuccess: (_data, vars) => callbacks.onSuccess?.(vars),
    onError: (err, vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
      callbacks.onError?.(err, vars);
    },
    onSettled: () => refreshAfterSubscriptionWrite(qc, wsId),
  });
}

/**
 * Undo for `useUnsubscribeArchiveInbox` with scope "issue": restore the
 * subscription as it was, then the item. A plain function rather than a hook,
 * because it runs from a toast that outlives the page that showed it.
 *
 * Restoring is not subscribing again: that would rewrite a commenter, delegated
 * or project-inherited subscription to manual, and the issue would start
 * delivering more than it did before the user left it.
 */
export async function undoUnsubscribeArchiveInbox(
  qc: QueryClient,
  wsId: string,
  { id, issueId }: { id: string; issueId: string },
): Promise<void> {
  try {
    await api.undoUnsubscribeFromIssue(issueId);
    await api.unarchiveInbox(id);
  } finally {
    refreshAfterSubscriptionWrite(qc, wsId);
  }
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

/**
 * Undo for a batch archive: restore the notifications it took, one request
 * per row, since a restore brings back the whole issue's group. A plain
 * function rather than a hook, because it runs from a toast that outlives the
 * page that showed it.
 */
export async function undoArchiveInboxBatch(
  qc: QueryClient,
  wsId: string,
  ids: readonly string[],
): Promise<void> {
  try {
    await Promise.all(ids.map((id) => api.unarchiveInbox(id)));
  } finally {
    refreshInboxAfterWrite(qc, wsId);
  }
}

/**
 * When the issue of an inbox item is snoozed until, as far as the cache knows;
 * null when it isn't snoozed or its time has passed.
 */
export function cachedInboxSnoozedUntil(qc: QueryClient, wsId: string, id: string): Date | null {
  const snoozed = qc.getQueryData<SnoozedInboxIssue[]>(inboxKeys.snoozed(wsId));
  const issueId = cachedInboxIssueId(qc, wsId, id) ?? snoozed?.find((entry) => entry.item_id === id)?.issue_id;
  if (!issueId) return null;
  const until =
    snoozed?.find((entry) => entry.issue_id === issueId)?.snoozed_until ??
    qc
      .getQueryData<InboxItem[]>(inboxKeys.list(wsId))
      ?.find((item) => item.issue_id === issueId && !item.archived && item.snoozed_until)?.snoozed_until;
  if (!until) return null;
  const date = new Date(until);
  return date.getTime() > Date.now() ? date : null;
}

export interface SnoozeInboxInput {
  /** Any notification on the issue; the snooze covers the whole issue. */
  id: string;
  until: Date;
}

/**
 * Snooze an issue in the inbox until a time (MUL-9333). Optimistic: the
 * issue's rows carry the time at once, which takes the issue out of the list
 * (`deduplicateInboxItems`), and the Snoozed view gains or moves the entry.
 */
export function useSnoozeInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, until }: SnoozeInboxInput) => api.snoozeInbox(id, until.toISOString()),
    onMutate: async ({ id, until }) => {
      await qc.cancelQueries({ queryKey: inboxKeys.all(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      const prevSnoozed = qc.getQueryData<SnoozedInboxIssue[]>(inboxKeys.snoozed(wsId));
      const snoozedUntil = until.toISOString();
      const issueId =
        cachedInboxIssueId(qc, wsId, id) ?? prevSnoozed?.find((entry) => entry.item_id === id)?.issue_id ?? null;
      if (issueId) {
        qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
          old?.map((item) => (item.issue_id === issueId && !item.archived ? { ...item, snoozed_until: snoozedUntil } : item)),
        );
        qc.setQueryData<SnoozedInboxIssue[]>(inboxKeys.snoozed(wsId), (old) => {
          if (!old) return old;
          const existing = old.find((entry) => entry.issue_id === issueId);
          const entry: SnoozedInboxIssue = existing
            ? { ...existing, snoozed_until: snoozedUntil }
            : { issue_id: issueId, item_id: id, snoozed_until: snoozedUntil, snoozed_at: new Date().toISOString(), updates: 0 };
          return [...old.filter((other) => other.issue_id !== issueId), entry].sort(
            (a, b) => new Date(a.snoozed_until).getTime() - new Date(b.snoozed_until).getTime(),
          );
        });
      }
      return { prev, prevSnoozed };
    },
    onError: (_err, _input, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
      if (ctx?.prevSnoozed) qc.setQueryData(inboxKeys.snoozed(wsId), ctx.prevSnoozed);
    },
    onSettled: () => {
      refreshInboxAfterWrite(qc, wsId);
    },
  });
}

export interface UnsnoozeInboxInput {
  id: string;
  /** Restore the issue as read, for undoing a snooze on an issue that was read. */
  read?: boolean;
}

/**
 * End a snooze now: the issue is back in the inbox. Optimistic on the
 * Snoozed view and on rows still in the list cache (an undo right after
 * snoozing); the rest of the list follows the server.
 */
export function useUnsnoozeInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, read }: UnsnoozeInboxInput) => api.unsnoozeInbox(id, { read }),
    onMutate: async ({ id, read }) => {
      await qc.cancelQueries({ queryKey: inboxKeys.all(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      const prevSnoozed = qc.getQueryData<SnoozedInboxIssue[]>(inboxKeys.snoozed(wsId));
      const issueId =
        prevSnoozed?.find((entry) => entry.item_id === id)?.issue_id ?? cachedInboxIssueId(qc, wsId, id);
      if (issueId) {
        qc.setQueryData<SnoozedInboxIssue[]>(inboxKeys.snoozed(wsId), (old) =>
          old?.filter((entry) => entry.issue_id !== issueId),
        );
        qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
          old?.map((item) =>
            item.issue_id === issueId && !item.archived
              ? { ...item, snoozed_until: null, ...(read !== undefined ? { read } : {}) }
              : item,
          ),
        );
      }
      return { prev, prevSnoozed };
    },
    onError: (_err, _input, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
      if (ctx?.prevSnoozed) qc.setQueryData(inboxKeys.snoozed(wsId), ctx.prevSnoozed);
    },
    onSettled: () => {
      refreshInboxAfterWrite(qc, wsId);
    },
  });
}
