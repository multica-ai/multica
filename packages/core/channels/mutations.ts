import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { channelKeys } from "./queries";
import type { ListChannelsResponse } from "../types";

/**
 * Clears the channel's unread indicator server-side. Optimistically flips
 * has_unread to false in the cached channel list so the green dot drops
 * immediately. The server broadcasts a channel event so other devices
 * also sync (see use-realtime-sync.ts channel_message handler).
 */
export function useMarkChannelRead() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (channelId: string) => api.markChannelRead(channelId),
    onMutate: async (channelId) => {
      await qc.cancelQueries({ queryKey: channelKeys.list(wsId) });

      const prev = qc.getQueryData<ListChannelsResponse>(channelKeys.list(wsId));

      qc.setQueryData<ListChannelsResponse>(channelKeys.list(wsId), (old) =>
        old
          ? {
              ...old,
              channels: old.channels.map((c) =>
                c.id === channelId ? { ...c, has_unread: false } : c,
              ),
            }
          : old,
      );

      return { prev };
    },
    onError: (_err, _channelId, ctx) => {
      if (ctx?.prev) qc.setQueryData(channelKeys.list(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
  });
}
