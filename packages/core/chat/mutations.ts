import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { chatKeys } from "./queries";
import { createLogger } from "../logger";
import type { ChatSession } from "../types";

const logger = createLogger("chat.mut");

export function useCreateChatSession() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (data: { agent_id: string; title?: string }) => {
      logger.info("createChatSession.start", { agent_id: data.agent_id, titleLength: data.title?.length ?? 0 });
      return api.createChatSession(data);
    },
    onSuccess: (session) => {
      logger.info("createChatSession.success", { sessionId: session.id, agentId: session.agent_id });
    },
    onError: (err) => {
      logger.error("createChatSession.error", err);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
      qc.invalidateQueries({ queryKey: chatKeys.allSessions(wsId) });
    },
  });
}

/**
 * Clears the session's unread state server-side. Optimistically flips
 * has_unread to false in the cached lists so the FAB badge drops
 * immediately. The server broadcasts chat:session_read so other devices
 * also sync.
 */
export function useMarkChatSessionRead() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (sessionId: string) => {
      logger.info("markChatSessionRead.start", { sessionId });
      return api.markChatSessionRead(sessionId);
    },
    onMutate: async (sessionId) => {
      await qc.cancelQueries({ queryKey: chatKeys.sessions(wsId) });
      await qc.cancelQueries({ queryKey: chatKeys.allSessions(wsId) });

      const prevSessions = qc.getQueryData<ChatSession[]>(chatKeys.sessions(wsId));
      const prevAll = qc.getQueryData<ChatSession[]>(chatKeys.allSessions(wsId));

      const clear = (old?: ChatSession[]) =>
        old?.map((s) => (s.id === sessionId ? { ...s, has_unread: false } : s));
      qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), clear);
      qc.setQueryData<ChatSession[]>(chatKeys.allSessions(wsId), clear);

      return { prevSessions, prevAll };
    },
    onError: (err, sessionId, ctx) => {
      logger.error("markChatSessionRead.error.rollback", { sessionId, err });
      if (ctx?.prevSessions) qc.setQueryData(chatKeys.sessions(wsId), ctx.prevSessions);
      if (ctx?.prevAll) qc.setQueryData(chatKeys.allSessions(wsId), ctx.prevAll);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
      qc.invalidateQueries({ queryKey: chatKeys.allSessions(wsId) });
    },
  });
}

/**
 * Hard-deletes a chat session. Optimistically removes the row from both
 * the active and all-sessions lists so the history panel updates instantly;
 * rolls back on error. The matching `chat:session_deleted` WS event keeps
 * other tabs/devices in sync — see use-realtime-sync.ts.
 */
export function useDeleteChatSession() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (sessionId: string) => {
      logger.info("deleteChatSession.start", { sessionId });
      return api.deleteChatSession(sessionId);
    },
    onMutate: async (sessionId) => {
      await qc.cancelQueries({ queryKey: chatKeys.sessions(wsId) });
      await qc.cancelQueries({ queryKey: chatKeys.allSessions(wsId) });

      const prevSessions = qc.getQueryData<ChatSession[]>(chatKeys.sessions(wsId));
      const prevAll = qc.getQueryData<ChatSession[]>(chatKeys.allSessions(wsId));

      const drop = (old?: ChatSession[]) => old?.filter((s) => s.id !== sessionId);
      qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), drop);
      qc.setQueryData<ChatSession[]>(chatKeys.allSessions(wsId), drop);

      logger.debug("deleteChatSession.optimistic", { sessionId });
      return { prevSessions, prevAll };
    },
    onError: (err, sessionId, ctx) => {
      logger.error("deleteChatSession.error.rollback", { sessionId, err });
      if (ctx?.prevSessions) qc.setQueryData(chatKeys.sessions(wsId), ctx.prevSessions);
      if (ctx?.prevAll) qc.setQueryData(chatKeys.allSessions(wsId), ctx.prevAll);
    },
    onSettled: (_data, _err, sessionId) => {
      logger.debug("deleteChatSession.settled", { sessionId });
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
      qc.invalidateQueries({ queryKey: chatKeys.allSessions(wsId) });
    },
  });
}
