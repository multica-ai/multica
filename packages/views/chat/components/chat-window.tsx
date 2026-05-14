"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { motion } from "motion/react";
import { Minus, Maximize2, Minimize2, ChevronDown, ChevronRight, Plus, Check, Trash2, Pencil } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { canAssignAgent } from "@multica/views/issues/components";
import { api } from "@multica/core/api";
import { useAgentPresenceDetail, useWorkspaceAgentAvailability } from "@multica/core/agents";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { ActorAvatar } from "../../common/actor-avatar";
import { OfflineBanner } from "./offline-banner";
import { NoAgentBanner } from "./no-agent-banner";
import {
  chatSessionsOptions,
  chatMessagesOptions,
  pendingChatTaskOptions,
  pendingChatTasksOptions,
  chatKeys,
} from "@multica/core/chat/queries";
import {
  useCreateChatSession,
  useDeleteChatSession,
  useMarkChatSessionRead,
  useUpdateChatSession,
} from "@multica/core/chat/mutations";
import { useChatStore } from "@multica/core/chat";
import { ChatMessageList, ChatMessageSkeleton } from "./chat-message-list";
import { ChatInput } from "./chat-input";
import {
  ContextAnchorButton,
  ContextAnchorCard,
  buildAnchorMarkdown,
  useRouteAnchorCandidate,
} from "./context-anchor";
import { ChatResizeHandles } from "./chat-resize-handles";
import { useChatResize } from "./use-chat-resize";
import { createLogger } from "@multica/core/logger";
import type { Agent, ChatMessage, ChatPendingTask, ChatSession } from "@multica/core/types";
import { useT } from "../../i18n";

const uiLogger = createLogger("chat.ui");
const apiLogger = createLogger("chat.api");

