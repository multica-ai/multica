"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { canAssignAgent } from "@multica/views/issues/components";
import { api } from "@multica/core/api";
import { useAgentPresenceDetail, useWorkspaceAgentAvailability } from "@multica/core/agents";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import {
  chatSessionsOptions,
  chatMessagesPageOptions,
  pendingChatTaskOptions,
  chatKeys,
  isTaskMessageTaskId,
  sortChatSessions,
} from "@multica/core/chat/queries";
import {
  useCreateChatSession,
  useMarkChatSessionRead,
  useSetChatSessionArchived,
} from "@multica/core/chat/mutations";
import { useChatStore } from "@multica/core/chat";
import { createLogger } from "@multica/core/logger";
import type {
  Agent,
  Attachment,
  ChatMessage,
  ChatMessagesPage,
  ChatPendingTask,
} from "@multica/core/types";
import { useT } from "../../i18n";

const uiLogger = createLogger("chat.ui");
const apiLogger = createLogger("chat.api");

// Derive a concise session title from the first user message: first line,
// markdown stripped, whitespace collapsed, capped. A deterministic title
// (no LLM) — the server has no summarization model, so this is the sensible
// default until a runtime-generated title is wired up.
const CHAT_TITLE_MAX = 30;
export function deriveChatTitle(content: string): string {
  const firstLine = (content.split("\n").find((l) => l.trim()) ?? content).trim();
  const cleaned = firstLine
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/[#*`>~_]/g, "")
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, "$1") // markdown links/images → their text
    .replace(/\s+/g, " ")
    .trim();
  if (cleaned.length <= CHAT_TITLE_MAX) return cleaned;
  return cleaned.slice(0, CHAT_TITLE_MAX - 1).trimEnd() + "…";
}

// True when a session has an in-flight optimistic write — an `optimistic-`
// message or a pending task in the cache. That is the signal of a just-created
// (or actively-sending) session still awaiting server confirmation, before the
// sessions-list refetch includes it. Deliberately NOT "has cached messages": a
// session deleted elsewhere can still have real cached history, which must not
// exempt it from the stale-session self-heal.
export function hasOptimisticInFlight(
  qc: ReturnType<typeof useQueryClient>,
  sessionId: string,
): boolean {
  const pending = qc.getQueryData<ChatPendingTask>(chatKeys.pendingTask(sessionId));
  if (pending?.task_id) return true;
  const flat = qc.getQueryData<ChatMessage[]>(chatKeys.messages(sessionId));
  if (flat?.some((m) => m.id.startsWith("optimistic-"))) return true;
  const paged = qc.getQueryData<InfiniteData<ChatMessagesPage>>(
    chatKeys.messagesPage(sessionId),
  );
  return Boolean(
    paged?.pages.some((page) =>
      page.messages.some((m) => m.id.startsWith("optimistic-")),
    ),
  );
}
const CHAT_VIRTUOSO_INITIAL_FIRST_ITEM_INDEX = 1_000_000;

function appendChatMessageToLatestPageCache(
  qc: ReturnType<typeof useQueryClient>,
  sessionId: string,
  message: ChatMessage,
) {
  qc.setQueryData<InfiniteData<ChatMessagesPage>>(
    chatKeys.messagesPage(sessionId),
    (old) => {
      if (!old) {
        return {
          pages: [{
            messages: [message],
            limit: 50,
            has_more: false,
            next_cursor: null,
          }],
          pageParams: [null],
        };
      }
      if (old.pages.some((page) => page.messages.some((m) => m.id === message.id))) {
        return old;
      }
      return {
        ...old,
        pages: old.pages.map((page, index) =>
          index === 0 ? { ...page, messages: [...page.messages, message] } : page,
        ),
      };
    },
  );
}

function removeChatMessageFromPageCache(
  qc: ReturnType<typeof useQueryClient>,
  sessionId: string,
  messageId: string,
) {
  qc.setQueryData<InfiniteData<ChatMessagesPage> | undefined>(
    chatKeys.messagesPage(sessionId),
    (old) => {
      if (!old) return old;
      return {
        ...old,
        pages: old.pages.map((page) => ({
          ...page,
          messages: page.messages.filter((m) => m.id !== messageId),
        })),
      };
    },
  );
}

