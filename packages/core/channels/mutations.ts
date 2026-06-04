import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { channelKeys } from "./queries";
import type {
  CreateChannelRequest,
  AddMemberRequest,
  SendChannelMessageRequest,
} from "../types/channel";

// ── Create Channel ──────────────────────────────────────

export function useCreateChannel(wsId: string) {
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (req: CreateChannelRequest) => {
      return api.createChannel(req);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
  });
}

// ── Archive Channel ─────────────────────────────────────

export function useArchiveChannel(wsId: string, channelId: string) {
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async () => {
      await api.deleteChannel(channelId);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
    },
  });
}

// ── Add Member ──────────────────────────────────────────

export function useAddChannelMember(wsId: string, channelId: string) {
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (req: AddMemberRequest) => {
      return api.addChannelMember(channelId, req);
    },
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: channelKeys.members(wsId, channelId),
      });
    },
  });
}

// ── Remove Member ───────────────────────────────────────

export function useRemoveChannelMember(
  wsId: string,
  channelId: string,
) {
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async ({ memberId, memberType }: { memberId: string; memberType?: string }) => {
      await api.removeChannelMember(channelId, memberId, memberType);
    },
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: channelKeys.members(wsId, channelId),
      });
    },
  });
}

// ── Send Message ────────────────────────────────────────

export function useSendChannelMessage(wsId: string, channelId: string) {
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (req: SendChannelMessageRequest) => {
      return api.sendChannelMessage(channelId, req);
    },
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: channelKeys.messages(wsId, channelId),
      });
    },
  });
}

// ── Mark Read ───────────────────────────────────────────

export function useMarkChannelRead(_wsId: string, channelId: string) {
  return useMutation({
    mutationFn: async (messageId: string) => {
      await api.markChannelRead(channelId, messageId);
    },
  });
}
