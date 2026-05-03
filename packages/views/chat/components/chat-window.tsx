"use client";

import React, { useCallback, useEffect, useMemo, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { motion } from "motion/react";
import { Minus, Maximize2, Minimize2, ChevronDown, Plus, Check } from "lucide-react";
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
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import { canAssignAgent } from "@multica/views/issues/components";
import { api } from "@multica/core/api";
import { useAgentPresenceDetail, useWorkspaceAgentAvailability } from "@multica/core/agents";
import { ActorAvatar } from "../../common/actor-avatar";
import { OfflineBanner } from "./offline-banner";
import { NoAgentBanner } from "./no-agent-banner";
import {
  chatSessionsOptions,
  allChatSessionsOptions,
  chatMessagesOptions,
  pendingChatTaskOptions,
  pendingChatTasksOptions,
  chatKeys,
} from "@multica/core/chat/queries";
import { useCreateChatSession, useMarkChatSessionRead } from "@multica/core/chat/mutations";
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

const uiLogger = createLogger("chat.ui");
const apiLogger = createLogger("chat.api");

export function ChatWindow() {
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
  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));
  const { data: allSessions = [] } = useQuery(allChatSessionsOptions(wsId));
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

  // Check if current session is archived
  const currentSession = activeSessionId
    ? allSessions.find((s) => s.id === activeSessionId)
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

  const handleSend = useCallback(
    async (content: string) => {
      if (!activeAgent) {
        apiLogger.warn("sendChatMessage skipped: no active agent");
        return;
      }

      const focusOn = useChatStore.getState().focusMode;
      const finalContent = focusOn && anchorCandidate
        ? `${buildAnchorMarkdown(anchorCandidate)}\n\n${content}`
        : content;

      let sessionId = activeSessionId;
      const isNewSession = !sessionId;

      apiLogger.info("sendChatMessage.start", {
        sessionId,
        isNewSession,
        agentId: activeAgent.id,
        contentLength: finalContent.length,
        hasAnchor: focusOn && !!anchorCandidate,
      });

      if (!sessionId) {
        const session = await createSession.mutateAsync({
          agent_id: activeAgent.id,
          title: finalContent.slice(0, 50),
        });
        sessionId = session.id;
        setActiveSession(sessionId);
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
      apiLogger.debug("sendChatMessage.optimistic", { sessionId, optimisticId: optimistic.id });

      const result = await api.sendChatMessage(sessionId, finalContent);
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
      createSession,
      setActiveSession,
      qc,
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
      layout
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
            <TooltipContent side="top">New chat</TooltipContent>
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
              {isExpanded || isAtMax ? "Restore" : "Fullscreen"}
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
            <TooltipContent side="top">Minimize</TooltipContent>
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

      {/* Input — disabled for archived sessions; locked out entirely
       *  when there's no agent (the EmptyState above carries the CTA). */}
      <ChatInput
        onSend={handleSend}
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
    return <span className="text-xs text-muted-foreground">No agents</span>;
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
            <DropdownMenuLabel>My agents</DropdownMenuLabel>
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
            <DropdownMenuLabel>Others</DropdownMenuLabel>
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
 * Session dropdown: lists ALL sessions across agents. Each row carries the
 * owning agent's avatar so the user can tell them apart. Selecting a
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
  const wsId = useWorkspaceId();
  const agentById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);
  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const title = activeSession?.title?.trim() || "New chat";
  const triggerAgent = activeSession ? agentById.get(activeSession.agent_id) ?? null : null;

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

  return (
    <DropdownMenu>
      <DropdownMenuTrigger className="flex items-center gap-1.5 min-w-0 rounded-md px-1.5 py-1 transition-colors hover:bg-accent aria-expanded:bg-accent">
        {triggerAgent && (
          <ActorAvatar
            actorType="agent"
            actorId={triggerAgent.id}
            size={24}
            enableHoverCard
            showStatusDot
          />
        )}
        <span className="truncate text-sm font-medium">{title}</span>
        {otherSessionRunning ? (
          <span
            aria-label="Another chat is running"
            title="Another chat is running"
            className="size-1.5 shrink-0 rounded-full bg-amber-500 animate-pulse"
          />
        ) : otherSessionUnread ? (
          <span
            aria-label="Another chat has unread replies"
            title="Another chat has unread replies"
            className="size-1.5 shrink-0 rounded-full bg-brand"
          />
        ) : null}
        <ChevronDown className="size-3 text-muted-foreground shrink-0" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="max-h-80 w-auto min-w-56 max-w-80">
        {sessions.length === 0 ? (
          <div className="px-2 py-1.5 text-xs text-muted-foreground">
            No previous chats
          </div>
        ) : (
          sessions.map((session) => {
            const isCurrent = session.id === activeSessionId;
            const agent = agentById.get(session.agent_id) ?? null;
            const isRunning = inFlightSessionIds.has(session.id);
            return (
              <DropdownMenuItem
                key={session.id}
                onClick={() => onSelectSession(session)}
                className="flex min-w-0 items-center gap-2"
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
                <span className="truncate flex-1 text-sm">
                  {session.title?.trim() || "New chat"}
                </span>
                {/* Right-edge status pip: in-flight wins over unread because
                 *  "still working" is more actionable than "has reply" — and
                 *  the two rarely coexist in practice (the unread flag fires
                 *  on chat_message write, by which point the task has just
                 *  finished). Same pip shape as unread for visual rhythm,
                 *  amber + pulse to read as activity. */}
                {isRunning ? (
                  <span
                    aria-label="Running"
                    title="Running"
                    className="size-1.5 shrink-0 rounded-full bg-amber-500 animate-pulse"
                  />
                ) : session.has_unread ? (
                  <span
                    aria-label="Unread"
                    title="Unread"
                    className="size-1.5 shrink-0 rounded-full bg-brand"
                  />
                ) : null}
                {isCurrent && <Check className="size-3.5 text-muted-foreground shrink-0" />}
              </DropdownMenuItem>
            );
          })
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/**
 * Three starter prompts shown on the empty state. Tapping one sends it
 * immediately — ChatGPT-style — because the point is showing users what
 * this chat is for: operating on the workspace, not open-ended Q&A.
 */
const STARTER_PROMPTS: { icon: string; text: string }[] = [
  { icon: "📋", text: "List my open tasks by priority" },
  { icon: "📝", text: "Summarize what I did today" },
  { icon: "💡", text: "Plan what to work on next" },
];

function EmptyState({
  hasSessions,
  agentName,
  onPickPrompt,
}: {
  hasSessions: boolean;
  agentName?: string;
  onPickPrompt: (text: string) => void;
}) {
  // First-time experience: the user has never started a chat in this
  // workspace. Educate before suggesting actions — starter prompts
  // presume the user already knows what chat is for.
  //
  // Independent of agent state: missing-agent feedback lives in the
  // banner above the input, not here. That keeps this surface focused
  // on "what is chat" rather than "what's broken right now".
  if (!hasSessions) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-8">
        <div className="text-center space-y-3">
          <h3 className="text-base font-semibold">Chat with your agents</h3>
          <p className="text-sm text-muted-foreground">
            ✨ They know your workspace —{" "}
            <span className="font-medium text-foreground">
              issues, projects, skills
            </span>
            .
          </p>
          <p className="text-sm text-muted-foreground">
            Ask for a summary, plan your day, or hand off a quick task.
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
          {agentName ? `Hi, I'm ${agentName}` : "Welcome to Multica"}
        </h3>
        <p className="text-sm text-muted-foreground">Try asking</p>
      </div>
      <div className="w-full max-w-xs space-y-2">
        {STARTER_PROMPTS.map((prompt) => (
          <button
            key={prompt.text}
            type="button"
            onClick={() => onPickPrompt(prompt.text)}
            className="w-full rounded-lg border border-border bg-card px-3 py-2 text-left text-sm text-foreground transition-colors hover:bg-accent hover:border-brand/40"
          >
            <span className="mr-2">{prompt.icon}</span>
            {prompt.text}
          </button>
        ))}
      </div>
    </div>
  );
}
