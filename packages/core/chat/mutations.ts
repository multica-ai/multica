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
    mutationFn: (data: { agent_id: string; title?: string; selected_repo_urls?: string[] }) => {
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

export function useUpdateChatSessionRepos() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: ({ sessionId, selectedRepoUrls }: { sessionId: string; selectedRepoUrls: string[] }) => {
      logger.info("updateChatSessionRepos.start", { sessionId });
      return api.updateChatSessionRepos(sessionId, selectedRepoUrls);
    },
    onSuccess: (_data, { sessionId }) => {
      logger.info("updateChatSessionRepos.success", { sessionId });
    },
    onError: (err) => {
      logger.error("updateChatSessionRepos.error", err);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
      qc.invalidateQueries({ queryKey: chatKeys.allSessions(wsId) });
    },
  });
}

export function useArchiveChatSession() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (sessionId: string) => {
      logger.info("archiveChatSession.start", { sessionId });
      return api.archiveChatSession(sessionId);
    },
    onMutate: async (sessionId) => {
      await qc.cancelQueries({ queryKey: chatKeys.sessions(wsId) });
      await qc.cancelQueries({ queryKey: chatKeys.allSessions(wsId) });

      const prevSessions = qc.getQueryData<ChatSession[]>(chatKeys.sessions(wsId));
      const prevAll = qc.getQueryData<ChatSession[]>(chatKeys.allSessions(wsId));

      // Optimistic: remove from active, mark as archived in allSessions
      qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), (old) =>
        old ? old.filter((s) => s.id !== sessionId) : old,
      );
      qc.setQueryData<ChatSession[]>(chatKeys.allSessions(wsId), (old) =>
        old?.map((s) =>
          s.id === sessionId ? { ...s, status: "archived" as const } : s,
        ),
      );

      logger.debug("archiveChatSession.optimistic", { sessionId });
      return { prevSessions, prevAll };
    },
    onError: (err, sessionId, ctx) => {
      logger.error("archiveChatSession.error.rollback", { sessionId, err });
      if (ctx?.prevSessions) qc.setQueryData(chatKeys.sessions(wsId), ctx.prevSessions);
      if (ctx?.prevAll) qc.setQueryData(chatKeys.allSessions(wsId), ctx.prevAll);
    },
    onSettled: (_data, _err, sessionId) => {
      logger.debug("archiveChatSession.settled", { sessionId });
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
      qc.invalidateQueries({ queryKey: chatKeys.allSessions(wsId) });
    },
  });
}
