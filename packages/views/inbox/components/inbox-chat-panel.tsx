"use client";

import { useCallback, useMemo, useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import {
  chatMessagesOptions,
  pendingChatTaskOptions,
  chatKeys,
  allChatSessionsOptions,
} from "@multica/core/chat/queries";
import { useCreateChatSession, useMarkChatSessionRead } from "@multica/core/chat/mutations";
import { useChatStore } from "@multica/core/chat";
import { api } from "@multica/core/api";
import { canAssignAgent } from "../../issues/components";
import { ChatMessageList, ChatMessageSkeleton } from "../../chat/components/chat-message-list";
import { ChatStatusLine } from "../../chat/components/chat-status-line";
import { ChatInput } from "../../chat/components/chat-input";
import type { Agent, ChatMessage } from "@multica/core/types";
import { Bot, ChevronDown, Check, MessageSquare } from "lucide-react";
import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";

/**
 * Chat panel for the inbox detail area. Renders a full chat conversation
 * for the given session, reusing the existing ChatMessageList and ChatInput.
 *
 * When sessionId is null, shows a "new chat" state with agent picker.
 */
export function InboxChatPanel({
  sessionId,
  onSessionCreated,
}: {
  sessionId: string | null;
  onSessionCreated?: (id: string) => void;
}) {
  const wsId = useWorkspaceId();
  const user = useAuthStore((s) => s.user);
  const qc = useQueryClient();

  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: allSessions = [] } = useQuery(allChatSessionsOptions(wsId));
  const createSession = useCreateChatSession();
  const markRead = useMarkChatSessionRead();

  // Agent selection — stored in chat store so it persists
  const selectedAgentId = useChatStore((s) => s.selectedAgentId);
  const setSelectedAgentId = useChatStore((s) => s.setSelectedAgentId);

  const currentMember = members.find((m) => m.user_id === user?.id);
  const memberRole = currentMember?.role;
  const availableAgents = agents.filter(
    (a) => !a.archived_at && canAssignAgent(a, user?.id, memberRole),
  );
  const activeAgent =
    availableAgents.find((a) => a.id === selectedAgentId) ??
    availableAgents[0] ??
    null;

  // Messages & pending task for the active session
  const { data: rawMessages, isLoading: messagesLoading } = useQuery(
    chatMessagesOptions(sessionId ?? ""),
  );
  const messages = sessionId ? rawMessages ?? [] : [];
  const showSkeleton = !!sessionId && messagesLoading;

  const { data: pendingTask } = useQuery(
    pendingChatTaskOptions(sessionId ?? ""),
  );
  const pendingTaskId = pendingTask?.task_id ?? null;

  const currentSession = sessionId
    ? allSessions.find((s) => s.id === sessionId)
    : null;
  const isSessionArchived = currentSession?.status === "archived";

  // Auto mark-as-read
  const currentHasUnread =
    allSessions.find((s) => s.id === sessionId)?.has_unread ?? false;
  useEffect(() => {
    if (!sessionId || !currentHasUnread) return;
    markRead.mutate(sessionId);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId, currentHasUnread]);

  const handleSend = useCallback(
    async (content: string) => {
      if (!activeAgent) return;

      let sid = sessionId;

      if (!sid) {
        const session = await createSession.mutateAsync({
          agent_id: activeAgent.id,
          title: content.slice(0, 50),
        });
        sid = session.id;
        onSessionCreated?.(sid);
      }

      // Optimistic user message
      const optimistic: ChatMessage = {
        id: `optimistic-${Date.now()}`,
        chat_session_id: sid,
        role: "user",
        content,
        task_id: null,
        created_at: new Date().toISOString(),
      };
      qc.setQueryData<ChatMessage[]>(
        chatKeys.messages(sid),
        (old) => (old ? [...old, optimistic] : [optimistic]),
      );

      const result = await api.sendChatMessage(sid, content);
      qc.setQueryData(chatKeys.pendingTask(sid), {
        task_id: result.task_id,
        status: "queued",
      });
      qc.invalidateQueries({ queryKey: chatKeys.messages(sid) });
    },
    [sessionId, activeAgent, createSession, onSessionCreated, qc],
  );

  const handleStop = useCallback(async () => {
    if (!pendingTaskId) return;
    try {
      await api.cancelTaskById(pendingTaskId);
    } catch {
      // Task may already be completed
    }
    if (sessionId) {
      qc.setQueryData(chatKeys.pendingTask(sessionId), {});
      qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
    }
  }, [pendingTaskId, sessionId, qc]);

  const handleSelectAgent = useCallback(
    (agent: Agent) => {
      if (activeAgent && agent.id === activeAgent.id) return;
      setSelectedAgentId(agent.id);
    },
    [activeAgent, setSelectedAgentId],
  );

  const hasMessages = messages.length > 0 || !!pendingTaskId;

  // Hide the floating chat bubble while this panel is mounted
  const setOpen = useChatStore((s) => s.setOpen);
  const setHideFloatingChat = useChatStore((s) => s.setHideFloatingChat);
  useEffect(() => {
    setHideFloatingChat(true);
    if (useChatStore.getState().isOpen) setOpen(false);
    return () => setHideFloatingChat(false);
  }, [setOpen, setHideFloatingChat]);

  return (
    <div className="flex flex-col h-full overflow-hidden">
      {/* Messages — scrollable */}
      <div className="flex flex-col flex-1 min-h-0">
        {showSkeleton ? (
          <ChatMessageSkeleton />
        ) : hasMessages ? (
          <ChatMessageList
            messages={messages}
            pendingTaskId={pendingTaskId}
            isWaiting={!!pendingTaskId}
          />
        ) : (
          <div className="flex h-full flex-col items-center justify-center gap-3 text-muted-foreground">
            <MessageSquare className="size-10 text-muted-foreground/30" />
            <p className="text-sm">
              {activeAgent
                ? `Start a conversation with ${activeAgent.name}`
                : "Select an agent to start chatting"}
            </p>
          </div>
        )}
      </div>

      {/* Input — fixed at bottom */}
      <div className="shrink-0 border-t pt-2">
      <ChatStatusLine pendingTaskId={pendingTaskId} />
      <ChatInput
        onSend={handleSend}
        onStop={handleStop}
        isRunning={!!pendingTaskId}
        disabled={isSessionArchived}
        agentName={activeAgent?.name}
        leftAdornment={
          <AgentPicker
            agents={availableAgents}
            activeAgent={activeAgent}
            userId={user?.id}
            onSelect={handleSelectAgent}
          />
        }
      />
      </div>
    </div>
  );
}

