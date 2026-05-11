// JEH-851 — per-user channel archive mutations. Cerebro-only feature: the
// upstream `useArchiveInbox` hook archives a single inbox notification, but
// channels/DMs need a persistent per-user archive flag that hides the row
// from the channel list until something re-surfaces it.
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { channelKeys } from "@multica/core/channels";
import type { Channel } from "@multica/core/types";

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
