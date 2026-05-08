"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { inboxKeys } from "@multica/core/inbox/queries";
import type { InboxItem } from "@multica/core/types";

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
        old?.map((item) =>
          item.id === id
            ? { ...item, muted_until: mutedUntil.toISOString() }
            : item,
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
        old?.map((item) =>
          item.id === id ? { ...item, muted_until: null } : item,
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
