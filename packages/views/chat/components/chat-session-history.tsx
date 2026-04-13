"use client";

import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, MessageSquare, Archive, Bot, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import { allChatSessionsOptions } from "@multica/core/chat/queries";
import { useArchiveChatSession } from "@multica/core/chat/mutations";
import { useChatStore } from "@multica/core/chat";
import type { ChatSession, Agent } from "@multica/core/types";

export function ChatSessionHistory() {
  const wsId = useWorkspaceId();
  const setShowHistory = useChatStore((s) => s.setShowHistory);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const clearTimeline = useChatStore((s) => s.clearTimeline);
  const setPendingTask = useChatStore((s) => s.setPendingTask);
  const activeSessionId = useChatStore((s) => s.activeSessionId);

  const { data: sessions = [] } = useQuery(allChatSessionsOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const archiveSession = useArchiveChatSession();

  const agentMap = new Map(agents.map((a) => [a.id, a]));

  const handleSelectSession = (session: ChatSession) => {
    setActiveSession(session.id);
    clearTimeline();
    setPendingTask(null);
    setShowHistory(false);
  };

  const handleArchive = (e: React.MouseEvent, sessionId: string) => {
    e.stopPropagation();
    if (activeSessionId === sessionId) {
      setActiveSession(null);
    }
    archiveSession.mutate(sessionId, {
      onError: () => toast.error("Failed to archive session"),
    });
  };

  const activeSessions = sessions.filter((s) => s.status === "active");
  const archivedSessions = sessions.filter((s) => s.status === "archived");

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
          <TooltipContent side="bottom">Back</TooltipContent>
        </Tooltip>
        <span className="text-sm font-medium">Chat History</span>
      </div>

      {/* Session list */}
      <div className="flex-1 overflow-y-auto">
        {sessions.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-2 py-12 text-muted-foreground">
            <MessageSquare className="size-6" />
            <span className="text-sm">No chat sessions yet</span>
          </div>
        ) : (
          <>
            {activeSessions.length > 0 && (
              <SessionGroup
                label="Active"
                sessions={activeSessions}
                agentMap={agentMap}
                activeSessionId={activeSessionId}
                archivingId={archiveSession.isPending ? (archiveSession.variables as string) : null}
                onSelect={handleSelectSession}
                onArchive={handleArchive}
              />
            )}
            {archivedSessions.length > 0 && (
              <SessionGroup
                label="Archived"
                sessions={archivedSessions}
                agentMap={agentMap}
                activeSessionId={activeSessionId}
                archivingId={null}
                onSelect={handleSelectSession}
              />
            )}
          </>
        )}
      </div>
    </div>
  );
}

function SessionGroup({
  label,
  sessions,
  agentMap,
  activeSessionId,
  archivingId,
  onSelect,
  onArchive,
}: {
  label: string;
  sessions: ChatSession[];
  agentMap: Map<string, Agent>;
  activeSessionId: string | null;
  archivingId: string | null;
  onSelect: (session: ChatSession) => void;
  onArchive?: (e: React.MouseEvent, sessionId: string) => void;
}) {
  return (
    <div>
      <div className="px-4 pt-3 pb-1">
        <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
          {label}
        </span>
      </div>
      {sessions.map((session) => (
        <SessionItem
          key={session.id}
          session={session}
          agent={agentMap.get(session.agent_id) ?? null}
          isActive={session.id === activeSessionId}
          isArchiving={session.id === archivingId}
          onSelect={() => onSelect(session)}
          onArchive={onArchive ? (e) => onArchive(e, session.id) : undefined}
        />
      ))}
    </div>
  );
}

function SessionItem({
  session,
  agent,
  isActive,
  isArchiving,
  onSelect,
  onArchive,
}: {
  session: ChatSession;
  agent: Agent | null;
  isActive: boolean;
  isArchiving: boolean;
  onSelect: () => void;
  onArchive?: (e: React.MouseEvent) => void;
}) {
  const timeAgo = formatTimeAgo(session.updated_at);

  return (
    <button
      onClick={onSelect}
      className={cn(
        "group flex w-full items-start gap-3 px-4 py-2.5 text-left transition-colors hover:bg-accent/50",
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
            {session.title || "Untitled"}
          </span>
          {session.status === "archived" && (
            <Archive className="size-3 shrink-0 text-muted-foreground" />
          )}
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
      {onArchive && (
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                className={cn(
                  "shrink-0 mt-0.5 text-muted-foreground",
                  !isArchiving && "invisible group-hover:visible",
                )}
                onClick={onArchive}
                disabled={isArchiving}
              />
            }
          >
            {isArchiving ? <Loader2 className="animate-spin" /> : <Archive />}
          </TooltipTrigger>
          <TooltipContent side="bottom">Archive</TooltipContent>
        </Tooltip>
      )}
    </button>
  );
}

function formatTimeAgo(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  const diffHours = Math.floor(diffMs / 3600000);
  const diffDays = Math.floor(diffMs / 86400000);

  if (diffMins < 1) return "just now";
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 7) return `${diffDays}d ago`;
  return date.toLocaleDateString();
}
