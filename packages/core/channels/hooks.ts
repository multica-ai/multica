import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useAuthStore } from "../auth";
import type { Channel, ChannelMessage, ChannelMember } from "../types";

export const channelKeys = {
  all: ["channels"] as const,
  lists: () => [...channelKeys.all, "list"] as const,
  list: (workspaceId: string) => [...channelKeys.lists(), workspaceId] as const,
  details: () => [...channelKeys.all, "detail"] as const,
  detail: (id: string) => [...channelKeys.details(), id] as const,
  messages: (channelId: string) => [...channelKeys.all, "messages", channelId] as const,
  members: (channelId: string) => [...channelKeys.all, "members", channelId] as const,
};

export function useChannels(workspaceId?: string) {
  return useQuery({
    queryKey: channelKeys.list(workspaceId!),
    queryFn: () => api.listChannels(workspaceId!),
    enabled: !!workspaceId,
  });
}

export function useChannel(workspaceId?: string, channelId?: string) {
  return useQuery({
    queryKey: channelKeys.detail(channelId!),
    queryFn: () => api.getChannel(workspaceId!, channelId!),
    enabled: !!workspaceId && !!channelId,
  });
}

export function useCreateChannel(workspaceId?: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: { name: string; description?: string; is_private: boolean }) =>
      api.createChannel(workspaceId!, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: channelKeys.list(workspaceId!) });
    },
  });
}

export function useChannelMessages(workspaceId?: string, channelId?: string) {
  return useQuery({
    queryKey: channelKeys.messages(channelId!),
    queryFn: () => api.listChannelMessages(workspaceId!, channelId!),
    enabled: !!workspaceId && !!channelId,
  });
}

export function useCreateChannelMessage(workspaceId?: string, channelId?: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (content: string) =>
      api.createChannelMessage(workspaceId!, channelId!, { content }),
    onMutate: async (content) => {
      await qc.cancelQueries({ queryKey: channelKeys.messages(channelId!) });
      const previous = qc.getQueryData<ChannelMessage[]>(channelKeys.messages(channelId!));
      const user = useAuthStore.getState().user;

      if (previous && user) {
        const optimisticMsg: ChannelMessage = {
          id: `temp-${Date.now()}`,
          channel_id: channelId!,
          author_id: user.id,
          content,
          created_at: new Date().toISOString(),
        };
        qc.setQueryData<ChannelMessage[]>(channelKeys.messages(channelId!), [optimisticMsg, ...previous]);
      }
      return { previous };
    },
    onError: (err, variables, context) => {
      if (context?.previous) {
        qc.setQueryData(channelKeys.messages(channelId!), context.previous);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: channelKeys.messages(channelId!) });
    },
  });
}
