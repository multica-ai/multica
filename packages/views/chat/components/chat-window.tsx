"use client";

import React, { useCallback, useEffect, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Minus, Maximize2, Minimize2, Send, ChevronDown, Bot, Plus, History } from "lucide-react";
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
import { canAssignAgent } from "@multica/views/issues/components";
import { api } from "@multica/core/api";
import {
  chatSessionsOptions,
  allChatSessionsOptions,
  chatMessagesOptions,
  chatKeys,
} from "@multica/core/chat/queries";
import { useCreateChatSession } from "@multica/core/chat/mutations";
import { useChatStore } from "@multica/core/chat";
import { ChatMessageList } from "./chat-message-list";
import { ChatInput } from "./chat-input";
import { ChatSessionHistory } from "./chat-session-history";
import { ChatResizeHandles } from "./chat-resize-handles";
import { useChatResize } from "./use-chat-resize";
import { useWS } from "@multica/core/realtime";
import type { TaskMessagePayload, ChatDonePayload, Agent, ChatMessage } from "@multica/core/types";

export function ChatWindow() {
  const wsId = useWorkspaceId();
  const isOpen = useChatStore((s) => s.isOpen);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const pendingTaskId = useChatStore((s) => s.pendingTaskId);
  const timelineItems = useChatStore((s) => s.timelineItems);
  const selectedAgentId = useChatStore((s) => s.selectedAgentId);
  const setOpen = useChatStore((s) => s.setOpen);
  const showHistory = useChatStore((s) => s.showHistory);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const setPendingTask = useChatStore((s) => s.setPendingTask);
  const addTimelineItem = useChatStore((s) => s.addTimelineItem);
  const clearTimeline = useChatStore((s) => s.clearTimeline);
  const setSelectedAgentId = useChatStore((s) => s.setSelectedAgentId);
  const setShowHistory = useChatStore((s) => s.setShowHistory);
  const user = useAuthStore((s) => s.user);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));
  const { data: allSessions = [] } = useQuery(allChatSessionsOptions(wsId));
  const { data: rawMessages } = useQuery(
    chatMessagesOptions(activeSessionId ?? ""),
  );
  // When no active session, always show empty — don't use stale cache
  const messages = activeSessionId ? rawMessages ?? [] : [];

  // Check if current session is archived
  const currentSession = activeSessionId
    ? allSessions.find((s) => s.id === activeSessionId)
    : null;
  const isSessionArchived = currentSession?.status === "archived";

  const qc = useQueryClient();
  const createSession = useCreateChatSession();

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

  // Auto-restore most recent active session from server (only once on mount)
  const didRestoreRef = useRef(false);
  useEffect(() => {
    if (didRestoreRef.current) return;
    didRestoreRef.current = true;
    if (activeSessionId || sessions.length === 0) return;
    const latest = sessions.find((s) => s.status === "active");
    if (latest) {
      setActiveSession(latest.id);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- run once when sessions load
  }, [sessions]);

  // Use ref for pendingTaskId so WS handlers always see the latest value
  // without needing to re-subscribe on every change.
  const pendingTaskRef = useRef<string | null>(pendingTaskId);
  pendingTaskRef.current = pendingTaskId;

  const { subscribe } = useWS();

  useEffect(() => {
    // Returns true if the event was for our pending task and was handled.
    // Caller still decides whether to invalidate cache (chat:done / completed do; failed doesn't).
    const matchesPending = (taskId: string) =>
      !!pendingTaskRef.current && taskId === pendingTaskRef.current;

    const finalizePending = (invalidateCache: boolean) => {
      if (invalidateCache) {
        const sid = useChatStore.getState().activeSessionId;
        if (sid) {
          qc.invalidateQueries({ queryKey: chatKeys.messages(sid) });
        }
      }
      clearTimeline();
      setPendingTask(null);
    };

    const unsubMessage = subscribe("task:message", (payload) => {
      const p = payload as TaskMessagePayload;
      if (!matchesPending(p.task_id)) return;
      addTimelineItem({
        seq: p.seq,
        type: p.type,
        tool: p.tool,
        content: p.content,
        input: p.input,
        output: p.output,
      });
    });

    const unsubDone = subscribe("chat:done", (payload) => {
      const p = payload as ChatDonePayload;
      if (!matchesPending(p.task_id)) return;
      finalizePending(true);
    });

    const unsubCompleted = subscribe("task:completed", (payload) => {
      const p = payload as { task_id: string };
      if (!matchesPending(p.task_id)) return;
      finalizePending(true);
    });

    const unsubFailed = subscribe("task:failed", (payload) => {
      const p = payload as { task_id: string };
      if (!matchesPending(p.task_id)) return;
      finalizePending(false);
    });

    return () => {
      unsubMessage();
      unsubDone();
      unsubCompleted();
      unsubFailed();
    };
  }, [subscribe, addTimelineItem, clearTimeline, setPendingTask, qc]);

  const handleSend = useCallback(
    async (content: string) => {
      if (!activeAgent) return;

      let sessionId = activeSessionId;

      if (!sessionId) {
        const session = await createSession.mutateAsync({
          agent_id: activeAgent.id,
          title: content.slice(0, 50),
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

      const result = await api.sendChatMessage(sessionId, content);
      setPendingTask(result.task_id);
      qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
    },
    [
      activeSessionId,
      activeAgent,
      createSession,
      setActiveSession,
      setPendingTask,
      qc,
    ],
  );

  const handleStop = useCallback(async () => {
    if (!pendingTaskId) return;
    try {
      await api.cancelTaskById(pendingTaskId);
    } catch {
      // Task may already be completed
    }
    if (activeSessionId) {
      qc.invalidateQueries({ queryKey: chatKeys.messages(activeSessionId) });
    }
    clearTimeline();
    setPendingTask(null);
  }, [pendingTaskId, activeSessionId, clearTimeline, setPendingTask, qc]);

  const handleSelectAgent = useCallback(
    (agent: Agent) => {
      setSelectedAgentId(agent.id);
      // Reset session when switching agent
      setActiveSession(null);
    },
    [setSelectedAgentId, setActiveSession],
  );

  const windowRef = useRef<HTMLDivElement>(null);
  const { renderWidth, renderHeight, isAtMax, boundsReady, isDragging, toggleExpand, startDrag } = useChatResize(windowRef);

  const hasMessages = messages.length > 0 || timelineItems.length > 0;

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
      {/* Header */}
      {!showHistory && (
        <div className="flex items-center justify-between border-b px-4 py-2.5">
          <AgentSelector
            agents={availableAgents}
            activeAgent={activeAgent}
            userId={user?.id}
            onSelect={handleSelectAgent}
          />
          <div className="flex items-center gap-0.5">
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="text-muted-foreground"
                    onClick={() => setShowHistory(true)}
                  />
                }
              >
                <History />
              </TooltipTrigger>
              <TooltipContent side="bottom">Chat history</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="text-muted-foreground"
                    onClick={() => {
                      setActiveSession(null);
                      clearTimeline();
                      setPendingTask(null);
                    }}
                  />
                }
              >
                <Plus />
              </TooltipTrigger>
              <TooltipContent side="bottom">New chat</TooltipContent>
            </Tooltip>
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
              <TooltipContent side="bottom">
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
                    onClick={() => setOpen(false)}
                  />
                }
              >
                <Minus />
              </TooltipTrigger>
              <TooltipContent side="bottom">Minimize</TooltipContent>
            </Tooltip>
          </div>
        </div>
      )}

      {showHistory ? (
        <ChatSessionHistory />
      ) : (
        <>
          {/* Messages or Empty State */}
          {hasMessages ? (
            <ChatMessageList
              messages={messages}
              timelineItems={timelineItems}
              isWaiting={!!pendingTaskId}
            />
          ) : (
            <EmptyState agentName={activeAgent?.name} />
          )}

          {/* Input — disabled for archived sessions */}
          <ChatInput
            onSend={handleSend}
            onStop={handleStop}
            isRunning={!!pendingTaskId}
            disabled={isSessionArchived}
          />
        </>
      )}
    </div>
  );
}