export function ChatWindow() {
  const { t } = useT("chat");
  const wsId = useWorkspaceId();
  const isOpen = useChatStore((s) => s.isOpen);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const selectedAgentId = useChatStore((s) => s.selectedAgentId);
  const setOpen = useChatStore((s) => s.setOpen);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const setSelectedAgentId = useChatStore((s) => s.setSelectedAgentId);
  const user = useAuthStore((s) => s.user);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  // Single sessions cache. The dropdown groups locally into "active" /
  // "archived" — eliminating the separate active/all queries that used
  // to drift during the WS-invalidate window.
  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));
  const { data: rawMessages, isLoading: messagesLoading } = useQuery(
    chatMessagesOptions(activeSessionId ?? ""),
  );
  // When no active session, always show empty — don't use stale cache
  const messages = activeSessionId ? rawMessages ?? [] : [];
  // Skeleton only shows for an un-cached session fetch. Cached switches
  // return data synchronously — no flash. `enabled: false` (new chat)
  // keeps isLoading false so the starter prompts aren't hidden.
  const showSkeleton = !!activeSessionId && messagesLoading;

  // Server-authoritative pending task. Survives refresh / reopen / session
  // switch because it's keyed on sessionId in the Query cache; WS events
  // (chat:message / chat:done / task:*) keep it invalidated in real time.
  //
  // This is the SOLE source for pendingTaskId — no mirror in the store.
  const { data: pendingTask } = useQuery(
    pendingChatTaskOptions(activeSessionId ?? ""),
  );
  const pendingTaskId = pendingTask?.task_id ?? null;

  // Legacy archived sessions (the old soft-archive feature was removed but
  // pre-existing rows with status='archived' may still exist) render as
  // read-only: dropdown keeps showing them under "archived", but ChatInput
  // is disabled and the server still rejects POST /messages for them.
  const currentSession = activeSessionId
    ? sessions.find((s) => s.id === activeSessionId)
    : null;
  const isSessionArchived = currentSession?.status === "archived";

  const qc = useQueryClient();
  const createSession = useCreateChatSession();
  const markRead = useMarkChatSessionRead();

  const currentMember = members.find((m) => m.user_id === user?.id);
  const memberRole = currentMember?.role;
  const availableAgents = agents.filter(
    (a) => !a.archived_at && canAssignAgent(a, user?.id, memberRole),
  );

  // Resolve selected agent: stored preference → first available
  const activeAgent =
    availableAgents.find((a) => a.id === selectedAgentId) ??
    availableAgents[0] ??
    null;

  // Three-state availability — "loading" stays neutral (no banner, no
  // disable) so the input doesn't flash a fake "no agent" state in the
  // few hundred ms before the agent list query resolves. Only `"none"`
  // (server confirmed: zero usable agents) drives the disabled UI.
  const agentAvailability = useWorkspaceAgentAvailability();
  const noAgent = agentAvailability === "none";

  // Presence drives both the avatar status dot (via ActorAvatar) and the
  // OfflineBanner / TaskStatusPill availability copy. `useAgentPresenceDetail`
  // returns "loading" while queries are still resolving — pass `undefined`
  // downstream so banners and pill copy stay silent during loading rather
  // than flash speculative offline text.
  const presenceDetail = useAgentPresenceDetail(wsId, activeAgent?.id);
  const availability =
    presenceDetail === "loading" ? undefined : presenceDetail.availability;

  // Mount / unmount logging. ChatWindow lives in DashboardLayout, so this
  // fires on layout mount (login / workspace switch / fresh page load).
  useEffect(() => {
    uiLogger.info("ChatWindow mount", {
      isOpen,
      activeSessionId,
      pendingTaskId,
      selectedAgentId,
      wsId,
    });
    return () => {
      uiLogger.info("ChatWindow unmount", {
        activeSessionId,
        pendingTaskId,
      });
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once per mount
  }, []);

  // Open intent is fully driven by `activeSessionId` in storage — no mount
  // restore, no self-heal. Adding either reintroduces a "two signals
  // describing one fact" race (the previous self-heal mis-cleared the
  // freshly-created session because allSessions was still stale during the
  // post-create invalidate-refetch window).

  // WS events are handled globally in useRealtimeSync — the query cache
  // stays current even when this window is closed. See packages/core/realtime/.

  // Auto mark-as-read whenever the user is looking at a session with unread
  // state: window open + a session active + has_unread → PATCH.
  // has_unread comes from the list query; WS handlers invalidate it on
  // chat:done so a reply arriving while the user watches triggers this
  // effect again and is instantly cleared.
  const currentHasUnread =
    sessions.find((s) => s.id === activeSessionId)?.has_unread ?? false;
  useEffect(() => {
    if (!isOpen || !activeSessionId) return;
    if (!currentHasUnread) return;
    uiLogger.info("auto markRead", { sessionId: activeSessionId });
    markRead.mutate(activeSessionId);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- markRead ref stable
  }, [isOpen, activeSessionId, currentHasUnread]);

  // Focus-mode anchor: derived from route each render. Prepended to the
  // outgoing message when focus is on; the anchor persists across sends
  // (focus mode tracks the user's page, not a per-message attachment).
  const { candidate: anchorCandidate } = useRouteAnchorCandidate(wsId);

  const { uploadWithToast } = useFileUpload(api);

  // Lazy-creates a chat_session the first time the user needs an id —
  // either to send a message or to attach an uploaded file. Pulled out of
  // handleSend so the upload path (which fires before any text exists) can
  // get a session_id to hang the attachment on. Returns null when no agent
  // is available; callers must early-return in that case.
  //
  // Concurrent callers (e.g. user drops a file → handleUploadFile, then
  // quickly clicks send → handleSend) would each observe activeSessionId
  // === null and fire a separate createSession.mutateAsync, creating two
  // sessions and orphaning the attachment on the wrong one. The in-flight
  // promise ref dedupes those races: the first caller starts the create,
  // every subsequent caller awaits the same promise until it settles.
  //
  // titleSeed is the first 50 chars of the user's message when called from
  // send; the upload path passes "" and we leave the title empty so the
  // session-dropdown's existing localized `window.untitled` fallback kicks
  // in. A follow-up task may back-fill the real title from the first user
  // message — until then this keeps the session list scannable across locales.
  //
  // NOTE: ensureSession does NOT flip `activeSessionId` itself. Callers must
  // seed `chatKeys.messages(sessionId)` in the Query cache BEFORE calling
  // `setActiveSession(sessionId)`, otherwise the first useQuery subscription
  // for the new key reports `isLoading: true` and renders ChatMessageSkeleton
  // for one frame (the "new-chat first-message" white flash).
  const sessionPromiseRef = useRef<Promise<string | null> | null>(null);
  const ensureSession = useCallback(
    async (titleSeed: string): Promise<string | null> => {
      if (activeSessionId) return activeSessionId;
      if (!activeAgent) return null;
      if (sessionPromiseRef.current) return sessionPromiseRef.current;

      const promise = (async () => {
        try {
          const session = await createSession.mutateAsync({
            agent_id: activeAgent.id,
            title: titleSeed.slice(0, 50),
          });
          return session.id;
        } finally {
          sessionPromiseRef.current = null;
        }
      })();
      sessionPromiseRef.current = promise;
      return promise;
    },
    [activeSessionId, activeAgent, createSession],
  );

  const handleUploadFile = useCallback(
    async (file: File) => {
      const sessionId = await ensureSession("");
      if (!sessionId) return null;
      // Prime the messages cache as empty before flipping activeSessionId so
      // ChatMessageList mounts directly (no Skeleton frame). Skip the write
      // when an entry already exists — a concurrent handleSend may have
      // seeded an optimistic message we must not clobber.
      qc.setQueryData<ChatMessage[]>(
        chatKeys.messages(sessionId),
        (old) => old ?? [],
      );
      setActiveSession(sessionId);
      return uploadWithToast(file, { chatSessionId: sessionId });
    },
    [ensureSession, uploadWithToast, qc, setActiveSession],
  );

  const handleSend = useCallback(
    async (content: string, attachmentIds?: string[]) => {
      if (!activeAgent) {
        apiLogger.warn("sendChatMessage skipped: no active agent");
        return;
      }

      const focusOn = useChatStore.getState().focusMode;
      const finalContent = focusOn && anchorCandidate
        ? `${buildAnchorMarkdown(anchorCandidate)}\n\n${content}`
        : content;

      const isNewSession = !activeSessionId;

      apiLogger.info("sendChatMessage.start", {
        sessionId: activeSessionId,
        isNewSession,
        agentId: activeAgent.id,
        contentLength: finalContent.length,
        hasAnchor: focusOn && !!anchorCandidate,
        attachmentCount: attachmentIds?.length ?? 0,
      });

      const sessionId = await ensureSession(finalContent);
      if (!sessionId) {
        apiLogger.warn("sendChatMessage aborted: ensureSession returned null");
        return;
      }

      // Optimistic burst — everything that gives the user "I sent a message
      // and the agent is now working" feedback fires BEFORE the HTTP roundtrip.
      // Pre-#status-pill the pending-task seed lived after `await
      // sendChatMessage` and the pill blinked in a few hundred ms after the
      // user's message — small but visible "did it actually send?" gap.
      const sentAt = new Date().toISOString();
      const optimistic: ChatMessage = {
        id: `optimistic-${Date.now()}`,
        chat_session_id: sessionId,
        role: "user",
        content: finalContent,
        task_id: null,
        created_at: sentAt,
      };
      // Seed cache BEFORE flipping activeSessionId. If we set the active
      // session first, useQuery's first subscription to the new key sees no
      // cached data and renders ChatMessageSkeleton for one frame — the
      // "new-chat first-message" white flash. Priming the cache first means
      // the very first read after activeSessionId flips hits data
      // synchronously and ChatMessageList mounts directly.
      qc.setQueryData<ChatMessage[]>(
        chatKeys.messages(sessionId),
        (old) => (old ? [...old, optimistic] : [optimistic]),
      );
      // Seed the pending-task with a temporary id so the StatusPill mounts
      // and starts ticking the instant the user clicks send. Real task_id
      // and server-authoritative created_at land below; until then the pill
      // is anchored to the local clock (drift is the request RTT, ~50–200ms,
      // which doesn't change the rendered "Ns" value).
      qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sessionId), {
        task_id: `optimistic-${optimistic.id}`,
        status: "queued",
        created_at: sentAt,
      });
      // Cache primed → safe to publish the new active session. Idempotent
      // when the session was already active (existing-conversation send).
      setActiveSession(sessionId);
      apiLogger.debug("sendChatMessage.optimistic", { sessionId, optimisticId: optimistic.id });

      const result = await api.sendChatMessage(sessionId, finalContent, attachmentIds);
      apiLogger.info("sendChatMessage.success", {
        sessionId,
        messageId: result.message_id,
        taskId: result.task_id,
      });
      // Replace the temporary task_id with the server's real one (so the WS
      // task: handlers can match against it) and snap the anchor to the
      // server's created_at — keeping the elapsed-seconds reading stable.
      qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask(sessionId), {
        task_id: result.task_id,
        status: "queued",
        created_at: result.created_at,
      });
      qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
    },
    [
      activeSessionId,
      activeAgent,
      anchorCandidate,
      ensureSession,
      qc,
      setActiveSession,
    ],
  );

  const handleStop = useCallback(() => {
    if (!pendingTaskId || !activeSessionId) {
      apiLogger.debug("cancelTask skipped: no pending task");
      return;
    }
    // Optimistic clear — pill disappears + input unlocks the moment the
    // user clicks Stop, instead of after the HTTP roundtrip. WS
    // task:cancelled will confirm later (no-op if cache is already empty);
    // if the cancel POST fails because the task already finished, the
    // assistant message arrives via task:completed → chat:done and renders
    // normally. Either way the UI is in sync with reality without latency.
    apiLogger.info("cancelTask.start", { taskId: pendingTaskId, sessionId: activeSessionId });
    qc.setQueryData(chatKeys.pendingTask(activeSessionId), {});
    qc.invalidateQueries({ queryKey: chatKeys.messages(activeSessionId) });
    // Fire-and-forget — UI is already in its post-cancel state. We log the
    // outcome but never block on it.
    api.cancelTaskById(pendingTaskId).then(
      () => apiLogger.info("cancelTask.success", { taskId: pendingTaskId }),
      (err) =>
        apiLogger.warn("cancelTask.error (task may have already finished)", {
          taskId: pendingTaskId,
          err,
        }),
    );
  }, [pendingTaskId, activeSessionId, qc]);

  const handleSelectAgent = useCallback(
    (agent: Agent) => {
      // No-op when clicking the already-active agent — don't clobber the
      // current session just because the user closed the menu this way.
      // Compare against activeAgent (what the UI shows), not selectedAgentId
      // (which may be null / point to an archived agent on first load).
      if (activeAgent && agent.id === activeAgent.id) return;
      uiLogger.info("selectAgent", {
        from: selectedAgentId,
        to: agent.id,
        previousSessionId: activeSessionId,
      });
      setSelectedAgentId(agent.id);
      // Reset session when switching agent
      setActiveSession(null);
    },
    [activeAgent, selectedAgentId, activeSessionId, setSelectedAgentId, setActiveSession],
  );

  const handleNewChat = useCallback(() => {
    uiLogger.info("newChat", {
      previousSessionId: activeSessionId,
      previousPendingTask: pendingTaskId,
    });
    setActiveSession(null);
  }, [activeSessionId, pendingTaskId, setActiveSession]);

  const handleSelectSession = useCallback(
    (session: ChatSession) => {
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

  const handleMinimize = useCallback(() => {
    uiLogger.info("minimize (close)", {
      activeSessionId,
      pendingTaskId,
    });
    setOpen(false);
  }, [activeSessionId, pendingTaskId, setOpen]);

  const isExpanded = useChatStore((s) => s.isExpanded);

  const windowRef = useRef<HTMLDivElement>(null);
  const { renderWidth, renderHeight, isAtMax, boundsReady, isDragging, toggleExpand, startDrag } = useChatResize(windowRef);

  // Show the list (vs empty state) as soon as there's anything to display —
  // a real message, or a pending task whose timeline will stream in.
  const hasMessages = messages.length > 0 || !!pendingTaskId;

  const isVisible = isOpen && (isExpanded || boundsReady);

  const containerClass = isExpanded
    ? "absolute inset-3 z-50 flex flex-col rounded-xl ring-1 ring-foreground/10 bg-sidebar shadow-2xl overflow-hidden"
    : "absolute bottom-2 right-2 z-50 flex flex-col rounded-xl ring-1 ring-foreground/10 bg-sidebar shadow-2xl overflow-hidden";
  const containerStyle: React.CSSProperties = {
    ...(!isExpanded ? { width: renderWidth, height: renderHeight } : {}),
    transformOrigin: "bottom right",
    pointerEvents: isOpen ? "auto" : "none",
  };

  return (
    <motion.div
      ref={windowRef}
      className={containerClass}
      style={containerStyle}
      layout="position"
      initial={{ opacity: 0, scale: 0.95 }}
      animate={{
        opacity: isVisible ? 1 : 0,
        scale: isVisible ? 1 : 0.95,
      }}
      transition={{
        layout: isDragging
          ? { duration: 0 }
          : { type: "spring", duration: 0.3, bounce: 0 },
        opacity: { duration: 0.15 },
        scale: { type: "spring", duration: 0.2, bounce: 0 },
      }}
    >
      {!isExpanded && <ChatResizeHandles onDragStart={startDrag} />}
      {/* Header — ⊕ new + session dropdown | window tools */}
      <div className="flex items-center justify-between border-b px-4 py-2.5 gap-2">
        <div className="flex items-center gap-1 min-w-0">
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="rounded-full text-muted-foreground"
                  onClick={handleNewChat}
                />
              }
            >
              <Plus />
            </TooltipTrigger>
            <TooltipContent side="top">{t(($) => $.window.new_chat_tooltip)}</TooltipContent>
          </Tooltip>
          <SessionDropdown
            sessions={sessions}
            // Use the full agent list (incl. archived) so historical
            // sessions can still resolve their avatar.
            agents={agents}
            activeSessionId={activeSessionId}
            onSelectSession={handleSelectSession}
          />
        </div>
        <div className="flex items-center gap-0.5 shrink-0">
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted-foreground"
                  onClick={toggleExpand}
                />
              }
            >
              {isExpanded || isAtMax ? <Minimize2 /> : <Maximize2 />}
            </TooltipTrigger>
            <TooltipContent side="top">
              {isExpanded || isAtMax ? t(($) => $.window.restore_tooltip) : t(($) => $.window.expand_tooltip)}
            </TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted-foreground"
                  onClick={handleMinimize}
                />
              }
            >
              <Minus />
            </TooltipTrigger>
            <TooltipContent side="top">{t(($) => $.window.minimize_tooltip)}</TooltipContent>
          </Tooltip>
        </div>
      </div>

      {/* Messages / skeleton / empty state */}
      {showSkeleton ? (
        <ChatMessageSkeleton />
      ) : hasMessages ? (
        <ChatMessageList
          messages={messages}
          pendingTask={pendingTask}
          availability={availability}
        />
      ) : (
        <EmptyState
          hasSessions={sessions.length > 0}
          agentName={activeAgent?.name}
          onPickPrompt={(text) => handleSend(text)}
        />
      )}

      {/* Status banner above the input — single mutually-exclusive slot.
       *  Priority: no-agent > offline / unstable. Agent presence is the
       *  hard prerequisite (you can't send anything without one), so it
       *  always wins over a presence hint. ContextAnchorCard stays in
       *  topSlot because that's per-message context, not session state.
       *
       *  We key off `noAgent` (the resolved-empty state) rather than
       *  `!activeAgent`, so the loading window between mount and the
       *  first agent-list response stays banner-free. */}
      {noAgent ? (
        <NoAgentBanner />
      ) : (
        <OfflineBanner agentName={activeAgent?.name} availability={availability} />
      )}

      {/* Input — disabled for legacy archived sessions; locked out entirely
       *  when there's no agent (the EmptyState above carries the CTA). */}
      <ChatInput
        onSend={handleSend}
        onUploadFile={handleUploadFile}
        onStop={handleStop}
        isRunning={!!pendingTaskId}
        disabled={isSessionArchived}
        noAgent={noAgent}
        agentName={activeAgent?.name}
        topSlot={<ContextAnchorCard />}
        leftAdornment={
          <AgentDropdown
            agents={availableAgents}
            activeAgent={activeAgent}
            userId={user?.id}
            onSelect={handleSelectAgent}
          />
        }
        rightAdornment={<ContextAnchorButton />}
      />
    </motion.div>
  );
}

