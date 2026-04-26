"use client";

import React, { useCallback, useEffect, useMemo, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Minus, Maximize2, Minimize2, ChevronDown, Bot, Plus, Check, GitBranch } from "lucide-react";
import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";
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
import { useCurrentWorkspace } from "@multica/core/paths";
import { canAssignAgent } from "@multica/views/issues/components";
import { api } from "@multica/core/api";
import {
  chatSessionsOptions,
  allChatSessionsOptions,
  chatMessagesOptions,
  pendingChatTaskOptions,
  chatKeys,
} from "@multica/core/chat/queries";
import { useCreateChatSession, useMarkChatSessionRead } from "@multica/core/chat/mutations";
import { useChatStore } from "@multica/core/chat";
import { ChatMessageList, ChatMessageSkeleton } from "./chat-message-list";
import { ChatInput } from "./chat-input";
import { ChatResizeHandles } from "./chat-resize-handles";
import { useChatResize } from "./use-chat-resize";
import { createLogger } from "@multica/core/logger";
import type { Agent, ChatMessage, ChatSession } from "@multica/core/types";

const uiLogger = createLogger("chat.ui");
const apiLogger = createLogger("chat.api");

export function ChatWindow() {
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
  const isOpen = useChatStore((s) => s.isOpen);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const selectedAgentId = useChatStore((s) => s.selectedAgentId);
  const selectedRepoUrls = useChatStore((s) => s.selectedRepoUrls);
  const setOpen = useChatStore((s) => s.setOpen);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const setSelectedAgentId = useChatStore((s) => s.setSelectedAgentId);
  const setSelectedRepoUrls = useChatStore((s) => s.setSelectedRepoUrls);
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

  // Auto-restore most recent active session from server (only once on mount)
  const didRestoreRef = useRef(false);
  useEffect(() => {
    if (didRestoreRef.current) return;
    didRestoreRef.current = true;
    if (activeSessionId || sessions.length === 0) {
      uiLogger.debug("restore session skipped", {
        reason: activeSessionId ? "already has session" : "no sessions",
        activeSessionId,
        sessionCount: sessions.length,
      });
      return;
    }
    const latest = sessions.find((s) => s.status === "active");
    if (latest) {
      uiLogger.info("restore session on mount", { sessionId: latest.id });
      setActiveSession(latest.id);
    } else {
      uiLogger.debug("restore session: no active session found");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- run once when sessions load
  }, [sessions]);

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

  const handleSend = useCallback(
    async (content: string) => {
      if (!activeAgent) {
        apiLogger.warn("sendChatMessage skipped: no active agent");
        return;
      }

      let sessionId = activeSessionId;
      const isNewSession = !sessionId;

      apiLogger.info("sendChatMessage.start", {
        sessionId,
        isNewSession,
        agentId: activeAgent.id,
        contentLength: content.length,
      });

      if (!sessionId) {
        const session = await createSession.mutateAsync({
          agent_id: activeAgent.id,
          title: content.slice(0, 50),
          selected_repo_urls: selectedRepoUrls.length > 0 ? selectedRepoUrls : undefined,
        });
        sessionId = session.id;
        setActiveSession(sessionId);
      }

      // Optimistic: show user message immediately.
      const optimistic: ChatMessage = {
        id: `optimistic-${Date.now()}`,
        chat_session_id: sessionId,
        role: "user",
        content,
        task_id: null,
        created_at: new Date().toISOString(),
      };
      qc.setQueryData<ChatMessage[]>(
        chatKeys.messages(sessionId),
        (old) => (old ? [...old, optimistic] : [optimistic]),
      );
      apiLogger.debug("sendChatMessage.optimistic", { sessionId, optimisticId: optimistic.id });

      const result = await api.sendChatMessage(sessionId, content);
      apiLogger.info("sendChatMessage.success", {
        sessionId,
        messageId: result.message_id,
        taskId: result.task_id,
      });
      // Seed pending-task optimistically so the spinner shows instantly —
      // the WS chat:message handler will invalidate + refetch to confirm.
      qc.setQueryData(chatKeys.pendingTask(sessionId), {
        task_id: result.task_id,
        status: "queued",
      });
      qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
    },
    [
      activeSessionId,
      activeAgent,
      selectedRepoUrls,
      createSession,
      setActiveSession,
      qc,
    ],
  );

  const handleStop = useCallback(async () => {
    if (!pendingTaskId) {
      apiLogger.debug("cancelTask skipped: no pending task");
      return;
    }
    apiLogger.info("cancelTask.start", { taskId: pendingTaskId, sessionId: activeSessionId });
    try {
      await api.cancelTaskById(pendingTaskId);
      apiLogger.info("cancelTask.success", { taskId: pendingTaskId });
    } catch (err) {
      // Task may already be completed
      apiLogger.warn("cancelTask.error (task may have already finished)", { taskId: pendingTaskId, err });
    }
    if (activeSessionId) {
      // Clear pending immediately; WS task:cancelled will confirm.
      qc.setQueryData(chatKeys.pendingTask(activeSessionId), {});
      qc.invalidateQueries({ queryKey: chatKeys.messages(activeSessionId) });
    }
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

  const windowRef = useRef<HTMLDivElement>(null);
  const { renderWidth, renderHeight, isAtMax, boundsReady, isDragging, toggleExpand, startDrag } = useChatResize(windowRef);

  // Show the list (vs empty state) as soon as there's anything to display —
  // a real message, or a pending task whose timeline will stream in.
  const hasMessages = messages.length > 0 || !!pendingTaskId;

  const isVisible = isOpen && boundsReady;

  const containerClass = "absolute bottom-2 right-2 z-50 flex flex-col rounded-xl ring-1 ring-foreground/10 bg-sidebar shadow-2xl overflow-hidden";
  const containerStyle: React.CSSProperties = {
    width: `${renderWidth}px`,
    height: `${renderHeight}px`,
    opacity: isVisible ? 1 : 0,
    transform: isVisible ? "scale(1)" : "scale(0.95)",
    transformOrigin: "bottom right",
    pointerEvents: isOpen ? "auto" : "none",
    transition: isDragging
      ? "none"
      : "width 200ms ease-out, height 200ms ease-out, opacity 150ms ease-out, transform 150ms ease-out",
  };

  return (
    <div ref={windowRef} className={containerClass} style={containerStyle}>
      <ChatResizeHandles onDragStart={startDrag} />
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
              {isAtMax ? <Minimize2 /> : <Maximize2 />}
            </TooltipTrigger>
            <TooltipContent side="top">
              {isAtMax ? "Restore" : "Expand"}
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
          pendingTaskId={pendingTaskId}
          isWaiting={!!pendingTaskId}
        />
      ) : (
        <EmptyState
          agentName={activeAgent?.name}
          onPickPrompt={(text) => handleSend(text)}
        />
      )}

      {/* Input — disabled for archived sessions */}
      <ChatInput
        onSend={handleSend}
        onStop={handleStop}
        isRunning={!!pendingTaskId}
        disabled={isSessionArchived}
        agentName={activeAgent?.name}
        leftAdornment={
          <div className="flex items-center gap-1">
            <AgentDropdown
              agents={availableAgents}
              activeAgent={activeAgent}
              userId={user?.id}
              onSelect={handleSelectAgent}
            />
            <RepoMultiSelectDropdown
              repos={workspace?.repos ?? []}
              selectedUrls={selectedRepoUrls}
              onChangeUrls={setSelectedRepoUrls}
            />
          </div>
        }
      />
    </div>
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
        <AgentAvatarSmall agent={activeAgent} />
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
      <AgentAvatarSmall agent={agent} />
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
  const agentById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);
  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const title = activeSession?.title?.trim() || "New chat";
  const triggerAgent = activeSession ? agentById.get(activeSession.agent_id) ?? null : null;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger className="flex items-center gap-1.5 min-w-0 rounded-md px-1.5 py-1 transition-colors hover:bg-accent aria-expanded:bg-accent">
        {triggerAgent && <AgentAvatarSmall agent={triggerAgent} />}
        <span className="truncate text-sm font-medium">{title}</span>
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
            return (
              <DropdownMenuItem
                key={session.id}
                onClick={() => onSelectSession(session)}
                className="flex min-w-0 items-center gap-2"
              >
                {agent ? (
                  <AgentAvatarSmall agent={agent} />
                ) : (
                  <span className="size-6 shrink-0" />
                )}
                <span className="truncate flex-1 text-sm">
                  {session.title?.trim() || "New chat"}
                </span>
                {session.has_unread && (
                  <span className="size-1.5 shrink-0 rounded-full bg-brand" />
                )}
                {isCurrent && <Check className="size-3.5 text-muted-foreground shrink-0" />}
              </DropdownMenuItem>
            );
          })
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function AgentAvatarSmall({ agent }: { agent: Agent }) {
  return (
    <Avatar className="size-6">
      {agent.avatar_url && <AvatarImage src={agent.avatar_url} />}
      <AvatarFallback className="bg-purple-100 text-purple-700">
        <Bot className="size-3.5" />
      </AvatarFallback>
    </Avatar>
  );
}

