import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import { chatKeys, sortChatSessions } from "./queries";
import { createLogger } from "../logger";
import type { ChatSession, ChatPinnedAgent, ChatDraftRestoresResponse } from "../types";

const logger = createLogger("chat.mut");

/**
 * Consume a deferred-cancellation draft restore (#5219) after the composer has
 * applied it. The endpoint is idempotent, so consuming twice — or consuming a
 * row a previous attempt already deleted — is safe, which is what lets this
 * retry at all (mutations are `retry: false` app-wide).
 *
 * A lost consume must never re-restore a prompt the user has since sent, so the
 * "applied" decision is not carried by this request: the caller records it in
 * the persisted ledger (`markDraftRestoreApplied`) before firing, and reconciles
 * any row that outlives its ledger entry by consuming it again. This mutation
 * only has to make that reconciliation converge quickly.
 */
export function useConsumeChatDraftRestore() {
  const qc = useQueryClient();

  return useMutation({
    mutationFn: ({ sessionId, restoreId }: { sessionId: string; restoreId: string }) =>
      api.consumeChatDraftRestore(sessionId, restoreId),
    retry: 3,
    retryDelay: (attempt) => Math.min(1000 * 2 ** attempt, 8000),
    onMutate: ({ sessionId, restoreId }) => {
      qc.setQueryData<ChatDraftRestoresResponse>(
        chatKeys.draftRestores(sessionId),
        (old) =>
          old
            ? { ...old, restores: old.restores.filter((r) => r.id !== restoreId) }
            : old,
      );
    },
    onError: (err, { sessionId, restoreId }) => {
      // Exhausted the retries. The draft is safe (it is in the composer) and the
      // ledger keeps the row from being re-offered; the next mount reconciles it.
      logger.warn("consumeChatDraftRestore.error", { sessionId, restoreId, err });
    },
  });
}

/** Pin an agent to the quick-agent bar (optimistic append). */
export function usePinChatAgent() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (agentId: string) => api.pinChatAgent(agentId),
    onMutate: async (agentId) => {
      await qc.cancelQueries({ queryKey: chatKeys.pinnedAgents(wsId) });
      const prev = qc.getQueryData<ChatPinnedAgent[]>(chatKeys.pinnedAgents(wsId));
      qc.setQueryData<ChatPinnedAgent[]>(chatKeys.pinnedAgents(wsId), (old) => {
        if (old?.some((p) => p.agent_id === agentId)) return old;
        const maxPos = old?.reduce((m, p) => Math.max(m, p.position), 0) ?? 0;
        return [...(old ?? []), { agent_id: agentId, position: maxPos + 1 }];
      });
      return { prev };
    },
    onError: (err, agentId, ctx) => {
      logger.error("pinChatAgent.error.rollback", { agentId, err });
      if (ctx?.prev) qc.setQueryData(chatKeys.pinnedAgents(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: chatKeys.pinnedAgents(wsId) });
    },
  });
}

/** Unpin an agent from the quick-agent bar (optimistic removal). */
export function useUnpinChatAgent() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (agentId: string) => api.unpinChatAgent(agentId),
    onMutate: async (agentId) => {
      await qc.cancelQueries({ queryKey: chatKeys.pinnedAgents(wsId) });
      const prev = qc.getQueryData<ChatPinnedAgent[]>(chatKeys.pinnedAgents(wsId));
      qc.setQueryData<ChatPinnedAgent[]>(chatKeys.pinnedAgents(wsId), (old) =>
        old?.filter((p) => p.agent_id !== agentId),
      );
      return { prev };
    },
    onError: (err, agentId, ctx) => {
      logger.error("unpinChatAgent.error.rollback", { agentId, err });
      if (ctx?.prev) qc.setQueryData(chatKeys.pinnedAgents(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: chatKeys.pinnedAgents(wsId) });
    },
  });
}

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
    },
  });
}

/**
 * Clears the session's unread state server-side. Optimistically flips
 * has_unread to false in the cached list so the FAB badge drops
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

      const prevSessions = qc.getQueryData<ChatSession[]>(chatKeys.sessions(wsId));

      const clear = (old?: ChatSession[]) =>
        old?.map((s) => (s.id === sessionId ? { ...s, has_unread: false, unread_count: 0 } : s));
      qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), clear);

      return { prevSessions };
    },
    onError: (err, sessionId, ctx) => {
      logger.error("markChatSessionRead.error.rollback", { sessionId, err });
      if (ctx?.prevSessions) qc.setQueryData(chatKeys.sessions(wsId), ctx.prevSessions);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    },
  });
}

/**
 * Renames a chat session. Optimistically swaps the title in the cached
 * list so the dropdown reflects the new label immediately; rolls back on
 * error. The matching `chat:session_updated` WS event keeps other
 * tabs/devices in sync — see use-realtime-sync.ts.
 */