/**
 * Agent dropdown: avatar trigger, lists all available agents. Selecting a
 * different agent = switch agent + start a fresh chat (session=null).
 * The current agent is marked with a check and not clickable.
 */
function AgentDropdown({
  agents,
  activeAgent,
  userId,
  onSelect,
}: {
  agents: Agent[];
  activeAgent: Agent | null;
  userId: string | undefined;
  onSelect: (agent: Agent) => void;
}) {
  const { t } = useT("chat");
  // Split into the user's own agents and everyone else so the menu groups
  // them — matches the old AgentSelector layout.
  const { mine, others } = useMemo(() => {
    const mine: Agent[] = [];
    const others: Agent[] = [];
    for (const a of agents) {
      if (a.owner_id === userId) mine.push(a);
      else others.push(a);
    }
    return { mine, others };
  }, [agents, userId]);

  if (!activeAgent) {
    return <span className="text-xs text-muted-foreground">{t(($) => $.window.no_agents)}</span>;
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger className="flex items-center gap-1.5 rounded-md px-1.5 py-1 -ml-1 cursor-pointer outline-none transition-colors hover:bg-accent aria-expanded:bg-accent">
        <ActorAvatar
          actorType="agent"
          actorId={activeAgent.id}
          size={24}
          enableHoverCard
          showStatusDot
        />
        <span className="text-xs font-medium max-w-28 truncate">{activeAgent.name}</span>
        <ChevronDown className="size-3 text-muted-foreground shrink-0" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" side="top" className="max-h-80 w-auto max-w-64">
        {mine.length > 0 && (
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t(($) => $.window.my_agents)}</DropdownMenuLabel>
            {mine.map((agent) => (
              <AgentMenuItem
                key={agent.id}
                agent={agent}
                isCurrent={agent.id === activeAgent.id}
                onSelect={onSelect}
              />
            ))}
          </DropdownMenuGroup>
        )}
        {mine.length > 0 && others.length > 0 && <DropdownMenuSeparator />}
        {others.length > 0 && (
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t(($) => $.window.others)}</DropdownMenuLabel>
            {others.map((agent) => (
              <AgentMenuItem
                key={agent.id}
                agent={agent}
                isCurrent={agent.id === activeAgent.id}
                onSelect={onSelect}
              />
            ))}
          </DropdownMenuGroup>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function AgentMenuItem({
  agent,
  isCurrent,
  onSelect,
}: {
  agent: Agent;
  isCurrent: boolean;
  onSelect: (agent: Agent) => void;
}) {
  return (
    <DropdownMenuItem
      onClick={() => onSelect(agent)}
      className="flex min-w-0 items-center gap-2"
    >
      <ActorAvatar
        actorType="agent"
        actorId={agent.id}
        size={24}
        enableHoverCard
        showStatusDot
      />
      <span className="truncate flex-1">{agent.name}</span>
      {isCurrent && <Check className="size-3.5 text-muted-foreground shrink-0" />}
    </DropdownMenuItem>
  );
}

/**
 * Session dropdown: groups all sessions into "active" and "archived". The
 * archived branch is collapsed by default and only mounts on demand to
 * keep the menu compact when the user has many old chats. Selecting a
 * session from a different agent implicitly switches the agent too
 * (sessions are bound 1:1 to an agent). "New chat" lives in the header's
 * ⊕ button, not inside this dropdown.
 */
function SessionDropdown({
  sessions,
  agents,
  activeSessionId,
  onSelectSession,
}: {
  sessions: ChatSession[];
  agents: Agent[];
  activeSessionId: string | null;
  onSelectSession: (session: ChatSession) => void;
}) {
  const { t } = useT("chat");
  const wsId = useWorkspaceId();
  const agentById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);
  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const title = activeSession?.title?.trim() || t(($) => $.window.untitled);
  const triggerAgent = activeSession ? agentById.get(activeSession.agent_id) ?? null : null;

  const { active, archived } = useMemo(() => {
    const active: ChatSession[] = [];
    const archived: ChatSession[] = [];
    for (const s of sessions) {
      if (s.status === "archived") archived.push(s);
      else active.push(s);
    }
    return { active, archived };
  }, [sessions]);

  const [showArchived, setShowArchived] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<ChatSession | null>(null);
  // Inline rename: only one row can be in edit mode at a time. We track the
  // session id (not the full session) so a stale closure can't overwrite a
  // newer rename pulled in via WS.
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const deleteSession = useDeleteChatSession();
  const updateSession = useUpdateChatSession();
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const formatTimeAgo = useFormatTimeAgo();

  // Aggregate "which sessions have an in-flight task right now". Reuses
  // the same workspace-scoped query the FAB consumes, so toggling the chat
  // window doesn't fire a second request — TanStack dedupes by key.
  const { data: pending } = useQuery(pendingChatTasksOptions(wsId));
  const inFlightSessionIds = useMemo(
    () => new Set((pending?.tasks ?? []).map((t) => t.chat_session_id)),
    [pending],
  );

  // Cross-session aggregate signal for the closed-dropdown trigger.
  // "Active" here means there's something interesting happening in a
  // session OTHER than the one the user is currently looking at — the
  // user already sees their own session's state via the StatusPill /
  // unread auto-mark, so highlighting it on the trigger would be noise.
  // Same priority rule as the row pips: running > unread.
  const otherSessionRunning = sessions.some(
    (s) => s.id !== activeSessionId && inFlightSessionIds.has(s.id),
  );
  const otherSessionUnread = sessions.some(
    (s) => s.id !== activeSessionId && s.has_unread,
  );

  const handleConfirmDelete = () => {
    if (!pendingDelete) return;
    const sessionId = pendingDelete.id;
    // Eager local clear when the user is deleting the session they're
    // currently looking at — otherwise messages / pendingTask queries
    // keep rendering the now-deleted session until chat:session_deleted
    // arrives over WS (~50–200ms gap).
    if (activeSessionId === sessionId) setActiveSession(null);
    deleteSession.mutate(sessionId, {
      onSettled: () => setPendingDelete(null),
    });
  };

  const handleSubmitRename = (sessionId: string, raw: string) => {
    const trimmed = raw.trim();
    const current = sessions.find((s) => s.id === sessionId);
    setRenamingId(null);
    // No-op submits (unchanged or blank) skip the network round-trip — the
    // server would reject a blank title anyway, and an unchanged title would
    // just bump updated_at for no user-visible reason.
    if (!trimmed || trimmed === current?.title) return;
    updateSession.mutate({ sessionId, title: trimmed });
  };

  const renderRow = (session: ChatSession) => {
    const isCurrent = session.id === activeSessionId;
    const agent = agentById.get(session.agent_id) ?? null;
    const isRunning = inFlightSessionIds.has(session.id);
    const isRenaming = renamingId === session.id;
    return (
      <DropdownMenuItem
        key={session.id}
        // While renaming we don't want a row click to select the session
        // OR close the menu — the user is editing text, not navigating.
        // closeOnClick=false keeps the dropdown open across input clicks
        // / button clicks inside the row; the normal "click row → switch
        // session → close menu" flow is unchanged when isRenaming=false.
        closeOnClick={!isRenaming}
        onClick={() => {
          if (isRenaming) return;
          onSelectSession(session);
        }}
        className="group flex min-w-0 items-center gap-2"
      >
        {agent ? (
          <ActorAvatar
            actorType="agent"
            actorId={agent.id}
            size={24}
            enableHoverCard
            showStatusDot
          />
        ) : (
          <span className="size-6 shrink-0" />
        )}
        <div className="min-w-0 flex-1">
          {isRenaming ? (
            <SessionRenameInput
              initialValue={session.title ?? ""}
              onSubmit={(value) => handleSubmitRename(session.id, value)}
              onCancel={() => setRenamingId(null)}
            />
          ) : (
            <>
              <div className="truncate text-sm">
                {session.title?.trim() || t(($) => $.window.untitled)}
              </div>
              <div className="truncate text-xs text-muted-foreground/70">
                {formatTimeAgo(session.updated_at)}
              </div>
            </>
          )}
        </div>
        {/* Right-edge status pip: in-flight wins over unread because
         *  "still working" is more actionable than "has reply" — and
         *  the two rarely coexist in practice (the unread flag fires
         *  on chat_message write, by which point the task has just
         *  finished). Same pip shape as unread for visual rhythm,
         *  amber + pulse to read as activity.
         *
         *  Hidden while renaming so the inline input has room to
         *  breathe and trailing pips don't visually trail off-screen
         *  next to the editor caret. */}
        {!isRenaming && isRunning ? (
          <span
            aria-label={t(($) => $.window.running)}
            title={t(($) => $.window.running)}
            className="size-1.5 shrink-0 rounded-full bg-amber-500 animate-pulse"
          />
        ) : !isRenaming && session.has_unread ? (
          <span
            aria-label={t(($) => $.window.unread)}
            title={t(($) => $.window.unread)}
            className="size-1.5 shrink-0 rounded-full bg-brand"
          />
        ) : null}
        {!isRenaming && isCurrent && (
          <Check className="size-3.5 text-muted-foreground shrink-0" />
        )}
        {!isRenaming && (
          <>
            <button
              type="button"
              // preventDefault is what tells Base UI's Menu.Item to skip
              // its close-on-click; stopPropagation prevents the row's
              // onClick from also firing (which would switch sessions).
              // onPointerDown is stopped too so the menu's typeahead /
              // focus tracking doesn't pre-empt the click.
              onPointerDown={(e) => e.stopPropagation()}
              onClick={(e) => {
                e.stopPropagation();
                e.preventDefault();
                setRenamingId(session.id);
              }}
              className="shrink-0 rounded p-1 text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground focus-visible:opacity-100 group-hover:opacity-100"
              aria-label={t(($) => $.session_history.row_rename_aria)}
              title={t(($) => $.session_history.row_rename_aria)}
            >
              <Pencil className="size-3.5" />
            </button>
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                e.preventDefault();
                setPendingDelete(session);
              }}
              className="shrink-0 rounded p-1 text-muted-foreground opacity-0 transition-opacity hover:bg-destructive/10 hover:text-destructive focus-visible:opacity-100 group-hover:opacity-100"
              aria-label={t(($) => $.session_history.row_delete_aria)}
            >
              <Trash2 className="size-3.5" />
            </button>
          </>
        )}
      </DropdownMenuItem>
    );
  };

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger className="flex max-w-96 min-w-0 items-center gap-1.5 rounded-md px-1.5 py-1 transition-colors hover:bg-accent aria-expanded:bg-accent">
          {triggerAgent && (
            <ActorAvatar
              actorType="agent"
              actorId={triggerAgent.id}
              size={24}
              enableHoverCard
              showStatusDot
            />
          )}
          <span className="min-w-0 truncate text-sm font-medium">{title}</span>
          {otherSessionRunning ? (
            <span
              aria-label={t(($) => $.window.another_running)}
              title={t(($) => $.window.another_running)}
              className="size-1.5 shrink-0 rounded-full bg-amber-500 animate-pulse"
            />
          ) : otherSessionUnread ? (
            <span
              aria-label={t(($) => $.window.another_unread)}
              title={t(($) => $.window.another_unread)}
              className="size-1.5 shrink-0 rounded-full bg-brand"
            />
          ) : null}
          <ChevronDown className="size-3 text-muted-foreground shrink-0" />
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align="start"
          className="max-h-96 w-auto min-w-[max(16rem,var(--anchor-width,16rem))] max-w-96 overflow-y-auto"
        >
          {sessions.length === 0 ? (
            <div className="px-2 py-1.5 text-xs text-muted-foreground">
              {t(($) => $.window.no_previous)}
            </div>
          ) : (
            <>
              {active.length > 0 && (
                <DropdownMenuGroup>
                  <DropdownMenuLabel>{t(($) => $.window.active_group)}</DropdownMenuLabel>
                  {active.map(renderRow)}
                </DropdownMenuGroup>
              )}
              {archived.length > 0 && (
                <>
                  {active.length > 0 && <DropdownMenuSeparator />}
                  <DropdownMenuItem
                    onClick={(e) => {
                      e.preventDefault();
                      setShowArchived((v) => !v);
                    }}
                    className="flex items-center gap-1.5 text-xs text-muted-foreground"
                  >
                    {showArchived ? (
                      <ChevronDown className="size-3" />
                    ) : (
                      <ChevronRight className="size-3" />
                    )}
                    <span>
                      {t(($) => $.window.archived_group, { count: archived.length })}
                    </span>
                  </DropdownMenuItem>
                  {showArchived && (
                    <DropdownMenuGroup>
                      {archived.map(renderRow)}
                    </DropdownMenuGroup>
                  )}
                </>
              )}
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

      <AlertDialog
        open={!!pendingDelete}
        onOpenChange={(open) => {
          if (!open && !deleteSession.isPending) setPendingDelete(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.session_history.delete_dialog.title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {pendingDelete?.title
                ? t(($) => $.session_history.delete_dialog.description_with_title, {
                    title: pendingDelete.title,
                  })
                : t(($) => $.session_history.delete_dialog.description_default)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteSession.isPending}>
              {t(($) => $.session_history.delete_dialog.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={handleConfirmDelete}
              disabled={deleteSession.isPending}
              className="bg-destructive text-white hover:bg-destructive/90"
            >
              {deleteSession.isPending
                ? t(($) => $.session_history.delete_dialog.confirming)
                : t(($) => $.session_history.delete_dialog.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

/**
 * Inline editor for a session title. Mounts focused with the existing
 * title pre-selected so the user can either replace it outright or arrow
 * into the existing text. Enter commits, Escape cancels, a real click
 * outside the input also commits.
 *
 * We do NOT commit on the input's `blur` event: Base UI's Menu uses
 * focus-follows-cursor (hovering a sibling row drags DOM focus there),
 * so a blur handler would fire on every mouse-move and "save" the user's
 * half-typed title without them clicking anywhere. Instead a document-
 * level `pointerdown` listener — registered in capture phase so it runs
 * before Base UI's outside-click close handler — commits when the user
 * actually clicks outside the input.
 */
function SessionRenameInput({
  initialValue,
  onSubmit,
  onCancel,
}: {
  initialValue: string;
  onSubmit: (value: string) => void;
  onCancel: () => void;
}) {
  const { t } = useT("chat");
  const [value, setValue] = useState(initialValue);
  const inputRef = useRef<HTMLInputElement>(null);
  // Hold the latest value + callback in refs so the mount-only effect's
  // listener always sees fresh state without re-subscribing on every
  // keystroke (which would briefly leave a window where pointerdown isn't
  // observed).
  const valueRef = useRef(value);
  valueRef.current = value;
  const onSubmitRef = useRef(onSubmit);
  onSubmitRef.current = onSubmit;

  useEffect(() => {
    inputRef.current?.focus();
    inputRef.current?.select();

    const handlePointerDown = (e: PointerEvent) => {
      const input = inputRef.current;
      if (!input) return;
      if (input.contains(e.target as Node)) return;
      onSubmitRef.current(valueRef.current);
    };
    // Capture phase — Base UI registers its own outside-click handler in
    // bubble; running first lets us commit before the menu starts to
    // close (and unmount this component).
    document.addEventListener("pointerdown", handlePointerDown, true);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown, true);
    };
  }, []);

  return (
    <input
      ref={inputRef}
      type="text"
      value={value}
      maxLength={200}
      aria-label={t(($) => $.session_history.row_rename_aria)}
      onChange={(e) => setValue(e.target.value)}
      onClick={(e) => e.stopPropagation()}
      onPointerDown={(e) => e.stopPropagation()}
      onKeyDown={(e) => {
        // Stop the menu from stealing arrow / typeahead / space input.
        e.stopPropagation();
        if (e.key === "Enter") {
          e.preventDefault();
          onSubmit(value);
        } else if (e.key === "Escape") {
          e.preventDefault();
          onCancel();
        }
      }}
      className="w-full rounded-sm bg-background px-1 py-0.5 text-sm outline-none ring-1 ring-border focus-visible:ring-brand"
    />
  );
}

function useFormatTimeAgo(): (dateStr: string) => string {
  const { t } = useT("chat");
  return (dateStr: string) => {
    const date = new Date(dateStr);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMs / 3600000);
    const diffDays = Math.floor(diffMs / 86400000);

    if (diffMins < 1) return t(($) => $.session_history.time.just_now);
    if (diffMins < 60) return t(($) => $.session_history.time.minutes, { count: diffMins });
    if (diffHours < 24) return t(($) => $.session_history.time.hours, { count: diffHours });
    if (diffDays < 7) return t(($) => $.session_history.time.days, { count: diffDays });
    return date.toLocaleDateString();
  };
}

// Three starter prompts shown on the empty state. Each is keyed into the
// chat namespace so labels translate per locale; the icon stays raw since
// emojis are locale-neutral.
const STARTER_KEYS: ("list_open" | "summarize_today" | "plan_next")[] = [
  "list_open",
  "summarize_today",
  "plan_next",
];
const STARTER_ICONS: Record<(typeof STARTER_KEYS)[number], string> = {
  list_open: "📋",
  summarize_today: "📝",
  plan_next: "💡",
};

function EmptyState({
  hasSessions,
  agentName,
  onPickPrompt,
}: {
  hasSessions: boolean;
  agentName?: string;
  onPickPrompt: (text: string) => void;
}) {
  const { t } = useT("chat");
  // First-time experience: the user has never started a chat in this
  // workspace. Educate before suggesting actions — starter prompts
  // presume the user already knows what chat is for.
  if (!hasSessions) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-8">
        <div className="text-center space-y-3">
          <h3 className="text-base font-semibold">
            {t(($) => $.empty_state.first_time_title)}
          </h3>
          <p className="text-sm text-muted-foreground">
            {t(($) => $.empty_state.first_time_intro)}{" "}
            <span className="font-medium text-foreground">
              {t(($) => $.empty_state.first_time_pillars)}
            </span>
            {t(($) => $.empty_state.first_time_pillars_suffix)}
          </p>
          <p className="text-sm text-muted-foreground">
            {t(($) => $.empty_state.first_time_actions)}
          </p>
        </div>
      </div>
    );
  }

  // Returning user: starter prompts are the fastest path back to action.
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-5 px-6 py-8">
      <div className="text-center space-y-1">
        <h3 className="text-base font-semibold">
          {agentName
            ? t(($) => $.empty_state.returning_title_named, { name: agentName })
            : t(($) => $.empty_state.returning_title_default)}
        </h3>
        <p className="text-sm text-muted-foreground">
          {t(($) => $.empty_state.returning_subtitle)}
        </p>
      </div>
      <div className="w-full max-w-xs space-y-2">
        {STARTER_KEYS.map((key) => {
          const text = t(($) => $.starter_prompts[key]);
          return (
            <button
              key={key}
              type="button"
              onClick={() => onPickPrompt(text)}
              className="w-full rounded-lg border border-border bg-card px-3 py-2 text-left text-sm text-foreground transition-colors hover:bg-accent hover:border-brand/40"
            >
              <span className="mr-2">{STARTER_ICONS[key]}</span>
              {text}
            </button>
          );
        })}
      </div>
    </div>
  );
}
