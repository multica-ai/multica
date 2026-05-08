// CEREBRO-PATCH(channels-mutations): channel-rename + participant add/remove mutations (JEH-700)
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { channelKeys } from "./queries";
import { inboxKeys } from "../inbox/queries";
import { issueKeys } from "../issues/queries";
import { createLogger } from "../logger";
import type {
  Channel,
  ChannelMember,
  CreateChannelRequest,
  InboxItem,
  UpdateIssueRequest,
} from "../types";

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

// Rename a channel (kind='channel'). Channels are issues, so the title field
// goes through PUT /api/issues/{id}. We invalidate channel caches afterwards
// so the inbox row, sidebar pin, and detail header pick up the new name
// without a separate listener.
export function useUpdateChannel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: ({
      id,
      ...data
    }: { id: string } & Pick<UpdateIssueRequest, "title" | "description">) =>
      api.updateIssue(id, data),
    onMutate: async ({ id, ...data }) => {
      await Promise.all([
        qc.cancelQueries({ queryKey: channelKeys.detail(wsId, id) }),
        qc.cancelQueries({ queryKey: channelKeys.list(wsId) }),
      ]);
      const prevDetail = qc.getQueryData<Channel>(channelKeys.detail(wsId, id));
      const prevList = qc.getQueryData<Channel[]>(channelKeys.list(wsId));

      qc.setQueryData<Channel>(channelKeys.detail(wsId, id), (old) =>
        old
          ? {
              ...old,
              title: data.title ?? old.title,
              description:
                data.description !== undefined ? data.description : old.description,
            }
          : old,
      );
      qc.setQueryData<Channel[]>(channelKeys.list(wsId), (old) =>
        old?.map((c) =>
          c.id === id
            ? {
                ...c,
                title: data.title ?? c.title,
                description:
                  data.description !== undefined ? data.description : c.description,
              }
            : c,
        ),
      );
      return { prevDetail, prevList };
    },
    onError: (_err, { id }, ctx) => {
      if (ctx?.prevDetail)
        qc.setQueryData(channelKeys.detail(wsId, id), ctx.prevDetail);
      if (ctx?.prevList) qc.setQueryData(channelKeys.list(wsId), ctx.prevList);
    },
    onSettled: (_data, _err, { id }) => {
      qc.invalidateQueries({ queryKey: channelKeys.detail(wsId, id) });
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.detail(wsId, id) });
    },
  });
}

// Add or remove a channel participant. Maps onto the existing
// /api/issues/{id}/subscribe + /unsubscribe endpoints — channel participants
// are subscribers under the hood. Optimistic so the participants stack updates
// without waiting for the server round-trip.
export function useToggleChannelParticipant() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: async ({
      channelId,
      member,
      action,
    }: {
      channelId: string;
      member: ChannelMember;
      action: "add" | "remove";
    }) => {
      logger.info("toggleParticipant", { channelId, action, ...member });
      if (action === "add") {
        await api.subscribeToIssue(channelId, member.user_id, member.user_type);
      } else {
        await api.unsubscribeFromIssue(channelId, member.user_id, member.user_type);
      }
    },
    onMutate: async ({ channelId, member, action }) => {
      await Promise.all([
        qc.cancelQueries({ queryKey: channelKeys.detail(wsId, channelId) }),
        qc.cancelQueries({ queryKey: channelKeys.list(wsId) }),
      ]);
      const prevDetail = qc.getQueryData<Channel>(
        channelKeys.detail(wsId, channelId),
      );
      const prevList = qc.getQueryData<Channel[]>(channelKeys.list(wsId));

      const apply = (channel: Channel): Channel => {
        const exists = channel.participants.some(
          (p) => p.user_id === member.user_id && p.user_type === member.user_type,
        );
        if (action === "add") {
          if (exists) return channel;
          return { ...channel, participants: [...channel.participants, member] };
        }
        return {
          ...channel,
          participants: channel.participants.filter(
            (p) =>
              !(p.user_id === member.user_id && p.user_type === member.user_type),
          ),
        };
      };

      qc.setQueryData<Channel>(channelKeys.detail(wsId, channelId), (old) =>
        old ? apply(old) : old,
      );
      qc.setQueryData<Channel[]>(channelKeys.list(wsId), (old) =>
        old?.map((c) => (c.id === channelId ? apply(c) : c)),
      );
      return { prevDetail, prevList };
    },
    onError: (_err, { channelId }, ctx) => {
      if (ctx?.prevDetail)
        qc.setQueryData(channelKeys.detail(wsId, channelId), ctx.prevDetail);
      if (ctx?.prevList) qc.setQueryData(channelKeys.list(wsId), ctx.prevList);
    },
    onSettled: (_data, _err, { channelId }) => {
      qc.invalidateQueries({ queryKey: channelKeys.detail(wsId, channelId) });
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: issueKeys.subscribers(channelId) });
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
