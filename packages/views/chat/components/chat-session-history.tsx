"use client";

import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, MessageSquare, Bot } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import { allChatSessionsOptions } from "@multica/core/chat/queries";
import { useChatStore } from "@multica/core/chat";
import { createLogger } from "@multica/core/logger";
import type { ChatSession, Agent } from "@multica/core/types";
import { useT } from "../../i18n";

const logger = createLogger("chat.ui");

export function ChatSessionHistory() {
  const { t } = useT("chat");
  const wsId = useWorkspaceId();
  const setShowHistory = useChatStore((s) => s.setShowHistory);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const activeSessionId = useChatStore((s) => s.activeSessionId);

  const { data: sessions = [] } = useQuery(allChatSessionsOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));

  const agentMap = new Map(agents.map((a) => [a.id, a]));

  const handleSelectSession = (session: ChatSession) => {
    logger.info("selectSession", {
      from: activeSessionId,
      to: session.id,
      agentId: session.agent_id,
      status: session.status,
    });
    // Changing activeSessionId flips the query keys for messages +
    // pending-task; no manual clear needed.
    setActiveSession(session.id);
    setShowHistory(false);
  };

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      {/* Header */}
      <div className="flex items-center gap-2 border-b px-4 py-2.5">
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-muted-foreground"
                onClick={() => setShowHistory(false)}
              />
            }
          >
            <ArrowLeft />
          </TooltipTrigger>
          <TooltipContent side="bottom">{t(($) => $.session_history.back_tooltip)}</TooltipContent>
        </Tooltip>
        <span className="text-sm font-medium">{t(($) => $.session_history.header)}</span>
      </div>

      {/* Session list */}
      <div className="flex-1 overflow-y-auto">
        {sessions.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-12 text-muted-foreground">
            <MessageSquare className="size-6" />
            <span className="text-sm">{t(($) => $.session_history.empty)}</span>
          </div>
        ) : (
          <div>
            {sessions.map((session) => (
              <SessionItem
                key={session.id}
                session={session}
                agent={agentMap.get(session.agent_id) ?? null}
                isActive={session.id === activeSessionId}
                onSelect={() => handleSelectSession(session)}
              />
            ))}
          </div>
        )}
      </div>
    </div>
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

function SessionItem({
  session,
  agent,
  isActive,
  onSelect,
}: {
  session: ChatSession;
  agent: Agent | null;
  isActive: boolean;
  onSelect: () => void;
}) {
  const { t } = useT("chat");
  const formatTimeAgo = useFormatTimeAgo();
  const timeAgo = formatTimeAgo(session.updated_at);

  return (
    <button
      onClick={onSelect}
      className={cn(
        "flex w-full items-start gap-3 px-4 py-2.5 text-left transition-colors hover:bg-accent/50",
        isActive && "bg-accent/30",
      )}
    >
      <Avatar className="size-6 shrink-0 mt-0.5">
        {agent?.avatar_url && <AvatarImage src={agent.avatar_url} />}
        <AvatarFallback className="bg-purple-100 text-purple-700">
          <Bot className="size-3" />
        </AvatarFallback>
      </Avatar>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">
            {session.title || t(($) => $.session_history.untitled)}
          </span>
        </div>
        <div className="flex items-center gap-1.5 mt-0.5">
          {agent && (
            <span className="text-xs text-muted-foreground truncate">
              {agent.name}
            </span>
          )}
          <span className="text-xs text-muted-foreground/60">{timeAgo}</span>
        </div>
      </div>
    </button>
  );
}
