// CEREBRO-PATCH(chat-mutations-cerebro): JEH-799 chat-session header
// mutations. Lives in the cerebro zone alongside the header view; upstream
// `packages/core/chat/mutations.ts` is unmodified.
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { chatKeys } from "@multica/core/chat/queries";
import { useChatStore } from "@multica/core/chat";
import { useWorkspaceId } from "@multica/core/hooks";
import { createLogger } from "@multica/core/logger";
import type { ChatSession } from "@multica/core/types";

const logger = createLogger("cerebro-chat.mut");

/**
 * Updates a chat session's title or status. Optimistically rewrites the
 * cached lists so the header (and the cross-window dropdown trigger) reflects
 * the new title without waiting for the round-trip; rolls back on error.
 *
 * The matching `chat:session_updated` WS event keeps other tabs in sync —
 * see use-realtime-sync-cerebro.ts.
 */
export function useUpdateChatSession() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation({
    mutationFn: ({
      sessionId,
      title,
      status,
    }: {
      sessionId: string;
      title?: string;
      status?: "active" | "archived";
    }) => {
      logger.info("updateChatSession.start", { sessionId, title, status });
      return api.updateChatSession(sessionId, { title, status });
    },
    onMutate: async ({ sessionId, title, status }) => {
      await qc.cancelQueries({ queryKey: chatKeys.sessions(wsId) });
      await qc.cancelQueries({ queryKey: chatKeys.allSessions(wsId) });

      const prevSessions = qc.getQueryData<ChatSession[]>(chatKeys.sessions(wsId));
      const prevAll = qc.getQueryData<ChatSession[]>(chatKeys.allSessions(wsId));

      const apply = (s: ChatSession): ChatSession => ({
        ...s,
        ...(title !== undefined ? { title } : {}),
        ...(status !== undefined ? { status } : {}),
      });
      // When archiving, the session leaves the "active" list (which only
      // contains active sessions) but stays in the "all" list so the history
      // panel still surfaces it.
      const updateActive = (old?: ChatSession[]) => {
        if (!old) return old;
        const next = old.flatMap((s) => {
          if (s.id !== sessionId) return [s];
          if (status === "archived") return [];
          return [apply(s)];
        });
        return next;
      };
      const updateAll = (old?: ChatSession[]) =>
        old?.map((s) => (s.id === sessionId ? apply(s) : s));

      qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), updateActive);
      qc.setQueryData<ChatSession[]>(chatKeys.allSessions(wsId), updateAll);

      return { prevSessions, prevAll };
    },
    onError: (err, vars, ctx) => {
      logger.error("updateChatSession.error.rollback", { sessionId: vars.sessionId, err });
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
 * Converts a chat session into a tracked issue, returning the new issue's
 * identifier so the caller can navigate. The chat session is left intact —
 * the user can manually archive it afterwards if the conversation is now
 * superseded by the issue.
 */
export function useConvertChatSessionToIssue() {
  return useMutation({
    mutationFn: (sessionId: string) => {
      logger.info("convertChatSessionToIssue.start", { sessionId });
      return api.convertChatSessionToIssue(sessionId);
    },
    onSuccess: (resp) => {
      logger.info("convertChatSessionToIssue.success", {
        issueId: resp.issue_id,
        identifier: resp.identifier,
      });
    },
    onError: (err) => {
      logger.error("convertChatSessionToIssue.error", err);
    },
  });
}

/**
 * Archives the active chat session. Thin wrapper around useUpdateChatSession
 * that also drops the local `activeSessionId` pointer so the chat window
 * doesn't keep displaying a now read-only session.
 */
export function useArchiveActiveChatSession() {
  const update = useUpdateChatSession();
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  return {
    ...update,
    archive: (sessionId: string) => {
      update.mutate(
        { sessionId, status: "archived" },
        {
          onSuccess: () => {
            // Reset to "new chat" state — the input would be disabled on a
            // freshly-archived session anyway.
            setActiveSession(null);
          },
        },
      );
    },
  };
}

// TECH-3352 — per-user "remind me" (snooze) + "mark as unread" for chat-session
// inbox rows, bringing them to full parity with issue/channel/DM rows (which
// already have these via @multica/cerebro-channels + @multica/cerebro-inbox).
// Cerebro-only: chat sessions store the snooze directly on the row
// (chat_session.muted_until, per creator) and reuse the existing unread marker.
function patchSessionList(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  sessionId: string,
  patch: (s: ChatSession) => ChatSession,
) {
  qc.setQueryData<ChatSession[]>(chatKeys.sessions(wsId), (old) =>
    old?.map((s) => (s.id === sessionId ? patch(s) : s)),
  );
}

// Snooze ("remind me later"): hides the chat from the normal inbox views until
// `mutedUntil`, then it resurfaces on its own.
export function useMuteChat() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, mutedUntil }: { id: string; mutedUntil: Date }) =>
      api.muteChatSession(id, mutedUntil),
    onMutate: async ({ id, mutedUntil }) => {
      await qc.cancelQueries({ queryKey: chatKeys.sessions(wsId) });
      const prev = qc.getQueryData<ChatSession[]>(chatKeys.sessions(wsId));
      patchSessionList(qc, wsId, id, (s) => ({ ...s, muted_until: mutedUntil.toISOString() }));
      return { prev };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(chatKeys.sessions(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    },
  });
}

export function useUnmuteChat() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.unmuteChatSession(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: chatKeys.sessions(wsId) });
      const prev = qc.getQueryData<ChatSession[]>(chatKeys.sessions(wsId));
      patchSessionList(qc, wsId, id, (s) => ({ ...s, muted_until: null }));
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(chatKeys.sessions(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    },
  });
}

// Re-flag a read chat as unread (bold row + stripe). The server sets
// unread_since; the chat list reports has_unread=true for it.
export function useMarkChatUnread() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.markChatSessionUnread(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: chatKeys.sessions(wsId) });
      const prev = qc.getQueryData<ChatSession[]>(chatKeys.sessions(wsId));
      patchSessionList(qc, wsId, id, (s) => ({ ...s, has_unread: true }));
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) qc.setQueryData(chatKeys.sessions(wsId), ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
    },
  });
}
