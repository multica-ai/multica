"use client";

import React, { useCallback, useEffect, useMemo, useRef } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { History, Plus, Bot, ChevronDown, Check } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
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
  pendingChatTaskOptions,
  chatKeys,
} from "@multica/core/chat/queries";
import { useCreateChatSession, useMarkChatSessionRead } from "@multica/core/chat/mutations";
import { useChatStore } from "@multica/core/chat";
import { PageHeader } from "../../layout/page-header";
import { ChatMessageList, ChatMessageSkeleton } from "./chat-message-list";
import { ChatInput } from "./chat-input";
import {
  ContextAnchorButton,
  ContextAnchorCard,
  buildAnchorMarkdown,
  useRouteAnchorCandidate,
} from "./context-anchor";
import { createLogger } from "@multica/core/logger";
import type { Agent, ChatMessage, ChatSession } from "@multica/core/types";
import { useChatT, type ChatDict } from "../i18n";

const uiLogger = createLogger("chat.ui");
const apiLogger = createLogger("chat.api");

export function ChatPage() {
  const t = useChatT();
  const wsId = useWorkspaceId();
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const selectedAgentId = useChatStore((s) => s.selectedAgentId);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const setSelectedAgentId = useChatStore((s) => s.setSelectedAgentId);
  const user = useAuthStore((s) => s.user);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: sessions = [], isSuccess: sessionsLoaded } = useQuery(
    chatSessionsOptions(wsId),
  );
  const { data: allSessions = [] } = useQuery(allChatSessionsOptions(wsId));
  const { data: rawMessages, isLoading: messagesLoading } = useQuery(
    chatMessagesOptions(activeSessionId ?? ""),
  );
  const messages = activeSessionId ? rawMessages ?? [] : [];
  const showSkeleton = !!activeSessionId && messagesLoading;

  const { data: pendingTask } = useQuery(
    pendingChatTaskOptions(activeSessionId ?? ""),
  );
  const pendingTaskId = pendingTask?.task_id ?? null;

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
  const activeAgent =
    availableAgents.find((a) => a.id === selectedAgentId) ??
    availableAgents[0] ??
    null;

  // Restore most recent active session once the session query resolves.
  // The ref is set only AFTER we've seen a successful query — setting it
  // unconditionally on first render would lose the restore whenever the
  // page mounts before the query returns (cold-start / direct navigate).
  const didRestoreRef = useRef(false);
  useEffect(() => {
    if (didRestoreRef.current) return;
    if (!sessionsLoaded) return;
    didRestoreRef.current = true;
    if (activeSessionId) return;
    const latest = sessions.find((s) => s.status === "active");
    if (latest) setActiveSession(latest.id);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- run once when sessions load
  }, [sessionsLoaded, sessions]);

  // Auto mark-as-read whenever the viewer is on a session with unread.
  const currentHasUnread =
    sessions.find((s) => s.id === activeSessionId)?.has_unread ?? false;
  useEffect(() => {
    if (!activeSessionId || !currentHasUnread) return;
    markRead.mutate(activeSessionId);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- markRead ref stable
  }, [activeSessionId, currentHasUnread]);

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
      });

      if (!sessionId) {
        const session = await createSession.mutateAsync({
          agent_id: activeAgent.id,
          title: finalContent.slice(0, 50),
        });
        sessionId = session.id;
        setActiveSession(sessionId);
      }

      const optimistic: ChatMessage = {
        id: `optimistic-${Date.now()}`,
        chat_session_id: sessionId,
        role: "user",
        content: finalContent,
        task_id: null,
        created_at: new Date().toISOString(),
      };
      qc.setQueryData<ChatMessage[]>(
        chatKeys.messages(sessionId),
        (old) => (old ? [...old, optimistic] : [optimistic]),
      );

      const result = await api.sendChatMessage(sessionId, finalContent);
      qc.setQueryData(chatKeys.pendingTask(sessionId), {
        task_id: result.task_id,
        status: "queued",
      });
      qc.invalidateQueries({ queryKey: chatKeys.messages(sessionId) });
    },
    [activeSessionId, activeAgent, anchorCandidate, createSession, setActiveSession, qc],
  );

  const handleStop = useCallback(async () => {
    if (!pendingTaskId) return;
    try {
      await api.cancelTaskById(pendingTaskId);
    } catch (err) {
      apiLogger.warn("cancelTask.error", { taskId: pendingTaskId, err });
    }
    if (activeSessionId) {
      qc.setQueryData(chatKeys.pendingTask(activeSessionId), {});
      qc.invalidateQueries({ queryKey: chatKeys.messages(activeSessionId) });
    }
  }, [pendingTaskId, activeSessionId, qc]);

  const handleSelectAgent = useCallback(
    (agent: Agent) => {
      if (activeAgent && agent.id === activeAgent.id) return;
      uiLogger.info("selectAgent", { from: selectedAgentId, to: agent.id });
      setSelectedAgentId(agent.id);
      setActiveSession(null);
    },
    [activeAgent, selectedAgentId, setSelectedAgentId, setActiveSession],
  );

  const handleNewChat = useCallback(() => {
    setActiveSession(null);
  }, [setActiveSession]);

  const handleSelectSession = useCallback(
    (session: ChatSession) => {
      if (activeAgent && session.agent_id !== activeAgent.id) {
        setSelectedAgentId(session.agent_id);
      }
      setActiveSession(session.id);
    },
    [activeAgent, setSelectedAgentId, setActiveSession],
  );

  const hasMessages = messages.length > 0 || !!pendingTaskId;
  const activeTitle = currentSession?.title?.trim() || t.page.activeTitleFallback;

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col">
      <PageHeader className="gap-2">
        <span className="text-sm font-medium truncate">{activeTitle}</span>
        <div className="ml-auto flex items-center gap-1">
          <HistoryPopover
            sessions={allSessions}
            agents={agents}
            activeSessionId={activeSessionId}
            onSelectSession={handleSelectSession}
            t={t}
          />
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted-foreground"
                  onClick={handleNewChat}
                  aria-label={t.page.newChatAria}
                />
              }
            >
              <Plus />
            </TooltipTrigger>
            <TooltipContent side="bottom">{t.page.newChat}</TooltipContent>
          </Tooltip>
        </div>
      </PageHeader>

      {/* Body — centered max-width column */}
      <div className="relative flex min-h-0 flex-1 flex-col">
        {showSkeleton ? (
          <div className="mx-auto flex w-full max-w-3xl flex-1 flex-col">
            <ChatMessageSkeleton />
          </div>
        ) : hasMessages ? (
          <div className="mx-auto flex w-full max-w-3xl flex-1 flex-col min-h-0">
            <ChatMessageList
              messages={messages}
              pendingTaskId={pendingTaskId}
              isWaiting={!!pendingTaskId}
            />
          </div>
        ) : (
          <EmptyState agentName={activeAgent?.name} onPickPrompt={handleSend} t={t} />
        )}

        <div className="mx-auto w-full max-w-3xl pb-4">
          <ChatInput
            onSend={handleSend}
            onStop={handleStop}
            isRunning={!!pendingTaskId}
            disabled={isSessionArchived}
            agentName={activeAgent?.name}
            topSlot={<ContextAnchorCard />}
            leftAdornment={
              <AgentDropdown
                agents={availableAgents}
                activeAgent={activeAgent}
                userId={user?.id}
                onSelect={handleSelectAgent}
                t={t}
              />
            }
            rightAdornment={<ContextAnchorButton />}
          />
        </div>
      </div>
    </div>
  );
}