/**
 * Multi-select dropdown for workspace repositories.
 * Empty selection = "all repos" (default behaviour).
 */
function RepoMultiSelectDropdown({
  repos,
  selectedUrls,
  onChangeUrls,
}: {
  repos: { url?: string; description: string }[];
  selectedUrls: string[];
  onChangeUrls: (urls: string[]) => void;
}) {
  if (repos.length === 0) return null;

  const selectedCount = selectedUrls.length;

  function toggleRepo(url: string) {
    if (selectedUrls.includes(url)) {
      onChangeUrls(selectedUrls.filter((u) => u !== url));
    } else {
      onChangeUrls([...selectedUrls, url]);
    }
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger className="flex items-center gap-1 rounded-md px-1.5 py-1 cursor-pointer outline-none transition-colors hover:bg-accent aria-expanded:bg-accent">
        <GitBranch className="size-3.5 text-muted-foreground shrink-0" />
        {selectedCount > 0 ? (
          <span className="text-xs font-medium tabular-nums">{selectedCount}</span>
        ) : null}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" side="top" className="w-auto max-w-72">
        <DropdownMenuGroup>
          <DropdownMenuLabel className="text-xs">Repos (empty = all)</DropdownMenuLabel>
          <DropdownMenuSeparator />
          {repos.map((repo) => {
            const url = repo.url ?? "";
            if (!url) return null;
            const isSelected = selectedUrls.includes(url);
            const label = repo.description || url.split("/").pop() || url;
            return (
              <DropdownMenuItem
                key={url}
                closeOnClick={false}
                onClick={() => toggleRepo(url)}
                className="flex items-center gap-2"
              >
                <Check className={`size-3.5 shrink-0 ${isSelected ? "text-foreground" : "text-transparent"}`} />
                <span className="truncate flex-1 text-xs">{label}</span>
              </DropdownMenuItem>
            );
          })}
        </DropdownMenuGroup>
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
  agentName,
  onPickPrompt,
}: {
  agentName?: string;
  onPickPrompt: (text: string) => void;
}) {
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
