// JEH-851 — per-user channel archive mutations. Cerebro-only feature: the
// upstream `useArchiveInbox` hook archives a single inbox notification, but
// channels/DMs need a persistent per-user archive flag that hides the row
// from the channel list until something re-surfaces it.
import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { channelKeys } from "@multica/core/channels";
import type { Channel } from "@multica/core/types";

// FIR-2791 — the caller's archived channels/DMs, for the Archived block/view
// in the inbox. Keyed under channelKeys.list(wsId) so the archive/unarchive
// invalidations (and the cerebro_channel_unarchived WS handler) refresh it
// together with the live channel list.
export function archivedChannelsOptions(wsId: string) {
  return queryOptions({
    queryKey: [...channelKeys.list(wsId), "archived"] as const,
    queryFn: () => api.listChannels({ archived_only: true }),
    staleTime: Infinity,
  });
}

// Optimistically removes the channel from the channel list cache. Server-side
// re-surface (delete archive row when a new inbox_item lands) is broadcast as
// a `cerebro_channel_unarchived` WS event and handled in cerebro-realtime.
export function useArchiveChannel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (channelId: string) => api.archiveChannel(channelId),
    onMutate: async (channelId) => {
      await qc.cancelQueries({ queryKey: channelKeys.list(wsId) });
      const prev = qc.getQueryData<Channel[]>(channelKeys.list(wsId));
      qc.setQueryData<Channel[]>(channelKeys.list(wsId), (old) =>
        old?.filter((c) => c.id !== channelId),
      );
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

export function useUnarchiveChannel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (channelId: string) => api.unarchiveChannel(channelId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
  });
}
