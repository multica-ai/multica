import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { channelKeys } from "./queries";
import { inboxKeys } from "../inbox/queries";
import { createLogger } from "../logger";
import type { Channel, CreateChannelRequest, InboxItem } from "../types";

const logger = createLogger("channels.mut");

// Marks every unread inbox_item for this channel/DM and the current user as
// read in one server call — across both inbox- and notifications-routed rows.
// Optimistically clears unread_count on the channel cache and the read flag on
// any inbox rows for the same issue so the badge disappears without a refetch.
export function useMarkChannelRead() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (channelId: string) => api.markChannelRead(channelId),
    onMutate: async (channelId) => {
      await Promise.all([
        qc.cancelQueries({ queryKey: channelKeys.list(wsId) }),
        qc.cancelQueries({ queryKey: channelKeys.detail(wsId, channelId) }),
        qc.cancelQueries({ queryKey: inboxKeys.list(wsId) }),
      ]);
      const prevList = qc.getQueryData<Channel[]>(channelKeys.list(wsId));
      const prevDetail = qc.getQueryData<Channel>(channelKeys.detail(wsId, channelId));
      const prevInbox = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));

      qc.setQueryData<Channel[]>(channelKeys.list(wsId), (old) =>
        old?.map((c) => (c.id === channelId ? { ...c, unread_count: 0 } : c)),
      );
      qc.setQueryData<Channel>(channelKeys.detail(wsId, channelId), (old) =>
        old ? { ...old, unread_count: 0 } : old,
      );
      qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), (old) =>
        old?.map((item) =>
          item.issue_id === channelId && !item.read ? { ...item, read: true } : item,
        ),
      );

      return { prevList, prevDetail, prevInbox };
    },
    onError: (_err, channelId, ctx) => {
      if (ctx?.prevList) qc.setQueryData(channelKeys.list(wsId), ctx.prevList);
      if (ctx?.prevDetail) qc.setQueryData(channelKeys.detail(wsId, channelId), ctx.prevDetail);
      if (ctx?.prevInbox) qc.setQueryData(inboxKeys.list(wsId), ctx.prevInbox);
    },
    onSettled: (_data, _err, channelId) => {
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: channelKeys.detail(wsId, channelId) });
      qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
    },
  });
}

export function useCreateChannel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (data: CreateChannelRequest): Promise<Channel> => {
      logger.info("createChannel.start", {
        kind: data.kind,
        memberCount: data.member_ids.length,
        agentCount: data.agent_ids.length,
      });
      return api.createChannel(data);
    },
    onSuccess: (channel) => {
      logger.info("createChannel.success", {
        channelId: channel.id,
        kind: channel.kind,
      });
    },
    onError: (err) => {
      logger.error("createChannel.error", err);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
  });
}