export function removeChatMessageFromCaches(
  qc: ReturnType<typeof useQueryClient>,
  sessionId: string,
  messageId: string,
) {
  qc.setQueryData<ChatMessage[]>(
    chatKeys.messages(sessionId),
    (old) => old?.filter((m) => m.id !== messageId) ?? old,
  );
  removeChatMessageFromPageCache(qc, sessionId, messageId);
}

function replaceOptimisticChatMessageId(
  qc: ReturnType<typeof useQueryClient>,
  sessionId: string,
  optimisticId: string,
  messageId: string,
  taskId: string,
) {
  const replace = (messages: ChatMessage[] | undefined) => {
    if (!messages) return messages;
    if (messages.some((m) => m.id === messageId)) {
      return messages.filter((m) => m.id !== optimisticId);
    }
    return messages.map((m) =>
      m.id === optimisticId ? { ...m, id: messageId, task_id: taskId } : m,
    );
  };

  qc.setQueryData<ChatMessage[]>(chatKeys.messages(sessionId), replace);
  qc.setQueryData<InfiniteData<ChatMessagesPage> | undefined>(
    chatKeys.messagesPage(sessionId),
    (old) => {
      if (!old) return old;
      return {
        ...old,
        pages: old.pages.map((page) => ({
          ...page,
          messages: replace(page.messages) ?? page.messages,
        })),
      };
    },
  );
}

/**
 * Layout-agnostic chat controller. Holds every piece of chat conversation
 * state and behavior — agent resolution, session lookup, the optimistic
 * send/stop/cancel burst, message pagination, and auto-mark-read — so that
 * both surfaces render the same conversation logic:
 *
 *  - ChatWindow: the floating FAB overlay (adds resize / expand / minimize).
 *  - ChatPage:   the first-class Chat tab (two-pane thread list + conversation).
 *
 * The only thing the caller supplies is `isActive` — whether its surface is
 * currently on screen — which gates auto-mark-read so a background overlay
 * doesn't silently clear unread state the user hasn't actually seen.
 */
