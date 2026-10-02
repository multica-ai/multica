"use client";

import { useEffect, useMemo, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Archive, MessageSquare } from "lucide-react";
import type { Agent, ChatSession, MemberWithUser } from "@multica/core/types";
import {
  agentChatMessagesPageOptions,
  agentChatSessionsOptions,
} from "@multica/core/chat/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../../common/actor-avatar";
import { ChatMessageList } from "../../../chat/components/chat-message-list";
import { useT, useTimeAgo } from "../../../i18n";

// Mirrors CHAT_VIRTUOSO_INITIAL_FIRST_ITEM_INDEX in use-chat-controller.ts:
// Virtuoso's reverse infinite list needs headroom for pages prepended above.
const VIRTUOSO_INITIAL_FIRST_ITEM_INDEX = 1_000_000;

interface AgentChatsTabProps {
  agent: Agent;
  members: MemberWithUser[];
}

/**
 * Owner/admin monitoring surface on the agent detail page. Every member's
 * conversations with this agent, read-only, so the agent's owner can review
 * how it answers other people. The server gates the endpoints to the agent
 * owner or a workspace owner/admin and returns sessions newest-activity first.
 */
export function AgentChatsTab({ agent, members }: AgentChatsTabProps) {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const timeAgo = useTimeAgo();

  const [includeArchived, setIncludeArchived] = useState(false);
  const [selectedSessionId, setSelectedSessionId] = useState<string | null>(null);

  const { data: sessions = [], isLoading } = useQuery(
    agentChatSessionsOptions(wsId, agent.id, includeArchived),
  );

  // Land on the most recent conversation instead of an empty right pane. Only
  // auto-selects when nothing is chosen, so swapping the archived filter does
  // not yank a reader off the conversation they opened.
  useEffect(() => {
    if (selectedSessionId) return;
    const first = sessions[0];
    if (first) setSelectedSessionId(first.id);
  }, [sessions, selectedSessionId]);

  const membersByUserId = useMemo(
    () => new Map(members.map((member) => [member.user_id, member])),
    [members],
  );

  const selectedSession =
    sessions.find((session) => session.id === selectedSessionId) ?? null;

  return (
    <div className="flex min-h-0 flex-1 flex-col md:flex-row">
      <aside className="flex shrink-0 flex-col border-b md:w-80 md:border-b-0 md:border-r">
        <div className="flex items-center justify-between gap-2 px-4 py-3">
          <h2 className="text-body font-medium">{t(($) => $.chats.title)}</h2>
          <label className="flex cursor-pointer items-center gap-1.5 text-caption text-muted-foreground">
            <input
              type="checkbox"
              className="size-3.5 accent-foreground"
              checked={includeArchived}
              onChange={(event) => setIncludeArchived(event.target.checked)}
            />
            {t(($) => $.chats.show_archived)}
          </label>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto md:max-h-none">
          {isLoading ? (
            <div className="space-y-2 px-4 pb-4">
              <Skeleton className="h-14 w-full" />
              <Skeleton className="h-14 w-full" />
              <Skeleton className="h-14 w-full" />
            </div>
          ) : sessions.length === 0 ? (
            <div className="px-4 py-10 text-center text-caption text-muted-foreground">
              {t(($) => $.chats.empty_list)}
            </div>
          ) : (
            <ul className="pb-2">
              {sessions.map((session) => (
                <SessionRow
                  key={session.id}
                  session={session}
                  member={
                    membersByUserId.get(session.creator_id) ?? null
                  }
                  selected={session.id === selectedSessionId}
                  timeAgo={timeAgo}
                  untitledLabel={t(($) => $.chats.untitled)}
                  archivedLabel={t(($) => $.chats.archived)}
                  onSelect={() => setSelectedSessionId(session.id)}
                />
              ))}
            </ul>
          )}
        </div>
      </aside>

      <section className="flex min-h-0 flex-1 flex-col">
        {selectedSession ? (
          <ConversationTranscript
            key={selectedSession.id}
            agentId={agent.id}
            session={selectedSession}
            member={membersByUserId.get(selectedSession.creator_id) ?? null}
            timeAgo={timeAgo}
            emptyLabel={t(($) => $.chats.empty_transcript)}
          />
        ) : (
          <div className="flex flex-1 flex-col items-center justify-center gap-2 px-6 py-16 text-center">
            <MessageSquare
              className="h-7 w-7 text-muted-foreground"
              aria-hidden="true"
            />
            <p className="text-caption text-muted-foreground">
              {t(($) => $.chats.select_conversation)}
            </p>
          </div>
        )}
      </section>
    </div>
  );
}

