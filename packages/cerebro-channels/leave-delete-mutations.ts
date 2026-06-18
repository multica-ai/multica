// TECH-3758 — leave / delete channel mutations. Cerebro-only: the Chat
// interface needs deliberate channel removal that is distinct from inbox
// archive. "Leave" removes the caller's own subscription (the channel leaves
// their roster); "Delete" removes the channel for everyone (creator/admin/
// owner only). Both drop the row from the inbox list AND the roster list,
// which share the channelKeys.list cache prefix.
import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { channelKeys } from "@multica/core/channels";
import type { Channel } from "@multica/core/types";

// Optimistically removes the channel from every cached channel list under the
// workspace prefix (the inbox list and the include-archived roster list).
function removeFromChannelCaches(qc: QueryClient, wsId: string, channelId: string) {
  qc.setQueriesData<Channel[]>({ queryKey: channelKeys.list(wsId) }, (old) =>
    Array.isArray(old) ? old.filter((c) => c.id !== channelId) : old,
  );
}

function useRemoveChannelMutation(call: (channelId: string) => Promise<void>) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (channelId: string) => call(channelId),
    onMutate: async (channelId) => {
      await qc.cancelQueries({ queryKey: channelKeys.list(wsId) });
      const prev = qc.getQueriesData<Channel[]>({ queryKey: channelKeys.list(wsId) });
      removeFromChannelCaches(qc, wsId, channelId);
      return { prev };
    },
    onError: (_err, _channelId, ctx) => {
      ctx?.prev?.forEach(([key, data]) => qc.setQueryData(key, data));
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
  });
}

// Leaving removes the caller's own subscription — gated server-side by the
// channel's allow_self_leave policy.
export function useLeaveChannel() {
  return useRemoveChannelMutation((channelId) => api.leaveChannel(channelId));
}

// Deleting removes the channel for everyone — gated server-side to the channel
// creator and workspace admins/owners.
export function useDeleteChannel() {
  return useRemoveChannelMutation((channelId) => api.deleteChannel(channelId));
}
