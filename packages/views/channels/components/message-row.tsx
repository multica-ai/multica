"use client";

import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";
import { Bot } from "lucide-react";
import { ReadonlyContent } from "../../editor";
import type { ChannelMessage, MemberWithUser, Agent } from "@multica/core/types";

interface MessageRowProps {
  message: ChannelMessage;
  member?: MemberWithUser;
  agent?: Agent;
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
 * markdown body. Soft-deleted messages render a placeholder; the row is
 * preserved in the timeline so thread continuity isn't visually broken.
 */
export function MessageRow(props: MessageRowProps) {
  const { message } = props;
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
    <div className="flex gap-3 px-4 py-2 hover:bg-muted/40 transition-colors">
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
        </div>
        <div className="prose prose-sm max-w-none text-foreground">
          <ReadonlyContent content={message.content} />
        </div>
      </div>
    </div>
  );
}
