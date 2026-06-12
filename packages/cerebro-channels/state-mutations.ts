// TECH-3352 — per-user "remind me" (snooze) + "mark as unread" for channels
// and DMs. Cerebro-only: the upstream channel list has read/archive but no
// snooze or manual-unread, which inbox notifications already have. These
// mutations mirror the inbox mute/unread hooks against the channel cache.
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { channelKeys } from "@multica/core/channels";
import type { Channel } from "@multica/core/types";

function patchChannelList(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  channelId: string,
  patch: (c: Channel) => Channel,
) {
  qc.setQueryData<Channel[]>(channelKeys.list(wsId), (old) =>
    old?.map((c) => (c.id === channelId ? patch(c) : c)),
  );
}

// Snooze ("remind me later"): hides the conversation from the normal inbox
// views until `mutedUntil`, then it resurfaces on its own.
export function useMuteChannel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, mutedUntil }: { id: string; mutedUntil: Date }) =>
      api.muteChannel(id, mutedUntil),
    onMutate: async ({ id, mutedUntil }) => {
      await qc.cancelQueries({ queryKey: channelKeys.list(wsId) });
      const prev = qc.getQueryData<Channel[]>(channelKeys.list(wsId));
      patchChannelList(qc, wsId, id, (c) => ({ ...c, muted_until: mutedUntil.toISOString() }));
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(channelKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
  });
}

export function useUnmuteChannel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.unmuteChannel(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: channelKeys.list(wsId) });
      const prev = qc.getQueryData<Channel[]>(channelKeys.list(wsId));
      patchChannelList(qc, wsId, id, (c) => ({ ...c, muted_until: null }));
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(channelKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
  });
}

// Re-flag a read conversation as unread (bold row + stripe). The server stores
// a manual-unread marker; the channel list reports unread_count >= 1 for it.
export function useMarkChannelUnread() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.markChannelUnread(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: channelKeys.list(wsId) });
      const prev = qc.getQueryData<Channel[]>(channelKeys.list(wsId));
      patchChannelList(qc, wsId, id, (c) => ({
        ...c,
        unread_count: Math.max(1, c.unread_count),
      }));
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(channelKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
  });
}
