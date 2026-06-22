// FIR-1854 — mark a single channel/DM thread read, independent of the channel.
// Opening a thread row clears only that thread's unread replies; the channel's
// own unread state (and any other thread) is untouched. The mirror of
// useMarkChannelRead, but scoped to one thread_root_id.
"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { channelKeys } from "@multica/core/channels";
import { inboxKeys } from "@multica/core/inbox/queries";
import type { InboxItem } from "@multica/core/types";

export interface MarkThreadReadVars {
  channelId: string;
  rootId: string;
}

// FIR-1854 — flip ONLY this thread's unread inbox rows to read. A row belongs to
// the thread when it targets this channel (issue_id) AND carries this
// thread_root_id. The channel's own top-level messages (no thread_root_id) and
// every other thread — including the same root in a different channel — are left
// untouched. Pure so the "reading a thread clears only that thread, never the
// channel or a sibling thread" guarantee is unit-testable without a DOM.
export function markThreadReadInInbox(
  items: InboxItem[] | undefined,
  channelId: string,
  rootId: string,
): InboxItem[] | undefined {
  return items?.map((item) =>
    item.issue_id === channelId && item.details?.thread_root_id === rootId && !item.read
      ? { ...item, read: true }
      : item,
  );
}

export function useMarkThreadRead() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: ({ channelId, rootId }: MarkThreadReadVars) => api.markThreadRead(channelId, rootId),
    onMutate: async ({ channelId, rootId }) => {
      await qc.cancelQueries({ queryKey: inboxKeys.list(wsId) });
      const prevInbox = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
      // Only the rows of THIS thread flip to read — the channel's own unread
      // (top-level messages) and other threads stay untouched.
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
        markThreadReadInInbox(old, channelId, rootId),
      );
      return { prevInbox };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevInbox) qc.setQueryData(inboxKeys.list(wsId), ctx.prevInbox);
    },
    onSettled: (_data, _err, { channelId }) => {
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: channelKeys.detail(wsId, channelId) });
    },
  });
}
