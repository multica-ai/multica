"use client";

import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";
import { Bot, MessageSquareReply } from "lucide-react";
import { ReadonlyContent } from "../../editor";
import { Button } from "@multica/ui/components/ui/button";
import type { ChannelMessage, MemberWithUser, Agent } from "@multica/core/types";
import { MessageReactions } from "./message-reactions";

interface MessageRowProps {
  message: ChannelMessage;
  channelId: string;
  member?: MemberWithUser;
  agent?: Agent;
  /** When set, clicking opens a thread side panel for this message. */
  onOpenThread?: (parentMessageId: string) => void;
  /** Inside a thread panel, "reply in thread" is meaningless on the
   * replies themselves — pass true to suppress the action. */
  disableReplyAction?: boolean;
}

function authorName(props: MessageRowProps): string {
  if (props.message.author_type === "agent") {
    return props.agent?.name ?? "Unknown agent";
  }
  return props.member?.name ?? "Unknown member";
}

function authorInitial(name: string): string {
  return name.charAt(0).toUpperCase() || "?";
}

function formatTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
}

/**
 * MessageRow renders a single channel message: avatar, author label, time,
 * markdown body, reaction chips, and (when applicable) a thread-reply
 * count badge. Soft-deleted messages render a placeholder; the row is
 * preserved in the timeline so thread continuity isn't visually broken.
 *
 * Hover affordances: the "reply in thread" + "add reaction" buttons
 * fade in on hover (group-hover) so the timeline stays clean at rest.
 */
export function MessageRow(props: MessageRowProps) {
  const { message, channelId, onOpenThread, disableReplyAction } = props;
  const isAgent = message.author_type === "agent";
  const name = authorName(props);

  if (message.deleted_at) {
    return (
      <div className="px-4 py-2 text-sm italic text-muted-foreground">
        [message deleted]
      </div>
    );
  }

  return (
    <div className="group flex gap-3 px-4 py-2 hover:bg-muted/40 transition-colors">
      <Avatar className="h-8 w-8 shrink-0">
        {!isAgent && props.member?.avatar_url ? (
          <AvatarImage src={props.member.avatar_url} alt={name} />
        ) : null}
        <AvatarFallback className={isAgent ? "bg-purple-100 text-purple-900" : ""}>
          {isAgent ? <Bot className="h-4 w-4" /> : authorInitial(name)}
        </AvatarFallback>
      </Avatar>
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <span className="text-sm font-medium text-foreground">{name}</span>
          {isAgent ? (
            <span className="text-xs text-muted-foreground">agent</span>
          ) : null}
          <span className="text-xs text-muted-foreground">{formatTime(message.created_at)}</span>
          {message.edited_at ? (
            <span className="text-xs text-muted-foreground">(edited)</span>
          ) : null}
          <span className="ml-auto flex items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
            {!disableReplyAction && onOpenThread ? (
              <Button
                size="sm"
                variant="ghost"
                className="h-6 px-2 text-xs text-muted-foreground"
                onClick={() => onOpenThread(message.id)}
                aria-label="Reply in thread"
              >
                <MessageSquareReply className="mr-1 h-3 w-3" />
                Reply
              </Button>
            ) : null}
          </span>
        </div>
        <div className="prose prose-sm max-w-none text-foreground">
          <ReadonlyContent content={message.content} />
        </div>
        <MessageReactions
          channelId={channelId}
          messageId={message.id}
          reactions={message.reactions}
        />
        {!disableReplyAction && message.thread_reply_count > 0 && onOpenThread ? (
          <button
            type="button"
            onClick={() => onOpenThread(message.id)}
            className="mt-1 inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
          >
            <MessageSquareReply className="h-3 w-3" />
            {message.thread_reply_count}{" "}
            {message.thread_reply_count === 1 ? "reply" : "replies"}
          </button>
        ) : null}
      </div>
    </div>
  );
}
