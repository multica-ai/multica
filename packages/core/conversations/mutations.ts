import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { createLogger } from "../logger";
import { createSafeId } from "../utils";
import {
  conversationKeys,
  markConversationReadInCache,
  OPTIMISTIC_SEQ,
  removeConversationMessage,
  upsertConversationMessage,
} from "./queries";
import type { Conversation, ConversationMember, CreateChannelRequest } from "../types";

const logger = createLogger("conversations.mut");

function useConversationListRefresh() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return (conversation?: Conversation) => {
    if (conversation) qc.setQueryData(conversationKeys.detail(wsId, conversation.id), conversation);
    qc.invalidateQueries({ queryKey: conversationKeys.list(wsId) });
  };
}

export function useCreateChannel() {
  const refresh = useConversationListRefresh();
  return useMutation({
    mutationFn: (data: CreateChannelRequest) => api.createChannel(data),
    onSuccess: (conversation) => refresh(conversation),
    onError: (error) => logger.error("createChannel.error", { error }),
  });
}

export function useOpenDirectConversation() {
  const refresh = useConversationListRefresh();
  return useMutation({
    mutationFn: (userId: string) => api.openDirectConversation(userId),
    onSuccess: (conversation) => refresh(conversation),
    onError: (error) => logger.error("openDirectConversation.error", { error }),
  });
}

export function useUpdateConversation() {
  const refresh = useConversationListRefresh();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string; name?: string; about?: string; archived?: boolean }) =>
      api.updateConversation(id, data),
    onSuccess: (conversation) => refresh(conversation),
    onError: (error) => logger.error("updateConversation.error", { error }),
  });
}

export function useAddConversationMembers() {
  const refresh = useConversationListRefresh();
  return useMutation({
    mutationFn: ({ id, members }: { id: string; members: ConversationMember[] }) =>
      api.addConversationMembers(id, members),
    onSuccess: (conversation) => refresh(conversation),
    onError: (error) => logger.error("addConversationMembers.error", { error }),
  });
}

export function useRemoveConversationMember() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, member }: { id: string; member: ConversationMember }) =>
      api.removeConversationMember(id, member),
    onSettled: (_data, _error, { id }) => {
      qc.invalidateQueries({ queryKey: conversationKeys.detail(wsId, id) });
      qc.invalidateQueries({ queryKey: conversationKeys.list(wsId) });
    },
    onError: (error) => logger.error("removeConversationMember.error", { error }),
  });
}

export function useOpenConversationThread() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ conversationId, rootMessageId }: { conversationId: string; rootMessageId: string }) =>
      api.openConversationThread(conversationId, rootMessageId),
    onSuccess: (thread) => qc.setQueryData(conversationKeys.detail(wsId, thread.id), thread),
    onError: (error) => logger.error("openConversationThread.error", { error }),
  });
}

/**
 * Send a message. It shows at once as the viewer's own message; the server's
 * copy (from the response or the realtime event, whichever lands first)
 * replaces it by `client_message_id`, and a retry with that id never posts
 * twice.
 */
export function useSendConversationMessage() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      conversationId,
      content,
      clientMessageId,
    }: {
      conversationId: string;
      content: string;
      clientMessageId: string;
      authorId: string;
    }) => api.sendConversationMessage(conversationId, { content, client_message_id: clientMessageId }),
    onMutate: ({ conversationId, content, clientMessageId, authorId }) => {
      upsertConversationMessage(qc, wsId, {
        id: `optimistic:${clientMessageId}`,
        conversation_id: conversationId,
        seq: OPTIMISTIC_SEQ,
        author_type: "member",
        author_id: authorId,
        kind: "message",
        content,
        task_id: null,
        client_message_id: clientMessageId,
        created_at: new Date().toISOString(),
      });
    },
    onSuccess: (message) => {
      upsertConversationMessage(qc, wsId, message);
      markConversationReadInCache(qc, wsId, message.conversation_id, message.seq);
    },
    onError: (error, { conversationId, clientMessageId }) => {
      logger.error("sendConversationMessage.error", { error });
      removeConversationMessage(qc, wsId, conversationId, `optimistic:${clientMessageId}`);
    },
  });
}

export function newClientMessageId(): string {
  return createSafeId();
}

export function useMarkConversationRead() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ conversationId, seq }: { conversationId: string; seq: number }) =>
      api.markConversationRead(conversationId, seq),
    onMutate: ({ conversationId, seq }) => markConversationReadInCache(qc, wsId, conversationId, seq),
    onError: (error) => logger.error("markConversationRead.error", { error }),
  });
}