function SessionRow({
  session,
  member,
  selected,
  timeAgo,
  untitledLabel,
  archivedLabel,
  onSelect,
}: {
  session: ChatSession;
  member: MemberWithUser | null;
  selected: boolean;
  timeAgo: (value: string) => string;
  untitledLabel: string;
  archivedLabel: string;
  onSelect: () => void;
}) {
  const preview = session.last_message
    ? session.last_message.content.replace(/\s+/g, " ").trim()
    : "";
  const activityAt = session.last_message?.created_at ?? session.updated_at;

  return (
    <li>
      <button
        type="button"
        onClick={onSelect}
        aria-current={selected ? "true" : undefined}
        className={cn(
          "flex w-full items-start gap-2.5 px-4 py-2.5 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring",
          selected
            ? "bg-surface-selected text-surface-selected-foreground"
            : "hover:bg-surface-hover",
        )}
      >
        <ActorAvatar
          actorType="member"
          actorId={session.creator_id}
          name={member?.name}
          avatarUrl={member?.avatar_url ?? undefined}
          size="sm"
          className="mt-0.5 shrink-0"
        />
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-1.5">
            <span className="truncate text-caption font-medium">
              {member?.name || member?.email || session.creator_id}
            </span>
            {session.status === "archived" && (
              <Badge variant="secondary" className="gap-1 px-1.5 py-0 text-[10px]">
                <Archive className="h-2.5 w-2.5" aria-hidden="true" />
                {archivedLabel}
              </Badge>
            )}
          </span>
          <span className="mt-0.5 block truncate text-caption text-muted-foreground">
            {session.title || preview || untitledLabel}
          </span>
          <span className="mt-0.5 block truncate text-[11px] text-faint-foreground">
            {timeAgo(activityAt)}
          </span>
        </span>
      </button>
    </li>
  );
}

function ConversationTranscript({
  agentId,
  session,
  member,
  timeAgo,
  emptyLabel,
}: {
  agentId: string;
  session: ChatSession;
  member: MemberWithUser | null;
  timeAgo: (value: string) => string;
  emptyLabel: string;
}) {
  const {
    data,
    isLoading,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
  } = useInfiniteQuery(agentChatMessagesPageOptions(agentId, session.id));

  const pages = data?.pages ?? [];
  const messages = useMemo(
    () => [...pages].reverse().flatMap((page) => page.messages),
    [pages],
  );
  const olderMessageCount = pages
    .slice(1)
    .reduce((sum, page) => sum + page.messages.length, 0);
  const firstItemIndex =
    messages.length > 0
      ? VIRTUOSO_INITIAL_FIRST_ITEM_INDEX - olderMessageCount
      : 0;

  return (
    <>
      <header className="flex shrink-0 items-center gap-2.5 border-b px-4 py-3">
        <ActorAvatar
          actorType="member"
          actorId={session.creator_id}
          name={member?.name}
          avatarUrl={member?.avatar_url ?? undefined}
          size="sm"
        />
        <div className="min-w-0">
          <div className="truncate text-body font-medium">
            {member?.name || member?.email || session.creator_id}
          </div>
          <div className="truncate text-caption text-muted-foreground">
            {session.title || timeAgo(session.last_message?.created_at ?? session.updated_at)}
          </div>
        </div>
      </header>

      {isLoading ? (
        <div className="space-y-3 p-4">
          <Skeleton className="h-16 w-2/3" />
          <Skeleton className="ml-auto h-16 w-2/3" />
          <Skeleton className="h-16 w-1/2" />
        </div>
      ) : messages.length === 0 ? (
        <div className="flex flex-1 items-center justify-center px-6 py-12 text-center text-caption text-muted-foreground">
          {emptyLabel}
        </div>
      ) : (
        <ChatMessageList
          messages={messages}
          pendingTask={null}
          availability={undefined}
          firstItemIndex={firstItemIndex}
          hasOlderMessages={!!hasNextPage}
          isFetchingOlderMessages={isFetchingNextPage}
          onLoadOlderMessages={() => void fetchNextPage()}
        />
      )}
    </>
  );
}