function AgentSelector({
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
  if (!activeAgent) {
    return <span className="text-sm text-muted-foreground">No agents</span>;
  }

  if (agents.length <= 1) {
    return (
      <div className="flex items-center gap-2">
        <AgentAvatarSmall agent={activeAgent} />
        <span className="text-sm font-medium">{activeAgent.name}</span>
      </div>
    );
  }

  const myAgents = agents.filter((a) => a.owner_id === userId);
  const othersAgents = agents.filter((a) => a.owner_id !== userId);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger className="flex items-center gap-2 rounded-md px-1.5 py-1 -ml-1.5 transition-colors hover:bg-accent aria-expanded:bg-accent">
        <AgentAvatarSmall agent={activeAgent} />
        <span className="text-sm font-medium">{activeAgent.name}</span>
        <ChevronDown className="size-3 text-muted-foreground" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="max-h-60 w-auto max-w-56">
        {myAgents.length > 0 && (
          <DropdownMenuGroup>
            <DropdownMenuLabel>My Agents</DropdownMenuLabel>
            {myAgents.map((agent) => (
              <DropdownMenuItem
                key={agent.id}
                onClick={() => onSelect(agent)}
                className="flex min-w-0 items-center gap-2"
              >
                <AgentAvatarSmall agent={agent} />
                <span className="truncate">{agent.name}</span>
              </DropdownMenuItem>
            ))}
          </DropdownMenuGroup>
        )}
        {myAgents.length > 0 && othersAgents.length > 0 && <DropdownMenuSeparator />}
        {othersAgents.length > 0 && (
          <DropdownMenuGroup>
            <DropdownMenuLabel>Others</DropdownMenuLabel>
            {othersAgents.map((agent) => (
              <DropdownMenuItem
                key={agent.id}
                onClick={() => onSelect(agent)}
                className="flex min-w-0 items-center gap-2"
              >
                <AgentAvatarSmall agent={agent} />
                <span className="truncate">{agent.name}</span>
              </DropdownMenuItem>
            ))}
          </DropdownMenuGroup>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function AgentAvatarSmall({ agent }: { agent: Agent }) {
  return (
    <Avatar className="size-5">
      {agent.avatar_url && <AvatarImage src={agent.avatar_url} />}
      <AvatarFallback className="bg-purple-100 text-purple-700">
        <Bot className="size-3" />
      </AvatarFallback>
    </Avatar>
  );
}

function EmptyState({ agentName }: { agentName?: string }) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-4 px-8">
      <Send className="size-8 text-muted-foreground/50" />
      <div className="text-center">
        <h3 className="text-base font-semibold">Welcome to Multica</h3>
        <p className="mt-1 text-sm text-muted-foreground">
          {agentName
            ? `Chat with ${agentName} or ask anything`
            : "Ask anything or tell Multica what you need"}
        </p>
      </div>
    </div>
  );
}
