import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { channelKeys } from "./queries";
import { createLogger } from "../logger";
import type {
  Channel,
  ChannelMembership,
  ChannelMessage,
  CreateChannelRequest,
  UpdateChannelRequest,
  AddChannelMemberRequest,
  CreateChannelMessageRequest,
  CreateOrFetchDMRequest,
} from "../types";

const logger = createLogger("channels.mut");

export function useCreateChannel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateChannelRequest) => {
      logger.info("createChannel.start", { name: data.name, visibility: data.visibility });
      return api.createChannel(data);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
  });
}

export function useUpdateChannel(channelId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: UpdateChannelRequest) => api.updateChannel(channelId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: channelKeys.detail(wsId, channelId) });
    },
  });
}

export function useArchiveChannel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (channelId: string) => api.archiveChannel(channelId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
  });
}

export function useAddChannelMember(channelId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: AddChannelMemberRequest) => api.addChannelMember(channelId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: channelKeys.members(channelId) });
    },
  });
}

export function useRemoveChannelMember(channelId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (params: { memberType: string; memberId: string }) =>
      api.removeChannelMember(channelId, params.memberType, params.memberId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: channelKeys.members(channelId) });
    },
  });
}

// Optimistic message send: append the user's message to the cached list
// immediately so the UI feels instant, then reconcile when the server's
// canonical row arrives via the channel:message WS event.
export function useSendChannelMessage(channelId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateChannelMessageRequest) => api.sendChannelMessage(channelId, data),
    onMutate: async (data) => {
      await qc.cancelQueries({ queryKey: channelKeys.messages(channelId) });
      const prev = qc.getQueryData<ChannelMessage[]>(channelKeys.messages(channelId));
      const optimistic: ChannelMessage = {
        id: `optimistic-${Date.now()}`,
        channel_id: channelId,
        author_type: "member",
        author_id: "self",
        content: data.content,
        parent_message_id: data.parent_message_id ?? null,
        edited_at: null,
        deleted_at: null,
        created_at: new Date().toISOString(),
      };
      // Newest-first ordering matches the list query.
      qc.setQueryData<ChannelMessage[]>(channelKeys.messages(channelId), (old) =>
        old ? [optimistic, ...old] : [optimistic],
      );
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(channelKeys.messages(channelId), ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: channelKeys.messages(channelId) });
    },
  });
}

export function useMarkChannelRead(channelId: string) {
  return useMutation({
    mutationFn: (messageId: string) => api.markChannelRead(channelId, { message_id: messageId }),
  });
}

export function useCreateOrFetchDM() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateOrFetchDMRequest) => api.createOrFetchDM(data),
    onSuccess: (channel: Channel) => {
      // The DM may be brand new, so refresh the list.
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
      qc.setQueryData(channelKeys.detail(wsId, channel.id), channel);
    },
  });
}

// Type re-export so callers using the Channel returned from a mutation
// don't need to also import from "../types".
export type { Channel, ChannelMembership };