export function useUpdateChatSession() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (data: { sessionId: string; title: string }) => {
      logger.info("updateChatSession.start", {
        sessionId: data.sessionId,
        titleLength: data.title.length,
      });
      return api.updateChatSession(data.sessionId, { title: data.title });
    },
    onMutate: async ({ sessionId, title }) => {
      await qc.cancelQueries({ queryKey: chatKeys.sessions(wsId) });

      const prevSessions = qc.getQueryData<ChatSession[]>(chatKeys.sessions(wsId));

      const patch = (old?: ChatSession[]) =>
        old?.map((s) => (s.id === sessionId ? { ...s, title } : s));
      qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), patch);

      return { prevSessions };
    },
    onError: (err, vars, ctx) => {
      logger.error("updateChatSession.error.rollback", { sessionId: vars.sessionId, err });
      if (ctx?.prevSessions) qc.setQueryData(chatKeys.sessions(wsId), ctx.prevSessions);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    },
  });
}

/**
 * Pins or unpins a chat. Optimistically flips `pinned` and re-sorts the cached
 * list (pinned first, then by activity) so the row jumps to / from the top
 * instantly; rolls back on error. The matching `chat:session_updated` WS event
 * carries the new pin state to other tabs/devices — see use-realtime-sync.ts.
 */
export function useSetChatSessionPinned() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (data: { sessionId: string; pinned: boolean }) => {
      logger.info("setChatSessionPinned.start", data);
      return api.setChatSessionPinned(data.sessionId, data.pinned);
    },
    onMutate: async ({ sessionId, pinned }) => {
      await qc.cancelQueries({ queryKey: chatKeys.sessions(wsId) });

      const prevSessions = qc.getQueryData<ChatSession[]>(chatKeys.sessions(wsId));

      const patch = (old?: ChatSession[]) =>
        old &&
        sortChatSessions(old.map((s) => (s.id === sessionId ? { ...s, pinned } : s)));
      qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), patch);

      return { prevSessions };
    },
    onError: (err, vars, ctx) => {
      logger.error("setChatSessionPinned.error.rollback", { sessionId: vars.sessionId, err });
      if (ctx?.prevSessions) qc.setQueryData(chatKeys.sessions(wsId), ctx.prevSessions);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    },
  });
}

/**
 * Archives or unarchives a chat session. Optimistically flips `status` in the
 * cached list so the row moves between the active list and the "Archived" view
 * instantly (both filter on status locally); rolls back on error. Bumps
 * `updated_at` so the row re-sorts by activity in whichever view it lands.
 * The matching `chat:session_updated` WS event carries the new status to other
 * tabs/devices — see use-realtime-sync.ts.
 *
 * Archiving also zeroes the row's unread locally so every badge (FAB, sidebar
 * Chat tab, chat-window header) drops it in the same frame the row moves to the
 * Archived view. The backend already forces unread to 0 for archived rows (see
 * ListAllChatSessionsByCreator / MUL-4360); this is the optimistic half so there
 * is no window where the header/sidebar still count a just-archived session
 * before the refetch lands. Unarchive does NOT restore a count here — the true
 * unread state comes back from the server refetch (last_read_at is untouched).
 */
export function useSetChatSessionArchived() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: (data: { sessionId: string; archived: boolean }) => {
      logger.info("setChatSessionArchived.start", data);
      return api.setChatSessionArchived(data.sessionId, data.archived);
    },
    onMutate: async ({ sessionId, archived }) => {
      await qc.cancelQueries({ queryKey: chatKeys.sessions(wsId) });

      const prevSessions = qc.getQueryData<ChatSession[]>(chatKeys.sessions(wsId));

      const nowIso = new Date().toISOString();
      const patch = (old?: ChatSession[]) =>
        old &&
        sortChatSessions(
          old.map((s) =>
            s.id === sessionId
              ? {
                  ...s,
                  status: archived ? "archived" : "active",
                  updated_at: nowIso,
                  ...(archived ? { unread_count: 0, has_unread: false } : {}),
                }
              : s,
          ),
        );
      qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), patch);

      return { prevSessions };
    },
    onError: (err, vars, ctx) => {
      logger.error("setChatSessionArchived.error.rollback", { sessionId: vars.sessionId, err });
      if (ctx?.prevSessions) qc.setQueryData(chatKeys.sessions(wsId), ctx.prevSessions);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    },
  });
}

/**
 * Hard-deletes a chat session. Optimistically removes the row from the
 * sessions list so the dropdown updates instantly; rolls back on error.
 * The matching `chat:session_deleted` WS event keeps other tabs/devices
 * in sync — see use-realtime-sync.ts.
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

      const prevSessions = qc.getQueryData<ChatSession[]>(chatKeys.sessions(wsId));

      const drop = (old?: ChatSession[]) => old?.filter((s) => s.id !== sessionId);
      qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), drop);

      logger.debug("deleteChatSession.optimistic", { sessionId });
      return { prevSessions };
    },
    onError: (err, sessionId, ctx) => {
      logger.error("deleteChatSession.error.rollback", { sessionId, err });
      if (ctx?.prevSessions) qc.setQueryData(chatKeys.sessions(wsId), ctx.prevSessions);
    },
    onSettled: (_data, _err, sessionId) => {
      logger.debug("deleteChatSession.settled", { sessionId });
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    },
  });
}
