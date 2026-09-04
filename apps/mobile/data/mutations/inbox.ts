/**
 * Mobile inbox mutations. Mirrors the optimistic-update + invalidate pattern
 * of packages/core/inbox/mutations.ts — written here in mobile-owned code
 * per Sharing Principles (no runtime imports from @multica/core mutations).
 *
 * Behavioral parity:
 *   - mark-read: flip `read` to true locally; rollback on error; settle invalidate.
 *     `onMutate` writes setQueryData BEFORE awaiting cancelQueries — this is
 *     load-bearing for iOS Stack push transitions: when the user taps an
 *     inbox row and we router.push to issue/[id], iOS captures a snapshot of
 *     the source view for the slide animation; if the read-state flip hadn't
 *     landed in cache by that snapshot, the row appears unread frozen in
 *     the animation. Synchronous setQueryData ensures the next paint already
 *     has the flipped state. (Previously the caller did this hack at tap
 *     site; moved into the mutation so every caller benefits.)
 *   - archive single: flip `archived` to true on the item AND on every other
 *     inbox row that shares the same `issue_id` (web does the same — see
 *     packages/core/inbox/mutations.ts:37-46). Visually the row disappears
 *     because `deduplicateInboxItems` (apps/mobile/lib/inbox-display.ts)
 *     filters archived items out before render.
 *   - mark-all-read: flip `read` to true on every non-archived row (matches
 *     web; the server-side query does the same predicate).
 *   - archive batch (all / all-read / completed): no optimistic patch — the
 *     row predicates depend on server-side state (e.g. issue.status="done"
 *     isn't carried on every row, and mobile shouldn't re-derive the filter).
 *     Just invalidate on settle. Matches web.
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { QueryClient } from "@tanstack/react-query";
import type { InboxItem, InboxWorkspaceUnread } from "@multica/core/types";
import { api } from "@/data/api";
import { inboxKeys } from "@/data/queries/inbox";
import { useWorkspaceStore } from "@/data/workspace-store";
import { deduplicateInboxItems } from "@/lib/inbox-display";

/**
 * Refresh the cross-workspace unread summary that backs the tab badge. It
 * lives under its own account-level key, so invalidating the workspace list
 * does not reach it — every mutation here can change the number it holds.
 */
function invalidateUnreadSummary(qc: QueryClient) {
  qc.invalidateQueries({ queryKey: inboxKeys.unreadSummary() });
}

/**
 * Re-derive this workspace's unread count from the just-patched list cache
 * and write it into the summary cache, so the tab badge follows an optimistic
 * patch instead of waiting for the round-trip.
 *
 * Mirrors syncUnreadSummaryFromList in packages/core/inbox/mutations.ts, and
 * reuses the same `deduplicateInboxItems` the inbox screen renders through —
 * the badge can therefore never disagree with the rows on screen. Both caches
 * are read defensively: absent means nothing to be optimistic about, and the
 * server value stands until `onSettled`.
 */
function syncUnreadSummaryFromList(qc: QueryClient, wsId: string | null) {
  if (!wsId) return;
  const items = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
  if (!items) return;
  const count = deduplicateInboxItems(items).filter((i) => !i.read).length;
  qc.setQueryData<InboxWorkspaceUnread[]>(inboxKeys.unreadSummary(), (old) => {
    if (!old) return old;
    // Order carries no meaning — consumers look up by workspace id. A
    // zero-count workspace is dropped, mirroring the server response.
    const others = old.filter((entry) => entry.workspace_id !== wsId);
    return count > 0 ? [...others, { workspace_id: wsId, count }] : others;
  });
}

export function useMarkInboxRead() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (id: string) => api.markInboxRead(id),
    onMutate: async (id) => {
      const key = inboxKeys.list(wsId);
      // Synchronous patch FIRST — see the file-level doc comment for why.
      qc.setQueryData<InboxItem[]>(key, (old) =>
        old?.map((item) => (item.id === id ? { ...item, read: true } : item)),
      );
      // Same frame as the row patch, so the badge and the row never disagree
      // across the transition.
      syncUnreadSummaryFromList(qc, wsId);
      // Then the standard cancel + snapshot dance for rollback.
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<InboxItem[]>(key);
      return { prev, key };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(ctx.key, ctx.prev);
      syncUnreadSummaryFromList(qc, wsId);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
      invalidateUnreadSummary(qc);
    },
  });
}

export function useArchiveInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (id: string) => api.archiveInbox(id),
    onMutate: async (id) => {
      const key = inboxKeys.list(wsId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<InboxItem[]>(key);
      // Match web: archive every row that shares the same issue_id — the
      // single archive endpoint archives all sibling rows server-side too
      // (`server/internal/queries/inbox.sql` UPDATE … WHERE issue_id = ?).
      // Patching only the tapped row would let dedup'd siblings briefly
      // resurface between the request and the WS invalidate.
      const target = prev?.find((i) => i.id === id);
      const issueId = target?.issue_id ?? null;
      qc.setQueryData<InboxItem[]>(key, (old) =>
        old?.map((item) =>
          item.id === id || (issueId && item.issue_id === issueId)
            ? { ...item, archived: true }
            : item,
        ),
      );
      // Archiving an unread issue group drops it out of the badge at once.
      syncUnreadSummaryFromList(qc, wsId);
      return { prev, key };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(ctx.key, ctx.prev);
      syncUnreadSummaryFromList(qc, wsId);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
      invalidateUnreadSummary(qc);
    },
  });
}

export function useMarkAllInboxRead() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: () => api.markAllInboxRead(),
    onMutate: async () => {
      const key = inboxKeys.list(wsId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<InboxItem[]>(key);
      qc.setQueryData<InboxItem[]>(key, (old) =>
        old?.map((item) =>
          !item.archived ? { ...item, read: true } : item,
        ),
      );
      syncUnreadSummaryFromList(qc, wsId);
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(ctx.key, ctx.prev);
      syncUnreadSummaryFromList(qc, wsId);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
      invalidateUnreadSummary(qc);
    },
  });
}

// Batch archive mutations — invalidate-only, matching web. The optimistic
// path isn't worth the complexity: archive-completed depends on the issue
// status of each linked issue (not carried on InboxItem), and predicting
// that on the client risks divergence with the server's SQL filter. The badge
// therefore catches up on settle rather than moving instantly.
export function useArchiveAllInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    mutationFn: () => api.archiveAllInbox(),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
      invalidateUnreadSummary(qc);
    },
  });
}

export function useArchiveAllReadInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    mutationFn: () => api.archiveAllReadInbox(),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
      invalidateUnreadSummary(qc);
    },
  });
}

export function useArchiveCompletedInbox() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  return useMutation({
    mutationFn: () => api.archiveCompletedInbox(),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
      invalidateUnreadSummary(qc);
    },
  });
}
