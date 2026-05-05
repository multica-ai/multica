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

// Note: useToggleChannelReaction below also imports ChannelMessage. The
// import above already has it. ChannelReaction is referenced inline in
// the optimistic synthetic record so no separate import is needed.

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
        reactions: [],
        thread_reply_count: 0,
        attachments: [],
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
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (messageId: string) => api.markChannelRead(channelId, { message_id: messageId }),
    // Optimistically clear the unread badge in the sidebar so the user
    // gets instant feedback. The server will eventually return the same
    // shape (or a more accurate count if a message arrived between the
    // optimistic write and the response) when the list refetches.
    onMutate: (messageId: string) => {
      qc.setQueryData<Channel[] | undefined>(channelKeys.list(wsId), (current) => {
        if (!current) return current;
        return current.map((c) =>
          c.id === channelId
            ? { ...c, unread_count: 0, last_read_message_id: messageId }
            : c,
        );
      });
    },
    // After the mutation settles (success OR failure) refetch the canonical
    // counts. On failure this also rolls back the optimistic write.
    onSettled: () => {
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
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

// Phase 5 — edit a channel message in place. Optimistic patch in the
// timeline cache so the new content shows instantly; rollback on error.
// edited_at is filled by the server return value, so the optimistic
// path leaves it null and the settle phase fixes it up.
export function useUpdateChannelMessage(channelId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (params: { messageId: string; content: string }) =>
      api.updateChannelMessage(channelId, params.messageId, params.content),
    onMutate: async (params) => {
      await qc.cancelQueries({ queryKey: channelKeys.messages(channelId) });
      const prev = qc.getQueryData<ChannelMessage[]>(channelKeys.messages(channelId));
      qc.setQueryData<ChannelMessage[]>(channelKeys.messages(channelId), (old) =>
        old?.map((m) =>
          m.id === params.messageId ? { ...m, content: params.content } : m,
        ),
      );
      return { prev };
    },
    onError: (_err, _params, ctx) => {
      if (ctx?.prev) qc.setQueryData(channelKeys.messages(channelId), ctx.prev);
    },
    onSettled: (_data, _err, params) => {
      qc.invalidateQueries({ queryKey: channelKeys.messages(channelId) });
      qc.invalidateQueries({ queryKey: channelKeys.thread(params.messageId) });
    },
  });
}

// Phase 5 — soft-delete a channel message (author or channel admin).
// Optimistic update flips deleted_at on the cached row so the timeline
// renders the "[message deleted]" placeholder instantly. The thread
// cache (if any) is also flipped so a panel that's currently displaying
// the message gets the placeholder treatment.
export function useDeleteChannelMessage(channelId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (messageId: string) => api.deleteChannelMessage(channelId, messageId),
    onMutate: async (messageId) => {
      await qc.cancelQueries({ queryKey: channelKeys.messages(channelId) });
      const prev = qc.getQueryData<ChannelMessage[]>(channelKeys.messages(channelId));
      const stamp = new Date().toISOString();
      qc.setQueryData<ChannelMessage[]>(channelKeys.messages(channelId), (old) =>
        old?.filter((m) => m.id !== messageId).concat(
          (old ?? [])
            .filter((m) => m.id === messageId)
            .map((m) => ({ ...m, deleted_at: stamp })),
        ),
      );
      return { prev };
    },
    onError: (_err, _messageId, ctx) => {
      if (ctx?.prev) qc.setQueryData(channelKeys.messages(channelId), ctx.prev);
    },
    onSettled: (_data, _err, messageId) => {
      qc.invalidateQueries({ queryKey: channelKeys.messages(channelId) });
      qc.invalidateQueries({ queryKey: channelKeys.thread(messageId) });
    },
  });
}

// Phase 4 reaction mutations. Optimistic toggle: reaction immediately
// appears/disappears on the message in the cache; a server failure
// reverts and surfaces a toast.
//
// Channel-scope: the per-channel messages cache is the source of truth
// for reaction display. The thread cache (if open) gets invalidated too
// since a reaction on a thread reply needs to reflect there.
export function useToggleChannelReaction(channelId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (params: { messageId: string; emoji: string; currentlyReacted: boolean }) => {
      if (params.currentlyReacted) {
        await api.removeChannelReaction(channelId, params.messageId, params.emoji);
        return null;
      }
      return api.addChannelReaction(channelId, params.messageId, params.emoji);
    },
    onMutate: async (params) => {
      await qc.cancelQueries({ queryKey: channelKeys.messages(channelId) });
      const prevMessages = qc.getQueryData<ChannelMessage[]>(channelKeys.messages(channelId));
      // Optimistic patch in the message list. We don't have the actor's
      // full reaction record handy until the server returns; for the
      // optimistic add we synthesize a minimal one with id="optimistic-…"
      // that the WS event will replace.
      qc.setQueryData<ChannelMessage[]>(channelKeys.messages(channelId), (old) =>
        old?.map((m) => {
          if (m.id !== params.messageId) return m;
          if (params.currentlyReacted) {
            return {
              ...m,
              reactions: m.reactions.filter((r) => r.emoji !== params.emoji),
            };
          }
          return {
            ...m,
            reactions: [
              ...m.reactions,
              {
                id: `optimistic-${Date.now()}`,
                channel_message_id: m.id,
                actor_type: "member",
                actor_id: "self",
                emoji: params.emoji,
                created_at: new Date().toISOString(),
              },
            ],
          };
        }),
      );
      return { prevMessages };
    },
    onError: (_err, _params, ctx) => {
      if (ctx?.prevMessages) {
        qc.setQueryData(channelKeys.messages(channelId), ctx.prevMessages);
      }
    },
    onSettled: (_data, _err, params) => {
      qc.invalidateQueries({ queryKey: channelKeys.messages(channelId) });
      qc.invalidateQueries({ queryKey: channelKeys.thread(params.messageId) });
    },
  });
}

// Type re-export so callers using the Channel returned from a mutation
// don't need to also import from "../types".
export type { Channel, ChannelMembership };