/**
 * Popover-based history list. Per product direction, session history lives
 * inside the Chat tab — not in the global sidebar — so that Multica doesn't
 * read as "just another chat app." The trigger is a History icon in the
 * page header.
 */
function HistoryPopover({
  sessions,
  agents,
  activeSessionId,
  onSelectSession,
  t,
}: {
  sessions: ChatSession[];
  agents: Agent[];
  activeSessionId: string | null;
  onSelectSession: (session: ChatSession) => void;
  t: ChatDict;
}) {
  const [open, setOpen] = React.useState(false);
  const agentById = useMemo(() => new Map(agents.map((a) => [a.id, a])), [agents]);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Tooltip>
        <TooltipTrigger
          render={
            <PopoverTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="text-muted-foreground"
                  aria-label={t.page.historyAria}
                />
              }
            />
          }
        >
          <History />
        </TooltipTrigger>
        <TooltipContent side="bottom">{t.page.history}</TooltipContent>
      </Tooltip>
      <PopoverContent align="end" sideOffset={6} className="w-80 p-0">
        <div className="px-3 py-2 border-b">
          <span className="text-xs font-medium text-muted-foreground">{t.page.history}</span>
        </div>
        <div className="max-h-96 overflow-y-auto">
          {sessions.length === 0 ? (
            <div className="px-3 py-6 text-center text-xs text-muted-foreground">
              {t.page.noPreviousChats}
            </div>
          ) : (
            sessions.map((session) => {
              const isCurrent = session.id === activeSessionId;
              const agent = agentById.get(session.agent_id) ?? null;
              return (
                <button
                  key={session.id}
                  type="button"
                  onClick={() => {
                    onSelectSession(session);
                    setOpen(false);
                  }}
                  className={cn(
                    "flex w-full items-start gap-2 px-3 py-2 text-left transition-colors hover:bg-accent/60",
                    isCurrent && "bg-accent/40",
                  )}
                >
                  <AgentAvatarSmall agent={agent} />
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm">
                      {session.title?.trim() || t.page.activeTitleFallback}
                    </div>
                    <div className="truncate text-xs text-muted-foreground">
                      {agent?.name ?? t.page.unknownAgent}
                    </div>
                  </div>
                  {session.has_unread && (
                    <span className="mt-1.5 size-1.5 shrink-0 rounded-full bg-brand" />
                  )}
                  {isCurrent && (
                    <Check className="mt-1 size-3.5 shrink-0 text-muted-foreground" />
                  )}
                </button>
              );
            })
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function AgentDropdown({
  agents,
  activeAgent,
  userId,
  onSelect,
  t,
}: {
  agents: Agent[];
  activeAgent: Agent | null;
  userId: string | undefined;
  onSelect: (agent: Agent) => void;
  t: ChatDict;
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
    return <span className="text-xs text-muted-foreground">{t.agentPicker.noAgents}</span>;
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
            <DropdownMenuLabel>{t.agentPicker.myAgents}</DropdownMenuLabel>
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
            <DropdownMenuLabel>{t.agentPicker.others}</DropdownMenuLabel>
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

function AgentAvatarSmall({ agent }: { agent: Agent | null }) {
  return (
    <Avatar className="size-6 shrink-0">
      {agent?.avatar_url && <AvatarImage src={agent.avatar_url} />}
      <AvatarFallback className="bg-purple-100 text-purple-700">
        <Bot className="size-3.5" />
      </AvatarFallback>
    </Avatar>
  );
}

const STARTER_PROMPT_ICONS = ["📋", "📝", "💡"] as const;

function EmptyState({
  agentName,
  onPickPrompt,
  t,
}: {
  agentName?: string;
  onPickPrompt: (text: string) => void;
  t: ChatDict;
}) {
  const starterPrompts: { icon: string; text: string }[] = [
    { icon: STARTER_PROMPT_ICONS[0], text: t.emptyState.starterListTasks },
    { icon: STARTER_PROMPT_ICONS[1], text: t.emptyState.starterSummarize },
    { icon: STARTER_PROMPT_ICONS[2], text: t.emptyState.starterPlanNext },
  ];
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-6 px-6 py-12">
      <div className="text-center space-y-1">
        <h3 className="text-xl font-semibold">
          {agentName ? t.emptyState.greeting(agentName) : t.emptyState.welcome}
        </h3>
        <p className="text-sm text-muted-foreground">{t.emptyState.helpPrompt}</p>
      </div>
      <div className="w-full max-w-md space-y-2">
        {starterPrompts.map((prompt) => (
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
