"use client";

import { useMutation, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { inboxKeys } from "@multica/core/inbox/queries";
import type { InboxItem } from "@multica/core/types";

function updateInboxIssueSiblings(
  items: InboxItem[] | undefined,
  id: string,
  update: (item: InboxItem) => InboxItem,
): InboxItem[] | undefined {
  const target = items?.find((item) => item.id === id);
  const issueId = target?.issue_id;
  return items?.map((item) =>
    item.id === id || (issueId && item.issue_id === issueId)
      ? update(item)
      : item,
  );
}

type InboxPages = InfiniteData<InboxItem[], number>;

function isInboxPages(value: InboxItem[] | InboxPages | undefined): value is InboxPages {
  return !!value && !Array.isArray(value) && "pages" in value;
}

function filterArchivedCache(
  old: InboxItem[] | InboxPages | undefined,
  id: string,
  issueId: string | null | undefined,
): InboxItem[] | InboxPages | undefined {
  const keep = (item: InboxItem) =>
    item.id === id || (issueId && item.issue_id === issueId) ? false : true;
  if (isInboxPages(old)) {
    return {
      ...old,
      pages: old.pages.map((page) => page.filter(keep)),
    };
  }
  return old?.filter(keep);
}

function findArchivedCacheItem(
  old: InboxItem[] | InboxPages | undefined,
  id: string,
): InboxItem | undefined {
  if (isInboxPages(old)) {
    return old.pages.flat().find((item) => item.id === id);
  }
  return old?.find((item) => item.id === id);
}

/**
 * Mute an inbox item until the supplied timestamp. Optimistic — sets
 * `muted_until` locally before the server round-trip and rolls back on
 * error, mirroring the upstream archive/read mutations.
 */
export function useMuteInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, mutedUntil }: { id: string; mutedUntil: Date }) =>
      api.muteInbox(id, mutedUntil),
    onMutate: async ({ id, mutedUntil }) => {
      await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
        updateInboxIssueSiblings(old, id, (item) =>
          ({ ...item, muted_until: mutedUntil.toISOString() }),
        ),
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

export function useUnmuteInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.unmuteInbox(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
        updateInboxIssueSiblings(old, id, (item) => ({ ...item, muted_until: null })),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

/**
 * Restore an archived inbox row to the active inbox. Mirror of the upstream
 * `useArchiveInbox` — optimistically flips `archived` back to `false` so the
 * row disappears from the archived view immediately, then invalidates both
 * the active and archived list caches so the row resurfaces in the active
 * inbox without a manual reload.
 */
export function useUnarchiveInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.unarchiveInbox(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: inboxKeys.archivedList(wsId) });
      const prev = qc.getQueryData<InboxItem[] | InboxPages>(inboxKeys.archivedList(wsId));
      const target = findArchivedCacheItem(prev, id);
      const issueId = target?.issue_id;
      qc.setQueryData<InboxItem[] | InboxPages>(inboxKeys.archivedList(wsId), (old) =>
        filterArchivedCache(old, id, issueId),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.archivedList(wsId), ctx.prev);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: inboxKeys.archivedList(wsId) });
      // Active inbox query must refetch too — the unarchived row needs to
      // resurface there without a manual reload.
      void qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

/**
 * Force an inbox row back to unread. Counterpart to the existing
 * `useMarkInboxRead` from `@multica/core/inbox/mutations`.
 */
export function useMarkInboxUnread() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.markInboxUnread(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      const prev = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
        old?.map((item) =>
          item.id === id ? { ...item, read: false } : item,
        ),
      );
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(inboxKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}
