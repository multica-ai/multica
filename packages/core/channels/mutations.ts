import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { channelKeys } from "./queries";

export function useCreateChannel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: { name: string; description?: string; lark_chat_id?: string }) => api.createChannel(data),
    onSuccess: () => qc.invalidateQueries({ queryKey: channelKeys.list(wsId) }),
  });
}

export function useSendChannelMessage() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ channelId, content }: { channelId: string; content: string }) => api.sendChannelMessage(channelId, content),
    onSuccess: (msg) => {
      qc.invalidateQueries({ queryKey: channelKeys.messages(msg.channel_id) });
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
  });
}

export function useSetChannelTyping() {
  return useMutation({
    mutationFn: ({ channelId, isTyping }: { channelId: string; isTyping: boolean }) => api.setChannelTyping(channelId, isTyping),
  });
}

export function useAddChannelMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ channelId, memberType, memberId }: { channelId: string; memberType: "user" | "agent"; memberId: string }) =>
      api.addChannelMember(channelId, { member_type: memberType, member_id: memberId }),
    onSuccess: (_data, vars) => qc.invalidateQueries({ queryKey: channelKeys.members(vars.channelId) }),
  });
}

export function useRemoveChannelMember() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ channelId, memberType, memberId }: { channelId: string; memberType: "user" | "agent"; memberId: string }) =>
      api.removeChannelMember(channelId, memberType, memberId),
    onSuccess: (_data, vars) => qc.invalidateQueries({ queryKey: channelKeys.members(vars.channelId) }),
  });
}