export function useChatController(opts?: { isActive?: boolean }) {
  const isActive = opts?.isActive ?? true;
  const { t } = useT("chat");
  const wsId = useWorkspaceId();
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const selectedAgentId = useChatStore((s) => s.selectedAgentId);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const setSelectedAgentId = useChatStore((s) => s.setSelectedAgentId);
  const user = useAuthStore((s) => s.user);
  const { data: agents = [], isSuccess: agentsLoaded } = useQuery(
    agentListOptions(wsId),
  );
  const { data: members = [], isSuccess: membersLoaded } = useQuery(
    memberListOptions(wsId),
  );
  const { data: sessions = [], isSuccess: sessionsLoaded } = useQuery(
    chatSessionsOptions(wsId),
  );
  const {
    data: rawMessagePages,
    isLoading: messagesLoading,
    fetchNextPage: fetchOlderMessages,
    hasNextPage: hasOlderMessages,
    isFetchingNextPage: isFetchingOlderMessages,
  } = useInfiniteQuery(chatMessagesPageOptions(activeSessionId ?? ""));

  const messagePages = activeSessionId ? rawMessagePages?.pages ?? [] : [];
  const messages = [...messagePages].reverse().flatMap((page) => page.messages);
  const olderMessageCount = messagePages
    .slice(1)
    .reduce((sum, page) => sum + page.messages.length, 0);
  const firstItemIndex =
    messages.length > 0
      ? CHAT_VIRTUOSO_INITIAL_FIRST_ITEM_INDEX - olderMessageCount
      : 0;
  const showSkeleton = !!activeSessionId && messagesLoading;

  const { data: pendingTask } = useQuery(
    pendingChatTaskOptions(activeSessionId ?? ""),
  );
  const pendingTaskId = pendingTask?.task_id ?? null;
  const stopRequestedBeforeTaskRef = useRef(false);
  const [restoreDraftRequest, setRestoreDraftRequest] = useState<{
    id: string;
    content: string;
    attachments?: Attachment[];
    sessionId?: string;
  } | null>(null);
  const handleRestoreDraftConsumed = useCallback(() => {
    setRestoreDraftRequest(null);
  }, []);
  // Nonce handed to ChatInput to pull focus into the compose box when a new
  // chat starts. Bumped by handleNewChat / handleStartNewChat only, so
  // selecting an existing chat or a deep link never steals focus.
  const [focusInputRequest, setFocusInputRequest] = useState(0);
  const requestInputFocus = useCallback(
    () => setFocusInputRequest((n) => n + 1),
    [],
  );

  const currentSession = activeSessionId
    ? sessions.find((s) => s.id === activeSessionId)
    : null;
  const isSessionArchived = currentSession?.status === "archived";

  const qc = useQueryClient();
  const createSession = useCreateChatSession();
  const markRead = useMarkChatSessionRead();
  const setArchived = useSetChatSessionArchived();

  const currentMember = members.find((m) => m.user_id === user?.id);
  const memberRole = currentMember?.role;
  const availableAgents = agents.filter(
    (a) => !a.archived_at && canAssignAgent(a, user?.id, memberRole),
  );
  // `availableAgents` is only trustworthy once BOTH queries above succeeded:
  // the permission filter reads the member role, so agents-without-members
  // misreports a public_to agent as unavailable. Consumers that must tell
  // "still loading" apart from "settled and not available" (the `?agent=`
  // deep link) gate on this instead of sniffing list emptiness. Query errors
  // deliberately keep this false — a failed fetch is not a permission verdict.
  const agentsSettled = agentsLoaded && membersLoaded;

  // The agent bound to the OPEN session, resolved from the full agent list
  // (archived included, since agentListOptions passes include_archived). An
  // archived agent is filtered out of `availableAgents`, so resolving the
  // active agent only from that list would make an archived-agent session
  // silently render some *other* available agent — wrong avatar/name/presence
  // in the header, and a send that targets the wrong agent. Binding to the
  // session's real agent keeps the conversation honest; the archived state
  // then makes it read-only (see isAgentArchived).
  const sessionAgent = currentSession
    ? agents.find((a) => a.id === currentSession.agent_id) ?? null
    : null;
  const isAgentArchived = !!sessionAgent?.archived_at;

  // Resolve selected agent: open session's agent → stored preference → first
  // available. New chats have no session, so they fall through to the picker.
  const activeAgent =
    sessionAgent ??
    availableAgents.find((a) => a.id === selectedAgentId) ??
    availableAgents[0] ??
    null;

  const agentAvailability = useWorkspaceAgentAvailability();
  const noAgent = agentAvailability === "none";

  const presenceDetail = useAgentPresenceDetail(wsId, activeAgent?.id);
  const availability =
    presenceDetail === "loading" ? undefined : presenceDetail.availability;

  // Auto mark-as-read whenever the user is actively looking at a session with
  // unread state. `isActive` lets the caller say "my surface is on screen":
  // the floating overlay passes `isOpen`, the tab passes `true`.
  //
  // The read is deferred by a tick and cancelled on cleanup, so a session that
  // is only *momentarily* active never gets marked read. This is the fix for
  // MUL-4360's mount race: `activeSessionId` is persisted, so on a bare `/chat`
  // navigation the page restores the last session for one frame before its
  // URL→store effect (which runs AFTER this hook's effects, since the hook is
  // called first) clears it back to null. Without the defer, that restored-but-
  // never-opened session was marked read in that gap — its badge vanished
  // though the user never opened it (right pane still shows "select a chat").
  // Deferring lets the subsequent activeSessionId change cancel the pending
  // read via cleanup; the store re-check is a belt-and-suspenders guard. Only a
  // session that stays active past the tick — a real select, deep link, or
  // refresh — is read.
  const currentHasUnread =
    sessions.find((s) => s.id === activeSessionId)?.has_unread ?? false;
  useEffect(() => {
    if (!isActive || !activeSessionId) return;
    if (!currentHasUnread) return;
    const sessionId = activeSessionId;
    const timer = setTimeout(() => {
      if (useChatStore.getState().activeSessionId !== sessionId) return;
      uiLogger.info("auto markRead", { sessionId });
      markRead.mutate(sessionId);
    }, 0);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- markRead ref stable
  }, [isActive, activeSessionId, currentHasUnread]);

  const { uploadWithToast } = useFileUpload(api);

  const sessionPromiseRef = useRef<Promise<string | null> | null>(null);
  const ensureSession = useCallback(
    async (titleSeed: string): Promise<string | null> => {
      // Trust the current session id only when it's real: present in the
      // loaded list, or a just-created one still awaiting the list refetch
      // (has an optimistic write). A dangling id (deleted / no access) must not
      // be treated as an existing session — fall through and create a fresh one
      // so the message lands somewhere instead of POSTing into a 404.
      if (
        activeSessionId &&
        (!sessionsLoaded ||
          sessions.some((s) => s.id === activeSessionId) ||
          hasOptimisticInFlight(qc, activeSessionId))
      ) {
        return activeSessionId;
      }
      if (!activeAgent) return null;
      if (sessionPromiseRef.current) return sessionPromiseRef.current;

      const promise = (async () => {
        try {
          const session = await createSession.mutateAsync({
            agent_id: activeAgent.id,
            title: deriveChatTitle(titleSeed),
          });
          return session.id;
        } finally {
          sessionPromiseRef.current = null;
        }
      })();
      sessionPromiseRef.current = promise;
      return promise;
    },
    [activeSessionId, activeAgent, createSession, sessions, sessionsLoaded, qc],
  );

  // Self-heal a dangling `activeSessionId`. Once the sessions list has loaded
  // and it isn't in the list — with no in-flight optimistic write exempting a
  // just-created session — the id was deleted, lost access, or never existed
  // (a stale `?session=` deep link, or a persisted floating-window selection).
  // Clearing it stops BOTH surfaces (the tab and the floating window) from
  // rendering an editable empty chat whose send would POST into a nonexistent
  // session. Lives in the shared controller so every surface self-heals.
  useEffect(() => {
    if (!activeSessionId || !sessionsLoaded) return;
    if (sessions.some((s) => s.id === activeSessionId)) return;
    if (hasOptimisticInFlight(qc, activeSessionId)) return;
    uiLogger.info("clearing dangling activeSessionId", { sessionId: activeSessionId });
    setActiveSession(null);
  }, [activeSessionId, sessionsLoaded, sessions, qc, setActiveSession]);

  const handleUploadFile = useCallback(
    async (file: File) => {
      if (!activeAgent) return null;
      return uploadWithToast(file);
    },
    [activeAgent, uploadWithToast],
  );

  const cancelChatTask = useCallback(
    async (
      taskId: string,
      sessionId: string,
      options: { restoreDraftToInput: boolean; source: string },
    ) => {
      apiLogger.info("cancelTask.start", {
        taskId,
        sessionId,
        source: options.source,
      });
      qc.setQueryData(chatKeys.pendingTask(sessionId), {});

      try {
        const result = await api.cancelTaskById(taskId);
        const restored = result.cancelled_chat_message;
        if (restored?.restore_to_input) {
          removeChatMessageFromCaches(qc, restored.chat_session_id, restored.message_id);
          if (options.restoreDraftToInput && restored.chat_session_id === sessionId) {
            setRestoreDraftRequest({
              id: restored.message_id,
              content: restored.content,
              attachments: restored.attachments,
              sessionId: restored.chat_session_id,
            });
          }
        }
        qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
        qc.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) });
        apiLogger.info("cancelTask.success", {
          taskId,
          sessionId,
          restoredToInput: !!restored?.restore_to_input && options.restoreDraftToInput,
        });
        return result;
      } catch (err) {
        apiLogger.warn("cancelTask.error (task may have already finished)", {
          taskId,
          sessionId,
          err,
        });
        qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
        qc.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) });
        return null;
      }
    },
    [qc],
  );

  const handleSend = useCallback(
    async (
      content: string,
      attachmentIds?: string[],
      commitInput?: (options?: { extraDraftKeys?: string[]; clearEditor?: boolean }) => void,
      draftAttachments: Attachment[] = [],
    ): Promise<boolean> => {
      if (!activeAgent) {
        apiLogger.warn("sendChatMessage skipped: no active agent");
        return false;
      }
      // Read-only conversation: the agent is retired and can no longer pick up
      // work, so refuse to enqueue a task that would sit orphaned forever. The
      // input is disabled in this state; this is the belt-and-braces guard.
      if (isAgentArchived) {
        apiLogger.warn("sendChatMessage skipped: agent is archived", {
          sessionId: activeSessionId,
          agentId: activeAgent.id,
        });
        return false;
      }

      const finalContent = content;
      const isNewSession = !activeSessionId;

      apiLogger.info("sendChatMessage.start", {
        sessionId: activeSessionId,
        isNewSession,
        agentId: activeAgent.id,
        contentLength: finalContent.length,
        attachmentCount: attachmentIds?.length ?? 0,
      });

      let sessionId: string | null = null;
      try {
        sessionId = await ensureSession(finalContent);
      } catch (err) {
        apiLogger.error("sendChatMessage.ensureSession.error", err);
        toast.error(t(($) => $.input.send_failed_toast));
        return false;
      }
      if (!sessionId) {
        apiLogger.warn("sendChatMessage aborted: ensureSession returned null");
        return false;
      }

      const sentAt = new Date().toISOString();
      const optimistic: ChatMessage = {
        id: `optimistic-${Date.now()}`,
        chat_session_id: sessionId,
        role: "user",
        content: finalContent,
        task_id: null,
        created_at: sentAt,
        attachments: draftAttachments,
      };
      appendChatMessageToLatestPageCache(qc, sessionId, optimistic);
      qc.setQueryData<ChatMessage[]>(
        chatKeys.messages(sessionId),
        (old) => (old ? [...old, optimistic] : [optimistic]),
      );
      qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sessionId), {
        task_id: `optimistic-${optimistic.id}`,
        status: "queued",
        created_at: sentAt,
      });
      const live = useChatStore.getState();
      const stillOnSourceSession =
        live.activeSessionId === activeSessionId &&
        (activeSessionId !== null || live.selectedAgentId === selectedAgentId);
      if (stillOnSourceSession) {
        setActiveSession(sessionId);
      }
      commitInput?.({ extraDraftKeys: [sessionId], clearEditor: stillOnSourceSession });
      apiLogger.debug("sendChatMessage.optimistic", { sessionId, optimisticId: optimistic.id });

      let result;
      try {
        result = await api.sendChatMessage(sessionId, finalContent, attachmentIds);
      } catch (err) {
        apiLogger.error("sendChatMessage.error.rollback", { sessionId, optimisticId: optimistic.id, err });
        stopRequestedBeforeTaskRef.current = false;
        removeChatMessageFromCaches(qc, sessionId, optimistic.id);
        qc.setQueryData(chatKeys.pendingTask(sessionId), {});
        setRestoreDraftRequest({
          id: `send-failed-${optimistic.id}`,
          content: finalContent,
          attachments: draftAttachments,
          sessionId,
        });
        toast.error(t(($) => $.input.send_failed_toast));
        return false;
      }
      apiLogger.info("sendChatMessage.success", {
        sessionId,
        messageId: result.message_id,
        taskId: result.task_id,
      });
      replaceOptimisticChatMessageId(qc, sessionId, optimistic.id, result.message_id, result.task_id);
      qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sessionId), {
        task_id: result.task_id,
        status: "queued",
        created_at: result.created_at,
      });
      if (stopRequestedBeforeTaskRef.current) {
        stopRequestedBeforeTaskRef.current = false;
        await cancelChatTask(result.task_id, sessionId, {
          restoreDraftToInput: true,
          source: "deferred-send",
        });
        return false;
      }
      if (attachmentIds && attachmentIds.length > 0 && result.attachment_ids) {
        const boundIds = new Set(result.attachment_ids);
        const missing = attachmentIds.filter((id) => !boundIds.has(id));
        if (missing.length > 0) {
          apiLogger.warn("sendChatMessage.attachments missing after send", {
            sessionId,
            messageId: result.message_id,
            missing,
          });
          toast.error(t(($) => $.input.attachment_bind_failed_toast));
        }
      }
      qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
      qc.invalidateQueries({ queryKey: chatKeys.messagesPage(sessionId) });
      return true;
    },
    [
      activeSessionId,
      selectedAgentId,
      activeAgent,
      isAgentArchived,
      ensureSession,
      cancelChatTask,
      qc,
      setActiveSession,
      t,
    ],
  );

  const handleStop = useCallback(() => {
    if (!pendingTaskId || !activeSessionId) {
      apiLogger.debug("cancelTask skipped: no pending task");
      return;
    }
    if (!isTaskMessageTaskId(pendingTaskId)) {
      stopRequestedBeforeTaskRef.current = true;
      apiLogger.info("cancelTask.deferred until server task id", {
        taskId: pendingTaskId,
        sessionId: activeSessionId,
      });
      return;
    }
    void cancelChatTask(pendingTaskId, activeSessionId, {
      restoreDraftToInput: true,
      source: "active-input",
    });
  }, [pendingTaskId, activeSessionId, cancelChatTask]);

  const handleNewChat = useCallback(() => {
    uiLogger.info("newChat", {
      previousSessionId: activeSessionId,
      previousPendingTask: pendingTaskId,
    });
    setActiveSession(null);
    requestInputFocus();
  }, [activeSessionId, pendingTaskId, setActiveSession, requestInputFocus]);

  // Start a fresh chat bound to a chosen agent. Unlike handleSelectAgent this
  // does not no-op when the agent is unchanged — "new chat" always clears the
  // active session so the user lands on an empty compose for that agent. The
  // session row is created lazily on the first send (see ensureSession).
  const handleStartNewChat = useCallback(
    (agent: Agent) => {
      uiLogger.info("startNewChat", {
        agentId: agent.id,
        previousSessionId: activeSessionId,
      });
      setSelectedAgentId(agent.id);
      setActiveSession(null);
      requestInputFocus();
    },
    [activeSessionId, setSelectedAgentId, setActiveSession, requestInputFocus],
  );

  const handleSelectSession = useCallback(
    (session: { id: string; agent_id: string }) => {
      // Sessions are bound 1:1 to an agent — picking a session from a
      // different agent implicitly switches the agent too.
      if (activeAgent && session.agent_id !== activeAgent.id) {
        uiLogger.info("selectSession (cross-agent)", {
          from: activeAgent.id,
          toAgent: session.agent_id,
          toSession: session.id,
        });
        setSelectedAgentId(session.agent_id);
      }
      setActiveSession(session.id);
    },
    [activeAgent, setSelectedAgentId, setActiveSession],
  );

  // Archiving the chat currently in view would otherwise strand the
  // conversation pane on a now read-only, "dangling" session. Mirror the Inbox
  // list: advance selection to the next chat in the (sorted, non-archived)
  // history, fall back to the previous one, and clear only when nothing is
  // left. Routing the non-null advance through handleSelectSession keeps
  // selectedAgentId in sync, so a follow-up "new chat" still defaults to the
  // right agent even when the next chat belongs to a different agent. A no-op
  // when the archived session isn't the open one — that selection stays put.
  const advanceSelectionAfterArchive = useCallback(
    (session: { id: string; agent_id: string }) => {
      if (activeSessionId !== session.id) return;
      const history = sortChatSessions(
        sessions.filter((s) => s.status !== "archived"),
      );
      const idx = history.findIndex((s) => s.id === session.id);
      const next = history[idx + 1] ?? history[idx - 1] ?? null;
      if (next) handleSelectSession(next);
      else setActiveSession(null);
    },
    [activeSessionId, sessions, handleSelectSession, setActiveSession],
  );

  const archiveSession = useCallback(
    (sessionId: string) => setArchived.mutate({ sessionId, archived: true }),
    [setArchived],
  );

  const hasMessages = messages.length > 0 || !!pendingTaskId;

  return {
    // identity / lists
    wsId,
    user,
    agents,
    availableAgents,
    agentsSettled,
    sessions,
    activeSessionId,
    selectedAgentId,
    currentSession,
    isSessionArchived,
    isAgentArchived,
    activeAgent,
    noAgent,
    availability,
    // messages
    messages,
    pendingTask,
    pendingTaskId,
    showSkeleton,
    hasMessages,
    firstItemIndex,
    hasOlderMessages: !!hasOlderMessages,
    isFetchingOlderMessages,
    fetchOlderMessages,
    // draft restore
    restoreDraftRequest,
    handleRestoreDraftConsumed,
    // compose-box focus nonce (bumped on new chat)
    focusInputRequest,
    // actions
    handleSend,
    handleStop,
    handleUploadFile,
    handleNewChat,
    handleStartNewChat,
    handleSelectSession,
    advanceSelectionAfterArchive,
    archiveSession,
    // store setters (for surfaces that sync selection to the URL, etc.)
    setActiveSession,
    setSelectedAgentId,
  };
}

export type ChatController = ReturnType<typeof useChatController>;