// ─── Agent picker (reused from chat-window pattern) ──────────────────────

function AgentPicker({
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
        <Avatar className="size-5">
          {activeAgent.avatar_url && <AvatarImage src={activeAgent.avatar_url} />}
          <AvatarFallback className="bg-purple-100 text-purple-700 text-[10px]">
            <Bot className="size-3" />
          </AvatarFallback>
        </Avatar>
        <span className="text-xs font-medium max-w-28 truncate">{activeAgent.name}</span>
        <ChevronDown className="size-3 text-muted-foreground shrink-0" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" side="top" className="max-h-80 w-auto max-w-64">
        {mine.length > 0 && (
          <DropdownMenuGroup>
            <DropdownMenuLabel>My agents</DropdownMenuLabel>
            {mine.map((a) => (
              <AgentMenuItem key={a.id} agent={a} isActive={a.id === activeAgent.id} onSelect={onSelect} />
            ))}
          </DropdownMenuGroup>
        )}
        {mine.length > 0 && others.length > 0 && <DropdownMenuSeparator />}
        {others.length > 0 && (
          <DropdownMenuGroup>
            <DropdownMenuLabel>Others</DropdownMenuLabel>
            {others.map((a) => (
              <AgentMenuItem key={a.id} agent={a} isActive={a.id === activeAgent.id} onSelect={onSelect} />
            ))}
          </DropdownMenuGroup>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function AgentMenuItem({
  agent,
  isActive,
  onSelect,
}: {
  agent: Agent;
  isActive: boolean;
  onSelect: (a: Agent) => void;
}) {
  return (
    <DropdownMenuItem onClick={() => onSelect(agent)}>
      <Avatar className="size-5">
        {agent.avatar_url && <AvatarImage src={agent.avatar_url} />}
        <AvatarFallback className="bg-purple-100 text-purple-700 text-[10px]">
          <Bot className="size-3" />
        </AvatarFallback>
      </Avatar>
      <span className="truncate">{agent.name}</span>
      {isActive && <Check className="ml-auto size-3.5 text-primary" />}
    </DropdownMenuItem>
  );
}
